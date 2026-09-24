// Two policies off one cheerio pass, per the plan: the permalink keeps
// fidelity (original look, subresources), the feed <content> keeps signal
// (reading structure only). Click-tracking wrappers are never followed here.
const cheerio = require("cheerio");
const { isBeacon } = require("./assets");

const UNSAFE = "script, iframe, object, embed, form, input, button, textarea, select, base, meta[http-equiv], link[rel=import], link[rel=stylesheet]";

function escapeHtml(s) {
	return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function stripUnsafe($) {
	$(UNSAFE).remove();
	$("*").each((_, el) => {
		for (const name of Object.keys(el.attribs || {})) {
			const value = String(el.attribs[name]).trim().toLowerCase();
			if (name.startsWith("on")) $(el).removeAttr(name);
			else if (["href", "src", "action", "xlink:href"].includes(name) && /^(javascript|vbscript):/.test(value)) $(el).removeAttr(name);
		}
	});
	$("img, source").removeAttr("srcset");
	$("img").each((_, el) => {
		if (isBeacon(el, $)) $(el).remove();
	});
}

function sanitizeForPermalink(html) {
	const $ = cheerio.load(html || "");
	stripUnsafe($);
	// The permalink is public: forbid scripts even if a sanitizer gap remains.
	$("head").prepend(`<meta http-equiv="Content-Security-Policy" content="script-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'">`);
	return $.html();
}

const READABLE_TAGS = new Set(["p", "h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "li", "blockquote", "pre", "code", "strong", "b", "em", "i", "br", "hr", "a"]);

function toReadableContent(html, text) {
	if (!html || !html.trim()) return plainTextToHtml(text);
	const $ = cheerio.load(html);
	stripUnsafe($);
	$("style, head, img, svg, picture, video, audio, noscript").remove();
	const out = [];
	(function walk(nodes) {
		for (const node of nodes) {
			if (node.type === "text") out.push(escapeHtml(node.data));
			else if (node.type === "tag") {
				if (READABLE_TAGS.has(node.name)) {
					if (["br", "hr"].includes(node.name)) out.push(`<${node.name}>`);
					else {
						const href = node.name === "a" ? ($(node).attr("href") || "").trim() : "";
						const attr = href ? ` href="${href.replace(/&/g, "&amp;").replace(/"/g, "&quot;")}"` : "";
						out.push(`<${node.name}${attr}>`);
						walk(node.children || []);
						out.push(`</${node.name}>`);
					}
				} else walk(node.children || []);
			}
		}
	})($("body").length ? $("body").contents().toArray() : $.root().contents().toArray());
	const result = out.join("").replace(/\s+/g, " ").trim();
	return result || plainTextToHtml(text);
}

function plainTextToHtml(text) {
	return String(text || "")
		.split(/\n\s*\n/)
		.map((p) => p.trim())
		.filter(Boolean)
		.map((p) => `<p>${escapeHtml(p)}</p>`)
		.join("");
}

module.exports = { sanitizeForPermalink, toReadableContent };
