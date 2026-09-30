// Builds the request body for POST /api/private/articles/enrich
// (docs/article-index-api.md) from a parsed Miniflux entry (src/miniflux.js)
// and, for email-only Articles, the Bridge's stored readable content.
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
	return body;
}

module.exports = { buildEnrichRequest };
