package resend

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newStub(t *testing.T, mux *http.ServeMux) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer re_key" {
			t.Errorf("%s %s: Authorization = %q, want Bearer re_key", r.Method, r.URL.Path, got)
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return New(server.URL+"/", "re_key", 0)
}

func TestReceivedEmails_AsksForTheLatest100AndDecodesThem(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /emails/receiving", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("limit"); got != "100" {
			t.Errorf("limit = %q, want 100", got)
		}
		if r.URL.Query().Has("to") {
			t.Errorf("unfiltered list sent to=%q", r.URL.Query().Get("to"))
		}
		w.Write([]byte(`{"object":"list","has_more":false,"data":[
			{"id":"e1","to":["tldr@news.example.com"],"from":"TLDR <dan@tldr.tech>",
			 "created_at":"2026-11-25T07:30:00.123456+00:00","subject":"Confirm your subscription","cc":[],"attachments":[]}]}`))
	})
	emails, err := newStub(t, mux).ReceivedEmails(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(emails) != 1 {
		t.Fatalf("emails = %+v", emails)
	}
	e := emails[0]
	if e.ID != "e1" || len(e.To) != 1 || e.To[0] != "tldr@news.example.com" || e.From != "TLDR <dan@tldr.tech>" ||
		e.Subject != "Confirm your subscription" || !e.CreatedAt.Equal(time.Date(2026, 11, 25, 7, 30, 0, 123456000, time.UTC)) {
		t.Errorf("email = %+v", e)
	}
}

func TestReceivedEmails_FiltersByRecipientWhenGiven(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /emails/receiving", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("to"); got != "tldr@news.example.com" {
			t.Errorf("to = %q", got)
		}
		w.Write([]byte(`{"object":"list","data":[]}`))
	})
	if _, err := newStub(t, mux).ReceivedEmails(context.Background(), "tldr@news.example.com"); err != nil {
		t.Fatal(err)
	}
}

func TestReceivedEmail_ReturnsTheBodyWithoutInlinedImages(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /emails/receiving/e1", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("html_format"); got != "cid" {
			t.Errorf("html_format = %q, want cid", got)
		}
		w.Write([]byte(`{"object":"email","id":"e1","to":["tldr@news.example.com"],"from":"dan@tldr.tech",
			"created_at":"2026-11-25T07:30:00Z","subject":"Hi","html":"<a href=\"https://x\">x</a>","text":null}`))
	})
	e, err := newStub(t, mux).ReceivedEmail(context.Background(), "e1")
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != "e1" || e.HTML != `<a href="https://x">x</a>` || e.Text != "" {
		t.Errorf("email = %+v", e)
	}
}

func TestErrorsCarryResendsMessage(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /emails/receiving", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"statusCode":401,"name":"restricted_api_key","message":"This API key is restricted to only send emails"}`))
	})
	_, err := newStub(t, mux).ReceivedEmails(context.Background())
	if err == nil || !strings.Contains(err.Error(), "restricted to only send emails") || !strings.Contains(err.Error(), "401") {
		t.Errorf("err = %v", err)
	}
}

func TestCreatedAtAcceptsPostgresStyleTimestamps(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /emails/receiving", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":"e1","created_at":"2026-11-25 07:30:00.5+00"}]}`))
	})
	emails, err := newStub(t, mux).ReceivedEmails(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !emails[0].CreatedAt.Equal(time.Date(2026, 11, 25, 7, 30, 0, 500000000, time.UTC)) {
		t.Errorf("created_at = %v", emails[0].CreatedAt)
	}
}
