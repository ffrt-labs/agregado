const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { resolveCanonicalUrl, isCanonicalCandidate } = require("../src/extract");

const fixture = (n) => JSON.parse(fs.readFileSync(path.join(__dirname, "fixtures", n), "utf8"));

test("ADR-0004 fixture 01: Archived-At header wins over the scraped link", () => {
	const f = fixture("01-archived-at-header.json");
	assert.equal(resolveCanonicalUrl(f.headers, f.html), "https://dispatch.example.com/p/weekly-dispatch-42");
});

test("ADR-0004 fixture 02: scrapes view-in-browser, rejects pixel and share links", () => {
	const f = fixture("02-view-in-browser.json");
	assert.equal(resolveCanonicalUrl(f.headers, f.html), "https://overflow.example.com/p/issue-108");
});

test("ADR-0004 fixture 03: nothing recoverable yields null", () => {
	const f = fixture("03-no-web-home.json");
	assert.equal(resolveCanonicalUrl(f.headers, f.html), null);
});

test("an unusable Archived-At falls through to the scrape", () => {
	const html = '<a href="https://x.example.com/p/1">Read online</a>';
	assert.equal(resolveCanonicalUrl({ "archived-at": "<mailto:a@b.c>" }, html), "https://x.example.com/p/1");
});

test("empty html and no header yields null", () => {
	assert.equal(resolveCanonicalUrl({}, ""), null);
	assert.equal(resolveCanonicalUrl({}, undefined), null);
});

test("a view-in-browser anchor whose text says unsubscribe is ignored", () => {
	assert.equal(resolveCanonicalUrl({}, '<a href="https://x.example.com/a">view online / unsubscribe</a>'), null);
});

test("isCanonicalCandidate", () => {
	const cases = [
		["https://example.com/p/1", true],
		["http://example.com/p/1", true],
		["https://example.com/pixel-art-in-css", true],
		["https://example.com/unsubscribe?u=1", false],
		["https://t.example.com/pixel.gif", false],
		["mailto:a@b.c", false],
		["/relative/path", false],
		["ftp://example.com/x", false],
		["https://twitter.com/intent/tweet?url=x", false],
		["https://www.facebook.com/sharer/sharer.php", false],
		["https://example.com/share?u=1", false],
		["not a url", false],
	];
	for (const [url, want] of cases) assert.equal(isCanonicalCandidate(url), want, url);
});
