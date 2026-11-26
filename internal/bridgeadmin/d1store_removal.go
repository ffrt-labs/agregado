package bridgeadmin

import (
	"context"
	"time"
)

// SourceRemoval over D1. Each statement is naturally idempotent: an UPDATE
// or DELETE matching no row succeeds with zero changes.

// Pause is guarded by status = 'active', so a Paused row always had been
// Active and Resume cannot move a Pending Source past Activate.
func (s *D1Store) Pause(ctx context.Context, alias string) error {
	_, err := s.db.Exec(ctx, "UPDATE sources SET status = 'paused' WHERE id = ? AND status = 'active'", alias)
	return err
}

func (s *D1Store) Resume(ctx context.Context, alias string) error {
	_, err := s.db.Exec(ctx, "UPDATE sources SET status = 'active' WHERE id = ? AND status = 'paused'", alias)
	return err
}

func (s *D1Store) IssueSummary(ctx context.Context, alias string) (IssueSummary, error) {
	var rows []struct {
		N      int    `json:"n"`
		LastAt *int64 `json:"last_at"`
	}
	const sql = "SELECT COUNT(*) AS n, MAX(published_at) AS last_at FROM entries WHERE source_id = ?"
	if _, err := s.db.Query(ctx, &rows, sql, alias); err != nil {
		return IssueSummary{}, err
	}
	var summary IssueSummary
	if len(rows) > 0 {
		summary.Count = rows[0].N
		if rows[0].LastAt != nil {
			t := time.Unix(*rows[0].LastAt, 0).UTC()
			summary.LastIssueAt = &t
		}
	}
	return summary, nil
}

func (s *D1Store) EntryIDs(ctx context.Context, alias string) ([]string, error) {
	var rows []struct {
		ID string `json:"id"`
	}
	if _, err := s.db.Query(ctx, &rows, "SELECT id FROM entries WHERE source_id = ?", alias); err != nil {
		return nil, err
	}
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids, nil
}

func (s *D1Store) DeleteEntries(ctx context.Context, alias string) error {
	_, err := s.db.Exec(ctx, "DELETE FROM entries WHERE source_id = ?", alias)
	return err
}

func (s *D1Store) DeleteSource(ctx context.Context, alias string) error {
	_, err := s.db.Exec(ctx, "DELETE FROM sources WHERE id = ?", alias)
	return err
}
