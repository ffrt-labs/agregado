// D1 statements for Cloudflare's REST query endpoint. Always parameterised:
// the alias is attacker-influenced (it is the recipient local-part).
function lookupSourceQuery(alias) {
	return { sql: "SELECT id, status FROM sources WHERE id = ?", params: [alias] };
}

// INSERT OR REPLACE keyed on entries.id makes a Resend redelivery of the same
// Message-ID a no-op overwrite rather than a duplicate entry.
function upsertEntryQuery(e) {
	return {
		sql: "INSERT OR REPLACE INTO entries (id, source_id, canonical_url, permalink_uuid, title, readable_content, published_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		params: [e.id, e.sourceId, e.canonicalUrl, e.permalinkUuid, e.title, e.readableContent, e.publishedAt],
	};
}

function claimAlertQuery(id, nowSeconds) {
	return { sql: "INSERT OR IGNORE INTO ingest_alerts (id, alerted_at) VALUES (?, ?)", params: [id, nowSeconds] };
}

module.exports = { lookupSourceQuery, upsertEntryQuery, claimAlertQuery };
