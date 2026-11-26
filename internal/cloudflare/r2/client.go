// Package r2 deletes objects from a Cloudflare R2 bucket through R2's
// S3-compatible API. It is not a general S3 client: it signs one kind of
// request (a path-style DELETE with an empty body) with AWS Signature
// Version 4, ported from n8n/src/sigv4.js. The S3 API is used rather than
// Cloudflare's REST API because it accepts a bucket-scoped R2 token ("Object
// Read & Write" on one bucket); the REST object endpoints need an
// account-wide R2 token.
//
// Endpoint: https://{account}.r2.cloudflarestorage.com/{bucket}/{key},
// region "auto", service "s3".
package r2

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	region          = "auto"
	service         = "s3"
	defaultTimeout  = 30 * time.Second
	emptyBodySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

type Options struct {
	// Endpoint defaults to https://{AccountID}.r2.cloudflarestorage.com.
	Endpoint        string
	AccountID       string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	Timeout         time.Duration
}

type Client struct {
	endpoint string
	// host is the Host the request is signed for and sent with.
	host      string
	accessKey string
	secret    string
	bucket    string
	http      *http.Client
	now       func() time.Time
}

func New(o Options) *Client {
	endpoint := o.Endpoint
	if endpoint == "" {
		endpoint = "https://" + o.AccountID + ".r2.cloudflarestorage.com"
	}
	endpoint = strings.TrimRight(endpoint, "/")
	host := endpoint
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		host = u.Host
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{
		endpoint:  endpoint,
		host:      host,
		accessKey: o.AccessKeyID,
		secret:    o.SecretAccessKey,
		bucket:    o.Bucket,
		http:      &http.Client{Timeout: timeout},
		now:       time.Now,
	}
}

// DeleteObject removes key from the bucket. An object that is already gone
// is not an error: S3 answers 204 for a missing key, and a 404 (NoSuchKey or
// similar) is treated the same, so a retried delete succeeds.
func (c *Client) DeleteObject(ctx context.Context, key string) error {
	if c.accessKey == "" || c.secret == "" {
		return fmt.Errorf("r2: access key id and secret are required")
	}
	path := "/" + escape(c.bucket) + "/" + escape(key)
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.endpoint+path, nil)
	if err != nil {
		return err
	}
	// Keep the escaped path byte for byte as signed.
	request.URL.RawPath = path
	request.Host = c.host
	c.sign(request, path, c.now().UTC())

	resp, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("r2: delete %s: %w", key, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 == 2 || resp.StatusCode == http.StatusNotFound {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return nil
	}
	return fmt.Errorf("r2: delete %s returned %s: %s", key, resp.Status, describe(resp.Body))
}

// sign adds x-amz-date, x-amz-content-sha256 and Authorization. Header names
// are lowercase and sorted in both the canonical headers and SignedHeaders;
// each canonical header line ends in \n, and the join supplies the blank
// line before SignedHeaders.
func (c *Client) sign(request *http.Request, canonicalURI string, now time.Time) {
	date := now.Format("20060102T150405Z")
	dateStamp := date[:8]

	request.Header.Set("x-amz-date", date)
	request.Header.Set("x-amz-content-sha256", emptyBodySHA256)
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalHeaders := "host:" + c.host + "\n" +
		"x-amz-content-sha256:" + emptyBodySHA256 + "\n" +
		"x-amz-date:" + date + "\n"

	canonicalRequest := strings.Join([]string{
		request.Method, canonicalURI, "", canonicalHeaders, signedHeaders, emptyBodySHA256,
	}, "\n")
	scope := dateStamp + "/" + region + "/" + service + "/aws4_request"
	stringToSign := strings.Join([]string{"AWS4-HMAC-SHA256", date, scope, sha256Hex(canonicalRequest)}, "\n")

	key := hmacSHA256([]byte("AWS4"+c.secret), dateStamp)
	key = hmacSHA256(key, region)
	key = hmacSHA256(key, service)
	key = hmacSHA256(key, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(key, stringToSign))

	request.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+c.accessKey+"/"+scope+
		", SignedHeaders="+signedHeaders+", Signature="+signature)
}

// escape is SigV4's URI encoding of one path segment: everything but the
// unreserved characters is percent-encoded, "/" included.
func escape(segment string) string {
	var b strings.Builder
	for i := 0; i < len(segment); i++ {
		ch := segment[i]
		if ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' ||
			ch == '-' || ch == '_' || ch == '.' || ch == '~' {
			b.WriteByte(ch)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", ch)
	}
	return b.String()
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

func describe(body io.Reader) string {
	raw, _ := io.ReadAll(io.LimitReader(body, 1<<10))
	var decoded struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	if xml.Unmarshal(raw, &decoded) == nil && decoded.Code != "" {
		if decoded.Message != "" {
			return decoded.Code + ": " + decoded.Message
		}
		return decoded.Code
	}
	return strings.TrimSpace(string(raw))
}
