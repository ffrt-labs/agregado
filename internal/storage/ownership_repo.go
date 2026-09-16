package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/felipeafreitas/agregado/internal/ownership"
	"github.com/jackc/pgx/v5"
)

// LegacyRepo reads the old Agregado schema for the ownership migration
// (issue #83). It is read-only by construction: nothing here writes to
// articles, sources, or any other legacy table, so a botched run can never
// damage the database the final dump is taken from.
type LegacyRepo struct{ db *DB }

func NewLegacyRepo(db *DB) *LegacyRepo { return &LegacyRepo{db: db} }

// Articles loads every legacy Article with the derived data that has a new
// owner. It deliberately selects `content IS NOT NULL` rather than content:
// bodies do not enter the new schema, and loading tens of megabytes of
// Markdown just to count it would be absurd.
//
// opened_at falls back to ingested_at when a row is flagged read but never
// recorded when — an Open is a weak signal whose timestamp only has to be
// plausible, and dropping those Opens would lose real preference data.
//
// The vote is the *current* one: article_feedback is an append-only log, so
// the lateral join takes the most recent row per Article.
func (r *LegacyRepo) Articles(ctx context.Context) ([]ownership.LegacyArticle, error) {
	rows, err := r.db.pool.Query(ctx, `
		SELECT a.id,
		       COALESCE(a.external_url, ''),
		       COALESCE(a.canonical_url, ''),
		       a.title,
		       COALESCE(a.author, ''),
		       COALESCE(a.summary, ''),
		       a.published_at,
		       COALESCE(a.relevance_score, 0),
		       COALESCE(
		           ARRAY_AGG(t.name ORDER BY t.name) FILTER (WHERE t.name IS NOT NULL),
		           '{}'
		       ) AS tags,
		       a.is_saved,
		       a.saved_at,
		       COALESCE(a.read_at, CASE WHEN a.is_read THEN a.ingested_at END) AS opened_at,
		       COALESCE(v.vote, ''),
		       v.created_at,
		       (COALESCE(a.content, '') <> '' OR COALESCE(a.distilled_content, '') <> '') AS has_body,
		       a.created_at
		FROM articles a
		LEFT JOIN article_tags at ON at.article_id = a.id
		LEFT JOIN tags t ON t.id = at.tag_id
		LEFT JOIN LATERAL (
		    SELECT f.vote, f.created_at
		    FROM article_feedback f
		    WHERE f.article_id = a.id
		    ORDER BY f.created_at DESC, f.id DESC
		    LIMIT 1
		) v ON true
		GROUP BY a.id, v.vote, v.created_at
		ORDER BY a.created_at, a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var articles []ownership.LegacyArticle
	for rows.Next() {
		var article ownership.LegacyArticle
		if err := rows.Scan(
			&article.ID, &article.ExternalURL, &article.CanonicalURL, &article.Title, &article.Author,
			&article.Summary, &article.PublishedAt, &article.Score, &article.Tags, &article.IsSaved,
			&article.SavedAt, &article.ReadAt, &article.Vote, &article.VotedAt, &article.HasBody,
			&article.CreatedAt,
		); err != nil {
			return nil, err
		}
		articles = append(articles, article)
	}
	return articles, rows.Err()
}

// SourceCount counts the Sources deliberately left behind: subscription
// management moves to Miniflux, so source configuration has no row in the new
// schema (ADR-0006). Counting them is how the exclusion gets reported instead
// of being a silent absence.
func (r *LegacyRepo) SourceCount(ctx context.Context) (int, error) {
	var total int
	err := r.db.pool.QueryRow(ctx, `SELECT COUNT(*) FROM sources`).Scan(&total)
	return total, err
}

// OwnershipImportRepo writes migrated records into the Article Index. Every
// statement is written so that running it twice changes nothing the second
// time, and so that it can never overwrite Enrichment the live pipeline
// produced — a migrated historical record only ever fills gaps.
type OwnershipImportRepo struct{ db *DB }

func NewOwnershipImportRepo(db *DB) *OwnershipImportRepo { return &OwnershipImportRepo{db: db} }

// state is what the Article Index already holds for one canonical URL.
type state struct {
	exists   bool
	hasOpen  bool
	hasVote  bool
	recordID string
}

func (r *OwnershipImportRepo) state(ctx context.Context, canonicalURL string) (state, error) {
	var found state
	err := r.db.pool.QueryRow(ctx, `
		SELECT a.id, a.opened_at IS NOT NULL, f.vote IS NOT NULL
		FROM article_index a
		LEFT JOIN article_index_feedback f ON f.article_index_id = a.id
		WHERE a.canonical_url = $1`, canonicalURL).Scan(&found.recordID, &found.hasOpen, &found.hasVote)
	if err == pgx.ErrNoRows {
		return state{}, nil
	}
	if err != nil {
		return state{}, err
	}
	found.exists = true
	return found, nil
}

// Import upserts one migrated record. The outcome is decided by reading the
// destination first, so a dry run and an apply agree on exactly what would
// happen — the read is the same statement in both modes, and only the writes
// are skipped.
//
// The insert is not serialized against the live enrichment pipeline. A record
// created in between the read and the write is absorbed by ON CONFLICT: the
// worst case is a count that is one off, never a lost or clobbered row.
func (r *OwnershipImportRepo) Import(ctx context.Context, record ownership.IndexImport, write ownership.Write) (ownership.ImportResult, error) {
	before, err := r.state(ctx, record.CanonicalURL)
	if err != nil {
		return ownership.ImportResult{}, err
	}
	result := ownership.ImportResult{
		Created:    !before.exists,
		OpenStored: record.OpenedAt != nil && !before.hasOpen,
		VoteStored: record.Vote != "" && !before.hasVote,
	}
	if write == ownership.DryRun {
		return result, nil
	}

	tags, err := json.Marshal(record.Tags)
	if err != nil {
		return ownership.ImportResult{}, fmt.Errorf("encode migrated tags: %w", err)
	}

	transaction, err := r.db.pool.Begin(ctx)
	if err != nil {
		return ownership.ImportResult{}, err
	}
	defer transaction.Rollback(ctx)

	// miniflux_entry_id is NULL: a migrated Article never existed in Miniflux,
	// so there is nothing to Decorate. processing_status is 'complete' because
	// nothing further will ever be computed for it — its Enrichment is final,
	// whether or not it is complete.
	//
	// ON CONFLICT touches opened_at only. A canonical URL the live pipeline has
	// already enriched keeps its own title, summary, tags and Score; the
	// migration is not allowed to overwrite fresher data with history.
	var recordID string
	err = transaction.QueryRow(ctx, `
		INSERT INTO article_index
		    (miniflux_entry_id, canonical_url, title, author, published_at, summary, tags, score,
		     processing_status, opened_at, created_at, updated_at)
		VALUES (NULL, $1, $2, NULLIF($3, ''), $4, NULLIF($5, ''), $6, NULLIF($7, 0)::smallint,
		        'complete', $8, NOW(), NOW())
		ON CONFLICT (canonical_url) DO UPDATE
		SET opened_at  = COALESCE(article_index.opened_at, EXCLUDED.opened_at),
		    updated_at = NOW()
		RETURNING id`,
		record.CanonicalURL, record.Title, record.Author, record.PublishedAt,
		record.Summary, tags, record.Score, record.OpenedAt).Scan(&recordID)
	if err != nil {
		return ownership.ImportResult{}, err
	}

	// The vote is written separately from the record so a partially completed
	// earlier run still lands it, and DO NOTHING so a vote cast since the
	// migration started is never rolled back to a historical one.
	if record.Vote != "" {
		votedAt := time.Now()
		if record.VotedAt != nil {
			votedAt = *record.VotedAt
		}
		if _, err := transaction.Exec(ctx, `
			INSERT INTO article_index_feedback (article_index_id, vote, created_at, updated_at)
			VALUES ($1, $2, $3, NOW())
			ON CONFLICT (article_index_id) DO NOTHING`, recordID, record.Vote, votedAt); err != nil {
			return ownership.ImportResult{}, err
		}
	}

	return result, transaction.Commit(ctx)
}

// Lookup reads a migrated record back out, so the run can be compared against
// the old database rather than trusted.
func (r *OwnershipImportRepo) Lookup(ctx context.Context, canonicalURL string) (ownership.IndexImport, bool, error) {
	var record ownership.IndexImport
	var tags []byte
	err := r.db.pool.QueryRow(ctx, `
		SELECT a.canonical_url, a.title, COALESCE(a.author, ''), a.published_at, COALESCE(a.summary, ''),
		       a.tags, COALESCE(a.score, 0), a.opened_at, COALESCE(f.vote, ''), f.created_at
		FROM article_index a
		LEFT JOIN article_index_feedback f ON f.article_index_id = a.id
		WHERE a.canonical_url = $1`, canonicalURL).Scan(
		&record.CanonicalURL, &record.Title, &record.Author, &record.PublishedAt, &record.Summary,
		&tags, &record.Score, &record.OpenedAt, &record.Vote, &record.VotedAt)
	if err == pgx.ErrNoRows {
		return ownership.IndexImport{CanonicalURL: canonicalURL}, false, nil
	}
	if err != nil {
		return ownership.IndexImport{}, false, err
	}
	if err := json.Unmarshal(tags, &record.Tags); err != nil {
		return ownership.IndexImport{}, false, fmt.Errorf("decode migrated tags: %w", err)
	}
	return record, true, nil
}
