package bridgeadmin

import (
	"context"
	"fmt"
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

// assertStatusMove checks call is the guarded status update: set to, only
// on alias's row, only while it is from.
func assertStatusMove(t *testing.T, call d1Call, to Status, alias string, from Status) {
	t.Helper()
	want := []any{string(to), alias, string(from)}
	if call.SQL != "UPDATE sources SET status = ? WHERE id = ? AND status = ?" || fmt.Sprint(call.Params) != fmt.Sprint(want) {
		t.Errorf("call = %+v, want %s -> %s guarded on %v", call, from, to, want)
	}
}

func TestD1Store_PauseOnlyTouchesActiveRows(t *testing.T) {
	store, calls := d1Stub(t, nil)

	if err := store.Pause(context.Background(), "syntax"); err != nil {
		t.Fatal(err)
	}
	assertStatusMove(t, lastCall(t, calls), StatusPaused, "syntax", StatusActive)
}

func TestD1Store_ResumeOnlyTouchesPausedRows(t *testing.T) {
	store, calls := d1Stub(t, nil)

	if err := store.Resume(context.Background(), "syntax"); err != nil {
		t.Fatal(err)
	}
	assertStatusMove(t, lastCall(t, calls), StatusActive, "syntax", StatusPaused)
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
