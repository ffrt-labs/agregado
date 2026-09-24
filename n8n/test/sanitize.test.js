const test = require("node:test");
const assert = require("node:assert/strict");
const { sanitizeForPermalink, toReadableContent } = require("../src/sanitize");

const dirty = `<html><head><title>t</title><style>p{color:red}</style></head><body>
<script>alert(1)</script>
<p onclick="x()">Hello <a href="javascript:alert(1)">bad</a> <a href="https://e.com/a">good</a></p>
<img src="https://t.example.com/open.gif" width="1" height="1">
<img src="https://cdn.example.com/hero.png" width="600">
<iframe src="https://evil.example.com"></iframe>
<form action="/x"><input></form>
<h2>Heading</h2><ul><li>one</li></ul>
</body></html>`;

test("permalink policy strips scripts, handlers, javascript: links, frames, forms and beacons", () => {
	const out = sanitizeForPermalink(dirty);
	assert.doesNotMatch(out, /<script|onclick|javascript:|<iframe|<form|<input|open\.gif/i);
	assert.match(out, /https:\/\/e\.com\/a/);
	assert.match(out, /hero\.png/);
	assert.match(out, /color:red/, "fidelity: styles survive");
});

test("permalink policy adds a script-blocking CSP", () => {
	assert.match(sanitizeForPermalink("<p>x</p>"), /Content-Security-Policy[^>]*script-src 'none'/);
});

test("readable policy keeps reading structure and drops presentation", () => {
	const out = toReadableContent(dirty);
	assert.match(out, /<p>Hello/);
	assert.match(out, /<h2>Heading<\/h2>/);
	assert.match(out, /<li>one<\/li>/);
	assert.match(out, /href="https:\/\/e\.com\/a"/);
	assert.doesNotMatch(out, /<style|<script|<img|onclick|javascript:|color:red|alert\(1\)/i);
});

test("readable policy falls back to escaped plain text when html is empty", () => {
	assert.equal(toReadableContent("", "a <b>\n\nsecond"), "<p>a &lt;b&gt;</p><p>second</p>");
});

test("permalink policy drops remote stylesheets and srcset (viewer-IP leaks)", () => {
	const out = sanitizeForPermalink('<link rel="stylesheet" href="https://t.example.com/a.css"><img src="a.png" srcset="https://t.example.com/b.png 2x">');
	assert.doesNotMatch(out, /stylesheet|srcset|t\.example\.com/);
});
