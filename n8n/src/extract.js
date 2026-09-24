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

function isCanonicalCandidate(rawUrl) {
	let url;
	try {
		url = new URL(String(rawUrl).trim());
	} catch {
		return false;
	}
	if (url.protocol !== "http:" && url.protocol !== "https:") return false;
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
