const test = require("node:test");
const assert = require("node:assert/strict");
const { entryId, permalinkUuid } = require("../src/identity");

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

test("entryId is stable and ignores angle brackets and whitespace", () => {
	assert.equal(entryId("<abc@example.com>"), entryId("  abc@example.com "));
	assert.match(entryId("abc@example.com"), /^[0-9a-f]{64}$/);
});

test("entryId differs per Message-ID", () => {
	assert.notEqual(entryId("a@x"), entryId("b@x"));
});

test("entryId rejects a missing Message-ID", () => {
	assert.throws(() => entryId(""), /Message-ID/);
	assert.throws(() => entryId(undefined), /Message-ID/);
});

test("permalinkUuid is a deterministic UUID for a given id and secret", () => {
	const id = entryId("a@x");
	assert.match(permalinkUuid(id, "s3cret"), UUID_RE);
	assert.equal(permalinkUuid(id, "s3cret"), permalinkUuid(id, "s3cret"));
});

test("permalinkUuid cannot be recomputed without the secret", () => {
	const id = entryId("a@x");
	assert.notEqual(permalinkUuid(id, "one"), permalinkUuid(id, "two"));
});

test("permalinkUuid refuses an empty secret", () => {
	assert.throws(() => permalinkUuid(entryId("a@x"), ""), /secret/);
});
