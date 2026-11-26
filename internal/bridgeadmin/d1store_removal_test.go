package bridgeadmin

import (
	"context"
	"strings"
	"testing"
	"time"
)

func lastCall(t *testing.T, calls *[]d1Call) d1Call {
	t.Helper()
	if len(*calls) == 0 {
		t.Fatal("no D1 call")
	}
	return (*calls)[len(*calls)-1]
}

func TestD1Store_PauseOnlyTouchesActiveRows(t *testing.T) {
	store, calls := d1Stub(t, nil)

	if err := store.Pause(context.Background(), "syntax"); err != nil {
		t.Fatal(err)
	}
	call := lastCall(t, calls)
	if !strings.HasPrefix(call.SQL, "UPDATE sources SET status = 'paused'") || !strings.Contains(call.SQL, "AND status = 'active'") ||
		len(call.Params) != 1 || call.Params[0] != "syntax" {
		t.Errorf("call = %+v, want an update guarded by status = 'active'", call)
	}
}

func TestD1Store_ResumeOnlyTouchesPausedRows(t *testing.T) {
	store, calls := d1Stub(t, nil)

	if err := store.Resume(context.Background(), "syntax"); err != nil {
		t.Fatal(err)
	}
	call := lastCall(t, calls)
	if !strings.HasPrefix(call.SQL, "UPDATE sources SET status = 'active'") || !strings.Contains(call.SQL, "status = 'paused'") {
		t.Errorf("sql = %q, want an update guarded by status = 'paused'", call.SQL)
	}
}

func TestD1Store_IssueSummaryCountsAndDatesTheLastIssue(t *testing.T) {
	store, calls := d1Stub(t, map[string]string{"COUNT(*)": `[{"n":3,"last_at":1763625600}]`})

	got, err := store.IssueSummary(context.Background(), "syntax")
	if err != nil {
		t.Fatal(err)
	}
	if got.Count != 3 || got.LastIssueAt == nil || !got.LastIssueAt.Equal(time.Unix(1763625600, 0)) {
		t.Errorf("summary = %+v", got)
	}
	if call := lastCall(t, calls); call.Params[0] != "syntax" || !strings.Contains(call.SQL, "WHERE source_id = ?") {
		t.Errorf("call = %+v", call)
	}
}

func TestD1Store_IssueSummaryOfASourceWithNoIssues(t *testing.T) {
	store, _ := d1Stub(t, map[string]string{"COUNT(*)": `[{"n":0,"last_at":null}]`})

	got, err := store.IssueSummary(context.Background(), "syntax")
	if err != nil || got.Count != 0 || got.LastIssueAt != nil {
		t.Fatalf("summary = %+v, %v", got, err)
	}
}

func TestD1Store_EntryIDs(t *testing.T) {
	store, calls := d1Stub(t, map[string]string{"SELECT id FROM entries": `[{"id":"e1"},{"id":"e2"}]`})

	ids, err := store.EntryIDs(context.Background(), "syntax")
	if err != nil || len(ids) != 2 || ids[0] != "e1" || ids[1] != "e2" {
		t.Fatalf("ids = %v, %v", ids, err)
	}
	if call := lastCall(t, calls); call.Params[0] != "syntax" || !strings.Contains(call.SQL, "WHERE source_id = ?") {
		t.Errorf("call = %+v", call)
	}
}

func TestD1Store_DeleteEntriesThenSource(t *testing.T) {
	store, calls := d1Stub(t, nil)
	ctx := context.Background()

	if err := store.DeleteEntries(ctx, "syntax"); err != nil {
		t.Fatal(err)
	}
	if call := lastCall(t, calls); call.SQL != "DELETE FROM entries WHERE source_id = ?" || call.Params[0] != "syntax" {
		t.Errorf("entries call = %+v", call)
	}
	if err := store.DeleteSource(ctx, "syntax"); err != nil {
		t.Fatal(err)
	}
	if call := lastCall(t, calls); call.SQL != "DELETE FROM sources WHERE id = ?" || call.Params[0] != "syntax" {
		t.Errorf("source call = %+v", call)
	}
}
