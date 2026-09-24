const test = require("node:test");
const assert = require("node:assert/strict");
const { planAssets, inlineAssets } = require("../src/assets");

const html = `<body>
<img src="https://cdn.example.com/hero.png">
<img src="https://t.example.com/pixel.gif">
<img src="https://t.example.com/o.gif" width="1" height="1">
<img src="data:image/png;base64,AAAA">
<img src="cid:logo">
<img src="https://cdn.example.com/hero.png">
</body>`;

test("planAssets lists each remote http(s) image once, skipping beacons, data: and cid:", () => {
	assert.deepEqual(planAssets(html), ["https://cdn.example.com/hero.png"]);
});

test("inlineAssets swaps fetched assets to data URIs and leaves failed ones untouched", async () => {
	const fetchAsset = async (url) => ({ contentType: "image/png", bytes: Buffer.from("PNG") });
	const out = await inlineAssets(html, fetchAsset);
	assert.match(out.html, /data:image\/png;base64,UE5H/);
	assert.doesNotMatch(out.html, /hero\.png/);
	assert.deepEqual(out.failed, []);
});

test("a failing asset fetch is skipped and reported, never thrown", async () => {
	const fetchAsset = async () => {
		throw new Error("404");
	};
	const out = await inlineAssets(html, fetchAsset);
	assert.deepEqual(out.failed, ["https://cdn.example.com/hero.png"]);
	assert.match(out.html, /hero\.png/);
});

test("non-image responses are treated as failures", async () => {
	const fetchAsset = async () => ({ contentType: "text/html", bytes: Buffer.from("<x>") });
	const out = await inlineAssets(html, fetchAsset);
	assert.deepEqual(out.failed, ["https://cdn.example.com/hero.png"]);
});
