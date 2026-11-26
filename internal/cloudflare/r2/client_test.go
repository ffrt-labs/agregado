package r2

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var fixedNow = time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)

// Ground truth computed independently with openssl (canonical request, HMAC
// key chain and signature by hand), with the credentials and clock of
// n8n/test/sigv4.test.js but for a DELETE with an empty body.
const (
	emptyPayloadHash  = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	wantAuthorization = "AWS4-HMAC-SHA256 Credential=AKIDTEST/20240102/auto/s3/aws4_request, " +
		"SignedHeaders=host;x-amz-content-sha256;x-amz-date, " +
		"Signature=46e646ced7de8d02db0b055f571bb3afc73fb55c2a851f0461f971a2073df973"
)

type seen struct {
	method, path, host, auth, date, payload string
}

func stubR2(t *testing.T, status int, body string) (*Client, *[]seen) {
	t.Helper()
	calls := &[]seen{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, seen{
			method: r.Method, path: r.URL.EscapedPath(), host: r.Host,
			auth: r.Header.Get("Authorization"), date: r.Header.Get("x-amz-date"),
			payload: r.Header.Get("x-amz-content-sha256"),
		})
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	c := New(Options{
		Endpoint:        server.URL,
		AccountID:       "abc123",
		AccessKeyID:     "AKIDTEST",
		SecretAccessKey: "secretkeytest",
		Bucket:          "test-bucket",
	})
	c.now = func() time.Time { return fixedNow }
	return c, calls
}

func TestDeleteObject_SendsASigV4SignedPathStyleDelete(t *testing.T) {
	c, calls := stubR2(t, http.StatusNoContent, "")
	// Sign for the real R2 host while talking to the stub.
	c.host = "abc123.r2.cloudflarestorage.com"

	if err := c.DeleteObject(context.Background(), "abc.html"); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 {
		t.Fatalf("calls = %+v", *calls)
	}
	got := (*calls)[0]
	if got.method != http.MethodDelete || got.path != "/test-bucket/abc.html" {
		t.Errorf("request = %s %s, want DELETE /test-bucket/abc.html", got.method, got.path)
	}
	if got.host != "abc123.r2.cloudflarestorage.com" {
		t.Errorf("host = %q", got.host)
	}
	if got.date != "20240102T030405Z" || got.payload != emptyPayloadHash {
		t.Errorf("x-amz-date %q, x-amz-content-sha256 %q", got.date, got.payload)
	}
	if got.auth != wantAuthorization {
		t.Errorf("Authorization =\n %s\nwant\n %s", got.auth, wantAuthorization)
	}
}

func TestDeleteObject_AMissingObjectCountsAsDeleted(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusOK, http.StatusNotFound} {
		c, _ := stubR2(t, status, `<?xml version="1.0"?><Error><Code>NoSuchKey</Code></Error>`)
		if err := c.DeleteObject(context.Background(), "gone"); err != nil {
			t.Errorf("status %d: err = %v, want nil (delete is idempotent)", status, err)
		}
	}
}

func TestDeleteObject_ReportsOtherFailuresWithR2sErrorCode(t *testing.T) {
	c, _ := stubR2(t, http.StatusForbidden, `<?xml version="1.0"?><Error><Code>AccessDenied</Code><Message>Access Denied</Message></Error>`)

	err := c.DeleteObject(context.Background(), "abc")
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("err = %v, want the status and R2's error code", err)
	}
}

func TestDeleteObject_EscapesTheKey(t *testing.T) {
	c, calls := stubR2(t, http.StatusNoContent, "")

	if err := c.DeleteObject(context.Background(), "entries/a b"); err != nil {
		t.Fatal(err)
	}
	if got := (*calls)[0].path; got != "/test-bucket/entries%2Fa%20b" {
		t.Errorf("path = %q", got)
	}
}

func TestNew_DefaultsToTheAccountsR2Endpoint(t *testing.T) {
	c := New(Options{AccountID: "abc123", AccessKeyID: "k", SecretAccessKey: "s", Bucket: "b"})
	if c.endpoint != "https://abc123.r2.cloudflarestorage.com" || c.host != "abc123.r2.cloudflarestorage.com" {
		t.Errorf("endpoint %q host %q", c.endpoint, c.host)
	}
}
