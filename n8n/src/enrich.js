// The enrich endpoint caps request bodies at 1 MiB; Miniflux entry content is
// otherwise unbounded, and feed_content is only a fallback for failed fetches.
const MAX_FEED_CONTENT_CHARS = 200000;

// Builds the request body for POST /api/private/articles/enrich
// (docs/article-index-api.md) from a parsed Miniflux entry (src/miniflux.js)
// and, for email-only Articles, the Bridge's stored readable content.
// feed_content is Miniflux's own content for the entry: the API uses it only
// when fetching the canonical URL fails, so a fetch failure still gets a
// `[content: partial]` Summary instead of failing the Article.
// source_id (the API's optional body field) is deliberately never sent: the
// new_entries payload has no value that is actually Agregado's Source id —
// Miniflux's own feed.id is a different identity space (n8n/README.md's
// "Deliberate deviations from the plan").
function buildEnrichRequest(entry, bridgeContent) {
	if (!entry || !entry.id) throw new Error("entry is missing its Miniflux id");
	if (!entry.url) throw new Error("entry is missing its url");
	const body = {
		entry_id: entry.id,
		canonical_url: entry.url,
		title: entry.title || "(no title)",
	};
	if (entry.author) body.author = entry.author;
	if (entry.publishedAt) body.published_at = entry.publishedAt;
	if (bridgeContent) body.bridge_content = bridgeContent;
	const feedContent = entry.feedContent && entry.feedContent.trim();
	if (feedContent) body.feed_content = feedContent.slice(0, MAX_FEED_CONTENT_CHARS);
	return body;
}

module.exports = { buildEnrichRequest };
