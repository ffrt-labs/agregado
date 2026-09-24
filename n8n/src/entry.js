// Pure core of the ingest workflow: everything between "Resend content
// fetched" and "write to R2/D1" that has no I/O, so it is unit-testable.
const { entryId, permalinkUuid } = require("./identity");
const { resolveCanonicalUrl } = require("./extract");
const { sanitizeForPermalink, toReadableContent } = require("./sanitize");

// The catch-all alias is the Source id (agregado#99): local-part of the first
// recipient, lowercased.
function aliasFromRecipients(recipients) {
	const first = (recipients || [])[0];
	if (!first) return null;
	const match = String(first).match(/<([^>]+)>/);
	const address = (match ? match[1] : String(first)).trim();
	const at = address.lastIndexOf("@");
	const local = (at === -1 ? address : address.slice(0, at)).toLowerCase();
	return local || null;
}

// Unregistered and `pending` Sources are not failures — no write, no alert.
function gateSource(source) {
	return source && source.status === "active" ? "proceed" : "skip";
}

function buildEntry({ email, sourceId, secret, now }) {
	const headers = email.headers || {};
	const html = email.html || "";
	const text = email.text || "";
	if (!html.trim() && !text.trim()) throw new Error("email has no content");

	const id = entryId(headers["message-id"]);
	const dateMs = Date.parse(headers["date"] || "");
	return {
		id,
		sourceId,
		permalinkUuid: permalinkUuid(id, secret),
		canonicalUrl: resolveCanonicalUrl(headers, html),
		title: (email.subject || "").trim() || "(no subject)",
		readableContent: toReadableContent(html, text),
		// Plain-text-only mail has no HTML to preserve; the readable form doubles as the permalink.
		permalinkHtml: html.trim() ? sanitizeForPermalink(html) : `<!doctype html><meta charset="utf-8">${toReadableContent("", text)}`,
		publishedAt: Number.isFinite(dateMs) ? Math.floor(dateMs / 1000) : now,
	};
}

module.exports = { buildEntry, gateSource, aliasFromRecipients };
