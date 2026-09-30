const test = require("node:test");
const assert = require("node:assert/strict");
const crypto = require("node:crypto");
const { verifyMinifluxSignature, parseNewEntries, matchBridgePermalink, classifyEnrichFailure } = require("../src/miniflux");

const secret = "miniflux-shared-secret";
const sign = (body) => crypto.createHmac("sha256", secret).update(body, "utf8").digest("hex");

test("verifyMinifluxSignature accepts a correctly signed body", () => {
	const body = '{"event_type":"new_entries"}';
	assert.equal(verifyMinifluxSignature({ secret, signatureHeader: sign(body), rawBody: body }), true);
});

test("verifyMinifluxSignature rejects a tampered body", () => {
	const body = '{"event_type":"new_entries"}';
	assert.equal(verifyMinifluxSignature({ secret, signatureHeader: sign(body), rawBody: '{"event_type":"x"}' }), false);
});

test("verifyMinifluxSignature rejects a signature from another secret", () => {
	const body = "{}";
	const other = crypto.createHmac("sha256", "another-secret").update(body, "utf8").digest("hex");
	assert.equal(verifyMinifluxSignature({ secret, signatureHeader: other, rawBody: body }), false);
});

test("verifyMinifluxSignature rejects missing header or body", () => {
	assert.equal(verifyMinifluxSignature({ secret, signatureHeader: "", rawBody: "{}" }), false);
	assert.equal(verifyMinifluxSignature({ secret, signatureHeader: "abc", rawBody: undefined }), false);
});

test("parseNewEntries ignores event types other than new_entries", () => {
	assert.deepEqual(parseNewEntries({ event_type: "save_entry", entries: [{ id: 1 }] }), []);
	assert.deepEqual(parseNewEntries(null), []);
});

test("parseNewEntries maps every entry, dropping empty optional fields", () => {
	const out = parseNewEntries({
		event_type: "new_entries",
		entries: [
			{ id: 1, url: "https://example.com/a", title: "A", author: "Jo", published_at: "2026-09-14T12:00:00Z" },
			{ id: 2, url: "https://example.com/b", title: "B" },
		],
	});
	assert.deepEqual(out, [
		{ id: 1, url: "https://example.com/a", title: "A", author: "Jo", publishedAt: "2026-09-14T12:00:00Z" },
		{ id: 2, url: "https://example.com/b", title: "B", author: undefined, publishedAt: undefined },
	]);
});

test("matchBridgePermalink recognises {bridge origin}/p/{uuid} case-insensitively", () => {
	const uuid = "550e8400-e29b-41d4-a716-446655440000";
	assert.equal(matchBridgePermalink(`https://bridge.example.com/p/${uuid}`, "https://bridge.example.com"), uuid);
	assert.equal(matchBridgePermalink(`https://bridge.example.com/p/${uuid.toUpperCase()}`, "https://bridge.example.com"), uuid);
});

test("matchBridgePermalink rejects ordinary Article URLs and other hosts", () => {
	const uuid = "550e8400-e29b-41d4-a716-446655440000";
	assert.equal(matchBridgePermalink("https://example.com/articles/a-real-article", "https://bridge.example.com"), null);
	assert.equal(matchBridgePermalink(`https://not-the-bridge.com/p/${uuid}`, "https://bridge.example.com"), null);
	assert.equal(matchBridgePermalink(null, "https://bridge.example.com"), null);
});

test("matchBridgePermalink tolerates a trailing slash on the configured origin", () => {
	const uuid = "550e8400-e29b-41d4-a716-446655440000";
	assert.equal(matchBridgePermalink(`https://bridge.example.com/p/${uuid}`, "https://bridge.example.com/"), uuid);
});

test("classifyEnrichFailure: a 4xx in the error message is terminal", () => {
	assert.equal(classifyEnrichFailure("Request failed with status code 400"), "terminal");
	assert.equal(classifyEnrichFailure("404 Not Found"), "terminal");
});

test("classifyEnrichFailure: anything else, including 5xx and network errors, is retryable", () => {
	assert.equal(classifyEnrichFailure("Request failed with status code 500"), "retryable");
	assert.equal(classifyEnrichFailure("connect ETIMEDOUT"), "retryable");
	assert.equal(classifyEnrichFailure(""), "retryable");
});
