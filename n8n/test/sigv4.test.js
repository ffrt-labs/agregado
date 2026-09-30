const test = require("node:test");
const assert = require("node:assert/strict");
const { signS3Put } = require("../src/sigv4");

// Ground truth computed independently via openssl (canonical request, HMAC
// key-derivation chain, and final signature by hand), not by re-running this
// module — so this catches formatting bugs (line order, missing blank line,
// header casing) that a test using the same code to check itself would miss.
const FIXED = {
	accessKeyId: "AKIDTEST",
	secretAccessKey: "secretkeytest",
	region: "auto",
	host: "abc123.r2.cloudflarestorage.com",
	bucket: "test-bucket",
	key: "abc.html",
	body: "hello world",
	contentType: "text/html; charset=utf-8",
	now: new Date("2024-01-02T03:04:05Z"),
};

const EXPECTED_PAYLOAD_HASH = "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9";
const EXPECTED_SIGNATURE = "287b5e86d4feae2ece8a51954164850de32bebb9ed21d03e498b3a865737eeaa";
const EXPECTED_AUTHORIZATION =
	"AWS4-HMAC-SHA256 Credential=AKIDTEST/20240102/auto/s3/aws4_request, SignedHeaders=content-type;host;x-amz-content-sha256;x-amz-date, Signature=" +
	EXPECTED_SIGNATURE;

test("signS3Put matches an independently computed signature", () => {
	const { url, headers } = signS3Put(FIXED);
	assert.equal(url, "https://abc123.r2.cloudflarestorage.com/test-bucket/abc.html");
	assert.equal(headers["x-amz-content-sha256"], EXPECTED_PAYLOAD_HASH);
	assert.equal(headers["x-amz-date"], "20240102T030405Z");
	assert.equal(headers["Content-Type"], "text/html; charset=utf-8");
	assert.equal(headers.Authorization, EXPECTED_AUTHORIZATION);
});

test("signature changes if the body changes", () => {
	const a = signS3Put(FIXED);
	const b = signS3Put({ ...FIXED, body: "hello world!" });
	assert.notEqual(a.headers["x-amz-content-sha256"], b.headers["x-amz-content-sha256"]);
	assert.notEqual(a.headers.Authorization, b.headers.Authorization);
});

test("signature changes if the secret changes", () => {
	const a = signS3Put(FIXED);
	const b = signS3Put({ ...FIXED, secretAccessKey: "different" });
	assert.notEqual(a.headers.Authorization, b.headers.Authorization);
});

test("URL-encodes the object key", () => {
	const { url } = signS3Put({ ...FIXED, key: "entries/a b.html" });
	assert.equal(url, "https://abc123.r2.cloudflarestorage.com/test-bucket/entries%2Fa%20b.html");
});

test("refuses to sign without credentials", () => {
	assert.throws(() => signS3Put({ ...FIXED, accessKeyId: "" }), /access key/);
	assert.throws(() => signS3Put({ ...FIXED, secretAccessKey: "" }), /access key/);
});
