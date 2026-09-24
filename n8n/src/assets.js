// Subresource capture: inline every remote image into the permalink HTML as a
// data: URI so the permalink stays self-contained if the publisher's CDN
// rots. Beacons are excluded. One asset failing never fails ingestion
// (agregado#113) — it is reported and left as-is.
const cheerio = require("cheerio");

const MAX_ASSET_BYTES = 2 * 1024 * 1024;

function isBeacon(img, $) {
	const w = $(img).attr("width");
	const h = $(img).attr("height");
	const src = ($(img).attr("src") || "").split("?")[0];
	return (w !== undefined && h !== undefined && Number(w) <= 1 && Number(h) <= 1) || /(^|[/.])pixel([/.]|$)/i.test(src);
}

function planAssets(html) {
	const $ = cheerio.load(html || "");
	const urls = new Set();
	$("img[src]").each((_, el) => {
		const src = ($(el).attr("src") || "").trim();
		if (/^https?:\/\//i.test(src) && !isBeacon(el, $)) urls.add(src);
	});
	return [...urls];
}

async function inlineAssets(html, fetchAsset) {
	const $ = cheerio.load(html || "");
	const failed = [];
	const dataUris = new Map();
	for (const url of planAssets(html)) {
		try {
			const { contentType, bytes } = await fetchAsset(url);
			if (!/^image\//i.test(contentType || "") || bytes.length > MAX_ASSET_BYTES) throw new Error("not an inlineable image");
			dataUris.set(url, `data:${contentType.split(";")[0]};base64,${Buffer.from(bytes).toString("base64")}`);
		} catch {
			failed.push(url);
		}
	}
	$("img[src]").each((_, el) => {
		const uri = dataUris.get(($(el).attr("src") || "").trim());
		if (uri) $(el).attr("src", uri);
	});
	return { html: $.html(), failed };
}

module.exports = { planAssets, inlineAssets };
