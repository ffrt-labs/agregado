const test = require("node:test");
const assert = require("node:assert/strict");
const { lookupSourceQuery, upsertEntryQuery, claimAlertQuery, lookupBridgeContentQuery } = require("../src/sql");

test("lookupSourceQuery binds the alias, never interpolates it", () => {
	const q = lookupSourceQuery("tldr'; DROP TABLE sources;--");
	assert.match(q.sql, /FROM sources WHERE id = \?/);
	assert.deepEqual(q.params, ["tldr'; DROP TABLE sources;--"]);
});

test("upsertEntryQuery is INSERT OR REPLACE keyed on id, with every column bound", () => {
	const q = upsertEntryQuery({
		id: "h", sourceId: "s", canonicalUrl: null, permalinkUuid: "u", title: "t", readableContent: "<p>x</p>", publishedAt: 5,
	});
	assert.match(q.sql, /^INSERT OR REPLACE INTO entries/);
	assert.equal((q.sql.match(/\?/g) || []).length, 7);
	assert.deepEqual(q.params, ["h", "s", null, "u", "t", "<p>x</p>", 5]);
});

test("claimAlertQuery is an idempotent claim", () => {
	const q = claimAlertQuery("h", 99);
	assert.match(q.sql, /^INSERT OR IGNORE INTO ingest_alerts/);
	assert.deepEqual(q.params, ["h", 99]);
});

test("lookupBridgeContentQuery binds the permalink UUID, never interpolates it", () => {
	const q = lookupBridgeContentQuery("550e8400-e29b-41d4-a716-446655440000'; DROP TABLE entries;--");
	assert.match(q.sql, /SELECT readable_content FROM entries WHERE permalink_uuid = \?/);
	assert.deepEqual(q.params, ["550e8400-e29b-41d4-a716-446655440000'; DROP TABLE entries;--"]);
});
