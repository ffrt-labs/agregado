// Package telemetry holds the Langfuse-facing vocabulary every instrumented
// package shares: deterministic trace IDs and the OTel attribute names
// Langfuse maps. It imports no Agregado package, so any package (ai,
// articleindex, digestartifact, api, ...) can use it without an import cycle.
package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
)

// ArticleIndexKey is the trace key of an Article's enrichment trace.
func ArticleIndexKey(recordID string) string { return "article-index:" + recordID }

// DigestKey is the trace key of a Digest trace. Use the date ID
// ("20261030"), not the UUID the store returns after Save.
func DigestKey(artifactID string) string { return "digest:" + artifactID }

// TraceID derives the 16-byte trace ID for a trace key: the first 16 bytes of
// sha256(key). This matches the Langfuse Python SDK's
// create_trace_id(seed=key), so nothing has to be stored to find a trace.
func TraceID(key string) [16]byte {
	sum := sha256.Sum256([]byte(key))
	var id [16]byte
	copy(id[:], sum[:16])
	return id
}

// TraceIDHex is TraceID as the 32-char lowercase hex Langfuse's APIs use.
func TraceIDHex(key string) string {
	id := TraceID(key)
	return hex.EncodeToString(id[:])
}
