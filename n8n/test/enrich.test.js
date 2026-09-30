const test = require("node:test");
const assert = require("node:assert/strict");
const { buildEnrichRequest } = require("../src/enrich");

const entry = { id: 42, url: "https://example.com/articles/a-real-article", title: "A real Article" };

test("ordinary RSS entry: canonical_url from the entry, no bridge_content", () => {
	const body = buildEnrichRequest(entry);
	assert.deepEqual(body, { entry_id: 42, canonical_url: entry.url, title: "A real Article" });
	assert.ok(!("bridge_content" in body));
});

test("bridge-served entry: bridge_content is populated", () => {
	const body = buildEnrichRequest(entry, "<p>Recovered readable content</p>");
	assert.equal(body.bridge_content, "<p>Recovered readable content</p>");
});

test("optional fields are included only when present", () => {
	const body = buildEnrichRequest({ ...entry, author: "Jo", publishedAt: "2026-09-14T12:00:00Z" });
	assert.equal(body.author, "Jo");
	assert.equal(body.published_at, "2026-09-14T12:00:00Z");
});

test("a missing title falls back to a placeholder, matching the API's required field", () => {
	const body = buildEnrichRequest({ id: 1, url: "https://example.com/a" });
	assert.equal(body.title, "(no title)");
});

test("an entry with no Miniflux id or url fails loudly", () => {
	assert.throws(() => buildEnrichRequest({ url: "https://example.com/a" }), /id/);
	assert.throws(() => buildEnrichRequest({ id: 1 }), /url/);
});

test("empty bridge content is treated the same as absent", () => {
	const body = buildEnrichRequest(entry, "");
	assert.ok(!("bridge_content" in body));
});
