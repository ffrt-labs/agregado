const test = require("node:test");
const assert = require("node:assert/strict");
const crypto = require("node:crypto");
const { verifySvix } = require("../src/svix");

const key = crypto.randomBytes(24);
const secret = `whsec_${key.toString("base64")}`;
const NOW = 1_800_000_000;

function sign(id, ts, body) {
	return "v1," + crypto.createHmac("sha256", key).update(`${id}.${ts}.${body}`).digest("base64");
}

function headersFor(body, ts = NOW) {
	return { "svix-id": "msg_1", "svix-timestamp": String(ts), "svix-signature": sign("msg_1", ts, body) };
}

test("accepts a correctly signed payload", () => {
	const body = '{"type":"email.received"}';
	assert.equal(verifySvix({ secret, headers: headersFor(body), rawBody: body, now: NOW }), true);
});

test("accepts when any of several space-separated signatures matches", () => {
	const body = "{}";
	const h = headersFor(body);
	h["svix-signature"] = `v1,AAAA ${h["svix-signature"]}`;
	assert.equal(verifySvix({ secret, headers: h, rawBody: body, now: NOW }), true);
});

test("rejects a tampered body", () => {
	const h = headersFor("{}");
	assert.equal(verifySvix({ secret, headers: h, rawBody: '{"x":1}', now: NOW }), false);
});

test("rejects missing headers", () => {
	assert.equal(verifySvix({ secret, headers: {}, rawBody: "{}", now: NOW }), false);
});

test("rejects a stale timestamp (replay)", () => {
	const body = "{}";
	assert.equal(verifySvix({ secret, headers: headersFor(body, NOW - 3600), rawBody: body, now: NOW }), false);
});

test("rejects a signature from another secret", () => {
	const other = `whsec_${crypto.randomBytes(24).toString("base64")}`;
	const body = "{}";
	assert.equal(verifySvix({ secret: other, headers: headersFor(body), rawBody: body, now: NOW }), false);
});

test("header names are matched case-insensitively", () => {
	const body = "{}";
	const h = Object.fromEntries(Object.entries(headersFor(body)).map(([k, v]) => [k.toUpperCase(), v]));
	assert.equal(verifySvix({ secret, headers: h, rawBody: body, now: NOW }), true);
});
