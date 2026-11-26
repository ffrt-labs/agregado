package bridgeadmin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/felipeafreitas/agregado/internal/cloudflare/d1"
)

type d1Call struct {
	SQL    string `json:"sql"`
	Params []any  `json:"params"`
}

// d1Stub answers each statement with the rows the test gives for the first
// matching SQL fragment, recording every call.
func d1Stub(t *testing.T, answers map[string]string) (*D1Store, *[]d1Call) {
	t.Helper()
	calls := &[]d1Call{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var call d1Call
		json.NewDecoder(r.Body).Decode(&call)
		*calls = append(*calls, call)
		for fragment, rows := range answers {
			if strings.Contains(call.SQL, fragment) {
				w.Write([]byte(`{"success":true,"result":[{"success":true,"results":` + rows + `,"meta":{"changes":1}}]}`))
				return
			}
		}
		w.Write([]byte(`{"success":true,"result":[{"success":true,"results":[],"meta":{"changes":0}}]}`))
	}))
	t.Cleanup(server.Close)
	client := d1.New(d1.Options{BaseURL: server.URL, AccountID: "a", DatabaseID: "b", APIToken: "t"})
	return NewD1Store(client), calls
}

func TestD1Store_ReadsSourcesAndIssueStats(t *testing.T) {
	store, calls := d1Stub(t, map[string]string{
		"FROM sources": `[{"id":"tldr","display_name":"TLDR","status":"active"}]`,
		"FROM entries": `[{"source_id":"tldr","last_at":1764055800,"last30":21}]`,
	})
	ctx := context.Background()

	sources, err := store.Sources(ctx)
	if err != nil || len(sources) != 1 || sources[0] != (Source{Alias: "tldr", DisplayName: "TLDR", Status: StatusActive}) {
		t.Fatalf("sources = %+v, %v", sources, err)
	}
	since := time.Unix(1761480000, 0)
	stats, err := store.IssueStats(ctx, since)
	if err != nil {
		t.Fatal(err)
	}
	got := stats["tldr"]
	if got.Last30Days != 21 || got.LastIssueAt == nil || !got.LastIssueAt.Equal(time.Unix(1764055800, 0)) {
		t.Errorf("stats = %+v", got)
	}
	if last := (*calls)[len(*calls)-1]; len(last.Params) != 1 || last.Params[0] != float64(1761480000) {
		t.Errorf("stats params = %v, want the window start in unix seconds", last.Params)
	}
}

func TestD1Store_InsertPendingStoresTheHashAsPending(t *testing.T) {
	store, calls := d1Stub(t, nil)

	if err := store.InsertPending(context.Background(), Source{Alias: "syntax", DisplayName: "Syntax"}, "abc123"); err != nil {
		t.Fatal(err)
	}
	call := (*calls)[0]
	if !strings.HasPrefix(call.SQL, "INSERT INTO sources") {
		t.Errorf("sql = %q, want a plain INSERT (a taken alias must fail, not be replaced)", call.SQL)
	}
	want := []any{"syntax", "Syntax", "abc123", "pending"}
	if len(call.Params) != len(want) {
		t.Fatalf("params = %v, want %v", call.Params, want)
	}
	for i := range want {
		if call.Params[i] != want[i] {
			t.Errorf("params = %v, want %v", call.Params, want)
		}
	}
}

func TestD1Store_RemovePendingOnlyTouchesPendingRows(t *testing.T) {
	store, calls := d1Stub(t, nil)

	if err := store.RemovePending(context.Background(), "syntax"); err != nil {
		t.Fatal(err)
	}
	call := (*calls)[0]
	if !strings.Contains(call.SQL, "status = 'pending'") || call.Params[0] != "syntax" {
		t.Errorf("call = %+v, want a delete guarded by status = 'pending'", call)
	}
}

func TestD1Store_SourceExists(t *testing.T) {
	store, _ := d1Stub(t, map[string]string{"WHERE id = ?": `[{"id":"tldr"}]`})
	ok, err := store.SourceExists(context.Background(), "tldr")
	if err != nil || !ok {
		t.Fatalf("exists = %v, %v", ok, err)
	}
}

func TestD1Store_RenameSourceSetsOnlyTheDisplayName(t *testing.T) {
	store, calls := d1Stub(t, map[string]string{"SET display_name": `[]`})

	if err := store.RenameSource(context.Background(), "lenny", "Lenny's Newsletter"); err != nil {
		t.Fatal(err)
	}
	call := (*calls)[0]
	if !strings.HasPrefix(call.SQL, "UPDATE sources SET display_name = ? WHERE id = ?") ||
		len(call.Params) != 2 || call.Params[0] != "Lenny's Newsletter" || call.Params[1] != "lenny" {
		t.Errorf("call = %+v", call)
	}
}

func TestD1Store_SetSecretHashReplacesTheStoredHash(t *testing.T) {
	store, calls := d1Stub(t, map[string]string{"SET basic_auth_secret_hash": `[]`})

	if err := store.SetSecretHash(context.Background(), "lenny", "def456"); err != nil {
		t.Fatal(err)
	}
	call := (*calls)[0]
	if !strings.HasPrefix(call.SQL, "UPDATE sources SET basic_auth_secret_hash = ? WHERE id = ?") ||
		len(call.Params) != 2 || call.Params[0] != "def456" || call.Params[1] != "lenny" {
		t.Errorf("call = %+v", call)
	}
}

func TestD1Store_RenameAndSetSecretHashFailWhenTheSourceIsGone(t *testing.T) {
	store, _ := d1Stub(t, nil) // every statement reports 0 changes

	if err := store.RenameSource(context.Background(), "gone", "Gone"); err == nil {
		t.Error("renaming a missing Source succeeded")
	}
	if err := store.SetSecretHash(context.Background(), "gone", "x"); err == nil {
		t.Error("a new hash for a missing Source succeeded")
	}
}
