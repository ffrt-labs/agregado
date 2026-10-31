// Pure helpers for the preferences-sync workflow (agregado#191, ADR-0010).
// The Google Doc PREFERENCES is authoritative; this workflow exports it as
// Markdown, hands it to Agregado (PUT /api/private/preferences), asks Agregado
// to regenerate it from the Reader's votes and Opens, and writes a changed
// result back to the Doc. No I/O here, so it is unit-testable.

const DRIVE_API = "https://www.googleapis.com/drive/v3/files";
const DRIVE_UPLOAD_API = "https://www.googleapis.com/upload/drive/v3/files";

function buildRequests(env) {
	const docId = env.PREFERENCES_DOC_ID;
	const baseUrl = env.AGREGADO_BASE_URL;
	if (!docId) throw new Error("PREFERENCES_DOC_ID is not set");
	if (!baseUrl) throw new Error("AGREGADO_BASE_URL is not set");
	const base = String(baseUrl).replace(/\/+$/, "");
	const id = encodeURIComponent(docId);
	return {
		// Drive exports a native Google Doc as Markdown; the upload below
		// converts Markdown back into the same Doc, keeping its ID and history.
		exportUrl: `${DRIVE_API}/${id}/export?mimeType=text%2Fmarkdown`,
		uploadUrl: `${DRIVE_UPLOAD_API}/${id}?uploadType=media`,
		storeUrl: `${base}/api/private/preferences`,
		regenerateUrl: `${base}/api/private/preferences/regenerate`,
	};
}

// Mirrors preferences.Normalize in Go: Docs appends two spaces to list items.
function normalize(text) {
	return String(text || "")
		.replace(/\r\n/g, "\n")
		.split("\n")
		.map((line) => line.replace(/[ \t]+$/, ""))
		.join("\n")
		.trim() + "\n";
}

// bulletsBySection maps each "## " section to its bullet texts.
function bulletsBySection(text) {
	const sections = {};
	let current = null;
	for (const line of normalize(text).split("\n")) {
		if (line.startsWith("## ")) {
			current = line.slice(3).trim();
			sections[current] = sections[current] || [];
		} else if (current && /^[-*] /.test(line)) {
			sections[current].push(line.slice(2).trim());
		}
	}
	return sections;
}

// describeChange lists the bullets the regeneration added and removed, so a
// bad rewrite is noticed from the notification alone. Capped to fit one
// notification; the Doc's version history has the full diff.
function describeChange(before, after, maxChars = 900) {
	const a = bulletsBySection(before);
	const b = bulletsBySection(after);
	const lines = [];
	for (const section of new Set([...Object.keys(a), ...Object.keys(b)])) {
		const was = new Set(a[section] || []);
		const now = new Set(b[section] || []);
		for (const bullet of now) if (!was.has(bullet)) lines.push(`+ ${section}: ${bullet}`);
		for (const bullet of was) if (!now.has(bullet)) lines.push(`- ${section}: ${bullet}`);
	}
	if (lines.length === 0) return "wording changed only";
	let out = "";
	for (const [i, line] of lines.entries()) {
		if (out.length + line.length + 1 > maxChars) return `${out}… and ${lines.length - i} more`;
		out += (out ? "\n" : "") + line;
	}
	return out;
}

// failureMessage turns an HTTP node's error item into one line. A 400 from
// the store step means the Doc itself is malformed, which the reader fixes.
function failureMessage(step, item) {
	const err = (item && item.error) || item || {};
	const detail = err.description || err.message || (typeof err === "string" ? err : "") || "unknown error";
	const hint = step === "Store profile" && /400/.test(String(err.httpCode || err.message || ""))
		? " Fix the PREFERENCES Doc: keep the title and the four bullet-only sections."
		: "";
	return `Preferences sync failed at "${step}": ${detail}.${hint}`;
}

module.exports = { buildRequests, normalize, bulletsBySection, describeChange, failureMessage };
