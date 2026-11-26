package d1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type queryBody struct {
	SQL    string `json:"sql"`
	Params []any  `json:"params"`
}

func stub(t *testing.T, handle func(w http.ResponseWriter, body queryBody)) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/accounts/acct/d1/database/db-id/query" {
			t.Errorf("request %s %s, want POST /accounts/acct/d1/database/db-id/query", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q, want Bearer tok", got)
		}
		var body queryBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		handle(w, body)
	}))
	t.Cleanup(server.Close)
	return New(Options{BaseURL: server.URL, AccountID: "acct", DatabaseID: "db-id", APIToken: "tok"})
}

func TestQuery_SendsSQLAndParamsAndDecodesRows(t *testing.T) {
	client := stub(t, func(w http.ResponseWriter, body queryBody) {
		if body.SQL != "SELECT id, n FROM t WHERE id = ? AND n >= ?" {
			t.Errorf("sql = %q", body.SQL)
		}
		if len(body.Params) != 2 || body.Params[0] != "tldr" || body.Params[1] != float64(5) {
			t.Errorf("params = %#v, want [tldr 5]", body.Params)
		}
		w.Write([]byte(`{"success":true,"errors":[],"result":[{"success":true,
			"results":[{"id":"tldr","n":7},{"id":"syntax","n":12}],
			"meta":{"changes":0}}]}`))
	})

	var rows []struct {
		ID string `json:"id"`
		N  int64  `json:"n"`
	}
	meta, err := client.Query(context.Background(), &rows, "SELECT id, n FROM t WHERE id = ? AND n >= ?", "tldr", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != "tldr" || rows[1].N != 12 {
		t.Errorf("rows = %+v", rows)
	}
	if meta.Changes != 0 {
		t.Errorf("changes = %d", meta.Changes)
	}
}

func TestExec_ReportsChangedRows(t *testing.T) {
	client := stub(t, func(w http.ResponseWriter, _ queryBody) {
		w.Write([]byte(`{"success":true,"result":[{"success":true,"results":[],"meta":{"changes":1}}]}`))
	})
	meta, err := client.Exec(context.Background(), "DELETE FROM t WHERE id = ?", "x")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Changes != 1 {
		t.Errorf("changes = %d, want 1", meta.Changes)
	}
}

func TestQuery_SurfacesD1ErrorMessage(t *testing.T) {
	client := stub(t, func(w http.ResponseWriter, _ queryBody) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"success":false,"errors":[{"code":7500,"message":"UNIQUE constraint failed: sources.id"}],"result":[]}`))
	})
	_, err := client.Exec(context.Background(), "INSERT INTO sources (id) VALUES (?)", "tldr")
	if err == nil || !strings.Contains(err.Error(), "UNIQUE constraint failed: sources.id") {
		t.Fatalf("err = %v, want it to carry D1's message", err)
	}
}

func TestQuery_RejectsSuccessFalseOn200(t *testing.T) {
	client := stub(t, func(w http.ResponseWriter, _ queryBody) {
		w.Write([]byte(`{"success":false,"errors":[{"code":1,"message":"boom"}]}`))
	})
	if _, err := client.Exec(context.Background(), "SELECT 1"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want boom", err)
	}
}
