const test = require("node:test");
const assert = require("node:assert/strict");
const { digestDateKey, buildDigestRequest, emptyDigestAlert } = require("../src/digest");

test("digestDateKey formats as YYYY-MM-DD in UTC", () => {
	assert.equal(digestDateKey("2026-09-14T23:30:00Z"), "2026-09-14");
	assert.equal(digestDateKey("2026-09-14T00:00:00Z"), "2026-09-14");
});

test("digestDateKey uses the UTC day, not the local day, across a UTC midnight boundary", () => {
	// A local offset that would read as the previous/next day if the UTC
	// suffix were stripped instead of converted.
	assert.equal(digestDateKey("2026-09-14T23:30:00-05:00"), "2026-09-15");
	assert.equal(digestDateKey("2026-09-14T00:30:00+05:00"), "2026-09-13");
});

test("buildDigestRequest builds the persisted-artifact URL for today's date", () => {
	const req = buildDigestRequest("2026-09-14T08:00:00Z", "https://agregado.example.com");
	assert.deepEqual(req, { date: "2026-09-14", url: "https://agregado.example.com/api/private/digests/2026-09-14" });
});

test("buildDigestRequest strips a trailing slash from the base URL", () => {
	const req = buildDigestRequest("2026-09-14T08:00:00Z", "https://agregado.example.com/");
	assert.equal(req.url, "https://agregado.example.com/api/private/digests/2026-09-14");
});

test("buildDigestRequest is deterministic: a retry for the same instant rebuilds the identical request", () => {
	const a = buildDigestRequest("2026-09-14T08:00:00Z", "https://agregado.example.com");
	const b = buildDigestRequest("2026-09-14T08:00:00Z", "https://agregado.example.com");
	assert.deepEqual(a, b);
});

test("a missing base URL fails loudly rather than building a request against the literal host 'undefined'", () => {
	assert.throws(() => buildDigestRequest("2026-09-14T08:00:00Z", undefined), /AGREGADO_BASE_URL/);
	assert.throws(() => buildDigestRequest("2026-09-14T08:00:00Z", ""), /AGREGADO_BASE_URL/);
});

test("emptyDigestAlert is null for a Digest with Articles", () => {
	assert.equal(emptyDigestAlert({ SelectedCount: 3, EmptyReason: "" }, "2026-10-05"), null);
});

test("emptyDigestAlert carries Agregado's reason for an empty Digest", () => {
	const alert = emptyDigestAlert({ SelectedCount: 0, EmptyReason: "No Articles finished Enrichment between Oct 4 13:00 and Oct 5 13:00." }, "2026-10-05");
	assert.deepEqual(alert, { date: "2026-10-05", message: "empty: No Articles finished Enrichment between Oct 4 13:00 and Oct 5 13:00." });
});

test("emptyDigestAlert still alerts when an older Agregado sends no reason", () => {
	assert.deepEqual(emptyDigestAlert({ SelectedCount: 0 }, "2026-10-05"), { date: "2026-10-05", message: "empty: no reason given" });
});
