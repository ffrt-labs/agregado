// Pure helpers for the daily-digest workflow (agregado#145): today's date as
// the Digest's idempotency key, and the request to Agregado's persisted
// artifact endpoint (internal/digestartifact/handler.go:
// POST /api/private/digests/{date}). No I/O, so it is unit-testable, the
// same pattern as src/enrich.js/src/miniflux.js for article enrichment.

// UTC, matching the handler's own day boundary (it parses the path segment
// with time.Parse("2006-01-02", raw) and otherwise defaults to
// time.Now().UTC()) — using the n8n container's local day here could ask for
// a different day than the one Agregado would pick on its own default path.
function digestDateKey(now) {
	return new Date(now).toISOString().slice(0, 10);
}

// The endpoint is idempotent per date (#79's AC8): calling it again for the
// same date re-returns the already-persisted artifact rather than
// regenerating it, so a retry only ever needs this same request rebuilt
// against the same date, never a different one.
function buildDigestRequest(now, baseUrl) {
	if (!baseUrl) throw new Error("AGREGADO_BASE_URL is not set");
	const date = digestDateKey(now);
	return { date, url: `${String(baseUrl).replace(/\/+$/, "")}/api/private/digests/${date}` };
}

// An empty Digest is still sent (#79's AC12), but it means the pipeline
// produced nothing to read, so it also reaches the alert channel. Agregado
// states why in EmptyReason; the artifact is not persisted, so re-running the
// workflow once the cause is fixed regenerates it.
function emptyDigestAlert(artifact, date) {
	if (!artifact || artifact.SelectedCount > 0) return null;
	return { date, message: `empty: ${artifact.EmptyReason || "no reason given"}` };
}

module.exports = { digestDateKey, buildDigestRequest, emptyDigestAlert };
