package bridgeadmin

import (
	"context"
	"time"

	"github.com/felipeafreitas/agregado/internal/cloudflare/d1"
)

// D1Store is SourceStore over the Bridge's D1 database
// (email-worker/migrations/0001_sources_and_entries.sql).
type D1Store struct {
	db *d1.Client
}

func NewD1Store(db *d1.Client) *D1Store { return &D1Store{db: db} }

var _ SourceStore = (*D1Store)(nil)

func (s *D1Store) Sources(ctx context.Context) ([]Source, error) {
	var rows []struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
		Status      string `json:"status"`
	}
	if _, err := s.db.Query(ctx, &rows, "SELECT id, display_name, status FROM sources ORDER BY display_name"); err != nil {
		return nil, err
	}
	sources := make([]Source, len(rows))
	for i, r := range rows {
		sources[i] = Source{Alias: r.ID, DisplayName: r.DisplayName, Status: r.Status}
	}
	return sources, nil
}

// IssueStats uses entries_source_published_idx; published_at is unix seconds.
func (s *D1Store) IssueStats(ctx context.Context, since time.Time) (map[string]IssueStats, error) {
	var rows []struct {
		SourceID string `json:"source_id"`
		LastAt   *int64 `json:"last_at"`
		Last30   int    `json:"last30"`
	}
	const sql = `SELECT source_id, MAX(published_at) AS last_at, SUM(published_at >= ?) AS last30
		FROM entries GROUP BY source_id`
	if _, err := s.db.Query(ctx, &rows, sql, since.Unix()); err != nil {
		return nil, err
	}
	stats := make(map[string]IssueStats, len(rows))
	for _, r := range rows {
		st := IssueStats{Last30Days: r.Last30}
		if r.LastAt != nil {
			t := time.Unix(*r.LastAt, 0).UTC()
			st.LastIssueAt = &t
		}
		stats[r.SourceID] = st
	}
	return stats, nil
}

func (s *D1Store) SourceExists(ctx context.Context, alias string) (bool, error) {
	var rows []struct {
		ID string `json:"id"`
	}
	if _, err := s.db.Query(ctx, &rows, "SELECT id FROM sources WHERE id = ?", alias); err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

// InsertPending is a plain INSERT, so a taken alias fails on the primary key
// rather than overwriting another Source's password.
func (s *D1Store) InsertPending(ctx context.Context, source Source, secretHash string) error {
	_, err := s.db.Exec(ctx,
		"INSERT INTO sources (id, display_name, basic_auth_secret_hash, status) VALUES (?, ?, ?, ?)",
		source.Alias, source.DisplayName, secretHash, StatusPending)
	return err
}

func (s *D1Store) RemovePending(ctx context.Context, alias string) error {
	_, err := s.db.Exec(ctx, "DELETE FROM sources WHERE id = ? AND status = 'pending'", alias)
	return err
}

// Activate is guarded by status = 'pending', so it never resumes a Paused
// Source; meta.changes says whether a row moved.
func (s *D1Store) Activate(ctx context.Context, alias string) (bool, error) {
	meta, err := s.db.Exec(ctx, "UPDATE sources SET status = 'active' WHERE id = ? AND status = 'pending'", alias)
	if err != nil {
		return false, err
	}
	return meta.Changes > 0, nil
}
