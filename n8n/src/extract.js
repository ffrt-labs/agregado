// Canonical-URL recovery, ported from internal/ingestion/email/parser.go
// (ADR-0004): Archived-At header > "view in browser" anchor > nothing.
const cheerio = require("cheerio");

const VIEW_IN_BROWSER_PHRASES = [
	"view in browser",
	"view this email",
	"view in your browser",
	"view email in browser",
	"view online",
	"view it online",
	"see it in your browser",
	"read online",
	"read in browser",
	"web version",
];

const SHARE_MARKERS = [
	"twitter.com/intent",
	"x.com/intent",
	"facebook.com/sharer",
	"linkedin.com/share",
	"t.me/share",
	"/share?",
	"/sharer",
];

// Discrete host/path segments, split on "/" and ".", so pixel.gif is caught
// but a slug like /pixel-art-in-css is not.
function hasBlockedSegment(url) {
	const segments = [...url.hostname.split(/[/.]/), ...url.pathname.split(/[/.]/)];
	return segments.some((s) => ["unsubscribe", "pixel"].includes(s.toLowerCase()));
}

// Parsed by hand: n8n's Code-node sandbox may not expose the URL global, and a
// try/catch around new URL() then rejects every candidate silently.
function parseHttpUrl(rawUrl) {
	const raw = String(rawUrl).trim();
	if (/\s/.test(raw)) return null;
	const m = raw.match(/^https?:\/\/([^/?#]+)([^?#]*)/i);
	if (!m) return null;
	const hostname = m[1].replace(/^.*@/, "").replace(/:\d*$/, "").toLowerCase();
	return hostname ? { hostname, pathname: m[2] } : null;
}

function isCanonicalCandidate(rawUrl) {
	const url = parseHttpUrl(rawUrl);
	if (!url) return false;
	if (hasBlockedSegment(url)) return false;
	const lower = String(rawUrl).toLowerCase();
	return !SHARE_MARKERS.some((m) => lower.includes(m));
}

function isViewInBrowserText(text) {
	if (!text || text.includes("unsubscribe")) return false;
	return VIEW_IN_BROWSER_PHRASES.some((p) => text.includes(p));
}

function scrapeViewInBrowser(html) {
	if (!html || !html.trim()) return null;
	const $ = cheerio.load(html);
	let found = null;
	$("a[href]").each((_, el) => {
		const text = $(el).text().trim().toLowerCase();
		if (!isViewInBrowserText(text)) return;
		const href = ($(el).attr("href") || "").trim();
		if (isCanonicalCandidate(href)) {
			found = href;
			return false;
		}
	});
	return found;
}

// `headers` keys are lowercased. RFC 5064 wraps the Archived-At value in <>.
function resolveCanonicalUrl(headers, html) {
	const raw = ((headers || {})["archived-at"] || "").trim();
	if (raw) {
		const candidate = raw.replace(/^<|>$/g, "").trim();
		if (isCanonicalCandidate(candidate)) return candidate;
	}
	return scrapeViewInBrowser(html);
}

module.exports = { resolveCanonicalUrl, isCanonicalCandidate };
