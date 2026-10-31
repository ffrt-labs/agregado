const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { workflow } = require("../scripts/build-preferences-sync");

const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
const node = (name) => workflow.nodes.find((n) => n.name === name);

test("every connection references existing nodes", () => {
	const names = new Set(workflow.nodes.map((n) => n.name));
	for (const [from, outputs] of Object.entries(workflow.connections)) {
		assert.ok(names.has(from), `unknown source ${from}`);
		for (const branch of outputs.main) for (const l of branch) assert.ok(names.has(l.node), `unknown target ${l.node}`);
	}
});

test("every Code node's bundled script is syntactically valid", () => {
	for (const n of workflow.nodes.filter((n) => n.type === "n8n-nodes-base.code")) {
		assert.doesNotThrow(() => new AsyncFunction("$input", "$", "$env", "$prevNode", "require", n.parameters.jsCode), n.name);
	}
});

test("it runs nightly at 06:30 Berlin, before the 08:00 Digest, with no inbound HTTP surface", () => {
	assert.equal(workflow.nodes[0].type, "n8n-nodes-base.scheduleTrigger");
	assert.equal(workflow.nodes[0].parameters.rule.interval[0].expression, "30 6 * * *");
	assert.equal(workflow.settings.timezone, "Europe/Berlin");
	assert.equal(workflow.nodes.some((n) => n.type === "n8n-nodes-base.webhook"), false);
});

test("the Doc is stored before regeneration, so a hand edit applies even if regeneration fails", () => {
	assert.equal(workflow.connections["Export Doc"].main[0][0].node, "Store profile");
	assert.equal(workflow.connections["Store profile"].main[0][0].node, "Regenerate");
});

test("Google calls use the Google Drive OAuth2 credential; Agregado calls use the shared secret", () => {
	for (const name of ["Export Doc", "Write Doc"]) assert.equal(node(name).parameters.nodeCredentialType, "googleDriveOAuth2Api", name);
	for (const name of ["Store profile", "Regenerate"]) {
		const header = node(name).parameters.headerParameters.parameters.find((p) => p.name === "X-Enrichment-Secret");
		assert.equal(header.value, "={{ $env.ENRICHMENT_SECRET }}", name);
	}
	assert.equal(node("Store profile").parameters.method, "PUT");
	assert.equal(node("Write Doc").parameters.method, "PATCH");
});

test("every external call routes its failure to the alert", () => {
	for (const name of ["Export Doc", "Store profile", "Regenerate", "Write Doc"]) {
		assert.equal(node(name).onError, "continueErrorOutput", name);
		assert.equal(workflow.connections[name].main[1][0].node, "Describe failure", name);
	}
});

test("Describe change ends the run when the profile did not change", async () => {
	const fn = new AsyncFunction("$input", "$", "$env", "require", node("Describe change").parameters.jsCode);
	const out = await fn({ first: () => ({ json: { changed: false, profile: "x" } }) }, () => ({ first: () => ({ json: { markdown: "x" } }) }), {}, require);
	assert.deepEqual(out, []);
});

test("Describe change reports added bullets when the profile changed", async () => {
	const before = "# PREFERENCES\n\n## Topics I want\n\n- Go\n";
	const after = "# PREFERENCES\n\n## Topics I want\n\n- Go\n- Rust\n";
	const fn = new AsyncFunction("$input", "$", "$env", "require", node("Describe change").parameters.jsCode);
	const out = await fn({ first: () => ({ json: { changed: true, profile: after, signals: 4 } }) }, () => ({ first: () => ({ json: { markdown: before } }) }), {}, require);
	assert.deepEqual(out[0].json, { profile: after, signals: 4, change: "+ Topics I want: Rust" });
});

test("the committed workflow JSON is up to date with the generator", () => {
	const committed = fs.readFileSync(path.join(__dirname, "..", "workflows", "preferences-sync.json"), "utf8");
	assert.equal(committed, JSON.stringify(workflow, null, 2) + "\n");
});
