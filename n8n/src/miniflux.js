// Miniflux's `new_entries` webhook: signature verification and payload
// parsing, plus the small pure decisions the article-enrichment workflow
// needs to make. No I/O, so it is unit-testable — the same pattern as
// svix.js/extract.js for the newsletter-ingest workflow.
const crypto = require("crypto");

// Miniflux signs webhook deliveries with a single HMAC-SHA256 hex digest of
// the raw body in `X-Miniflux-Signature`, unlike Resend/Svix's versioned,
// timestamped scheme (svix.js) — no timestamp tolerance check applies here.
function verifyMinifluxSignature({ secret, signatureHeader, rawBody }) {
	if (!secret || !signatureHeader || typeof rawBody !== "string") return false;
	const expected = crypto.createHmac("sha256", secret).update(rawBody, "utf8").digest("hex");
	const given = Buffer.from(String(signatureHeader), "utf8");
	const wanted = Buffer.from(expected, "utf8");
	return given.length === wanted.length && crypto.timingSafeEqual(given, wanted);
}

// Only `new_entries` carries Articles to enrich; other Miniflux webhook
// event types are ignored by returning no entries.
function parseNewEntries(payload) {
	if (!payload || payload.event_type !== "new_entries") return [];
	return (payload.entries || []).map((e) => ({
		id: e.id,
		url: e.url,
		title: e.title,
		author: e.author || undefined,
		publishedAt: e.published_at || undefined,
	}));
}

// Recognises `{bridge origin}/p/{uuid}` — an email-only Article served from
// the Bridge's Worker (n8n/README.md's "Enrichment" section, agregado#100).
// Ordinary RSS Article URLs never match.
function matchBridgePermalink(url, bridgeOrigin) {
	if (!url || !bridgeOrigin) return null;
	const escapedOrigin = String(bridgeOrigin)
		.replace(/\/+$/, "")
		.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
	const re = new RegExp(`^${escapedOrigin}/p/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$`, "i");
	const match = String(url).match(re);
	return match ? match[1].toLowerCase() : null;
}

// A 4xx from the enrich endpoint is a validation/contract problem a retry
// cannot fix (issue criterion 6); anything else — network failure, timeout,
// 5xx — is treated as transient and retried with backoff. n8n's HTTP node
// surfaces a failed request as an error message rather than a structured
// status field, so the code is parsed out of it.
function classifyEnrichFailure(message) {
	return /\b4\d{2}\b/.test(String(message || "")) ? "terminal" : "retryable";
}

module.exports = { verifyMinifluxSignature, parseNewEntries, matchBridgePermalink, classifyEnrichFailure };
