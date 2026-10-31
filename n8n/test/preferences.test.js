const test = require("node:test");
const assert = require("node:assert/strict");
const { buildRequests, normalize, describeChange, failureMessage } = require("../src/preferences");

const env = { PREFERENCES_DOC_ID: "doc123", AGREGADO_BASE_URL: "https://agregado.example/" };

test("buildRequests targets the Drive Markdown export/upload and Agregado's private endpoints", () => {
	assert.deepEqual(buildRequests(env), {
		exportUrl: "https://www.googleapis.com/drive/v3/files/doc123/export?mimeType=text%2Fmarkdown",
		uploadUrl: "https://www.googleapis.com/upload/drive/v3/files/doc123?uploadType=media",
		storeUrl: "https://agregado.example/api/private/preferences",
		regenerateUrl: "https://agregado.example/api/private/preferences/regenerate",
	});
});

test("buildRequests fails loudly on missing configuration", () => {
	assert.throws(() => buildRequests({ ...env, PREFERENCES_DOC_ID: "" }), /PREFERENCES_DOC_ID/);
	assert.throws(() => buildRequests({ ...env, AGREGADO_BASE_URL: undefined }), /AGREGADO_BASE_URL/);
});

test("normalize matches Go's: CRLF and the trailing spaces Docs adds to list items go", () => {
	assert.equal(normalize("# PREFERENCES\r\n\r\n- a  \r\n- b\t\n\n"), "# PREFERENCES\n\n- a\n- b\n");
});

const before = "# PREFERENCES\n\n## Topics I want\n\n- Go  \n- (example: x)\n\n## Topics I skip\n\n- crypto\n";

test("describeChange lists added and removed bullets per section", () => {
	const after = "# PREFERENCES\n\n## Topics I want\n\n- Go\n- Homelab networking\n\n## Topics I skip\n\n- crypto\n";
	assert.equal(describeChange(before, after), "+ Topics I want: Homelab networking\n- Topics I want: (example: x)");
});

test("describeChange says so when only wording outside the bullets changed", () => {
	assert.equal(describeChange(before, before.replace("# PREFERENCES\n", "# PREFERENCES\n\nIntro.\n")), "wording changed only");
});

test("describeChange stays within one notification", () => {
	const many = "# PREFERENCES\n\n## Topics I want\n\n" + Array.from({ length: 100 }, (_, i) => `- topic number ${i}`).join("\n");
	const out = describeChange("# PREFERENCES\n", many, 200);
	assert.ok(out.length < 230, out);
	assert.match(out, /… and \d+ more$/);
});

test("a 400 from Store profile tells the reader to fix the Doc", () => {
	const msg = failureMessage("Store profile", { error: { message: "400 - missing section", httpCode: "400" } });
	assert.match(msg, /Store profile/);
	assert.match(msg, /Fix the PREFERENCES Doc/);
	assert.doesNotMatch(failureMessage("Regenerate", { error: { message: "502" } }), /Fix the PREFERENCES Doc/);
});
