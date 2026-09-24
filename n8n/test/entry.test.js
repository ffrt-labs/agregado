const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { buildEntry, gateSource } = require("../src/entry");

const fixture = (n) => JSON.parse(fs.readFileSync(path.join(__dirname, "fixtures", n), "utf8"));
const NOW = 1_800_000_000;
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

function email(name, extra = {}) {
	const f = fixture(name);
	return { headers: { "message-id": "<m1@example.com>", ...f.headers, ...extra }, subject: f.subject, html: f.html, text: f.text };
}

test("with a canonical URL: entry carries it and still gets a permalink UUID", () => {
	const e = buildEntry({ email: email("01-archived-at-header.json"), sourceId: "dispatch", secret: "s", now: NOW });
	assert.equal(e.canonicalUrl, "https://dispatch.example.com/p/weekly-dispatch-42");
	assert.match(e.permalinkUuid, UUID_RE);
	assert.equal(e.sourceId, "dispatch");
	assert.equal(e.title, fixture("01-archived-at-header.json").subject);
});

test("without a canonical URL: null canonical, permalink UUID, original HTML kept for R2", () => {
	const e = buildEntry({ email: email("03-no-web-home.json"), sourceId: "plain", secret: "s", now: NOW });
	assert.equal(e.canonicalUrl, null);
	assert.match(e.permalinkUuid, UUID_RE);
	assert.match(e.permalinkHtml, /no Archived-At header/);
	assert.match(e.readableContent, /<p>This issue has no/);
});

test("identity is derived from Message-ID so a retry yields the identical entry", () => {
	const a = buildEntry({ email: email("03-no-web-home.json"), sourceId: "p", secret: "s", now: NOW });
	const b = buildEntry({ email: email("03-no-web-home.json"), sourceId: "p", secret: "s", now: NOW + 500 });
	assert.equal(a.id, b.id);
	assert.equal(a.permalinkUuid, b.permalinkUuid);
});

test("publishedAt uses the Date header, falling back to now", () => {
	const withDate = buildEntry({ email: email("03-no-web-home.json", { date: "Tue, 15 Sep 2026 12:00:00 +0000" }), sourceId: "p", secret: "s", now: NOW });
	assert.equal(withDate.publishedAt, Date.parse("2026-09-15T12:00:00Z") / 1000);
	const without = buildEntry({ email: email("03-no-web-home.json"), sourceId: "p", secret: "s", now: NOW });
	assert.equal(without.publishedAt, NOW);
});

test("a message with no Message-ID fails loudly", () => {
	const em = email("03-no-web-home.json");
	delete em.headers["message-id"];
	assert.throws(() => buildEntry({ email: em, sourceId: "p", secret: "s", now: NOW }), /Message-ID/);
});

test("a message with neither html nor text fails loudly", () => {
	const em = { headers: { "message-id": "<m@x>" }, subject: "s", html: "", text: "" };
	assert.throws(() => buildEntry({ email: em, sourceId: "p", secret: "s", now: NOW }), /content/);
});

test("gateSource: unknown and pending are no-ops, active proceeds", () => {
	assert.equal(gateSource(undefined), "skip");
	assert.equal(gateSource({ status: "pending" }), "skip");
	assert.equal(gateSource({ status: "active" }), "proceed");
});

test("recipient alias is the lowercased local-part of the first To address", () => {
	const { aliasFromRecipients } = require("../src/entry");
	assert.equal(aliasFromRecipients(["The Dispatch <TLDR@read.example.com>"]), "tldr");
	assert.equal(aliasFromRecipients(["a+tag@x.com"]), "a+tag");
	assert.equal(aliasFromRecipients([]), null);
});
