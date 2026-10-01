const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { workflow } = require("../scripts/build-daily-digest");

const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;

test("every connection references existing nodes", () => {
	const names = new Set(workflow.nodes.map((n) => n.name));
	for (const [from, outputs] of Object.entries(workflow.connections)) {
		assert.ok(names.has(from), `unknown source ${from}`);
		for (const branch of outputs.main) for (const l of branch) assert.ok(names.has(l.node), `unknown target ${l.node}`);
	}
});

test("every Code node's bundled script is syntactically valid", () => {
	for (const n of workflow.nodes.filter((n) => n.type === "n8n-nodes-base.code")) {
		assert.doesNotThrow(() => new AsyncFunction("$input", "$", "$env", "require", n.parameters.jsCode), n.name);
	}
});

test("the trigger is a schedule, not a webhook: this workflow has no inbound HTTP surface", () => {
	assert.equal(workflow.nodes[0].type, "n8n-nodes-base.scheduleTrigger");
	assert.equal(workflow.nodes.some((n) => n.type === "n8n-nodes-base.webhook"), false);
});

test("the schedule matches DIGEST_SCHEDULE's default (internal/config/config.go)", () => {
	const trigger = workflow.nodes.find((n) => n.type === "n8n-nodes-base.scheduleTrigger");
	assert.equal(trigger.parameters.rule.interval[0].expression, "0 8 * * *");
});

test("Build digest request computes today's date via src/digest.js, not an ad-hoc date", async () => {
	const node = workflow.nodes.find((n) => n.name === "Build digest request");
	const fn = new AsyncFunction("$input", "$", "$env", "require", node.parameters.jsCode);
	const out = await fn({ first: () => ({ json: {} }) }, () => {}, { AGREGADO_BASE_URL: "https://agregado.example.com" }, require);
	assert.match(out[0].json.date, /^\d{4}-\d{2}-\d{2}$/);
	assert.equal(out[0].json.url, `https://agregado.example.com/api/private/digests/${out[0].json.date}`);
	assert.equal(out[0].json.attempt, 0);
});

test("Get digest sends the shared enrichment secret as X-Enrichment-Secret, reusing the article-enrichment credential", () => {
	const node = workflow.nodes.find((n) => n.name === "Get digest");
	assert.equal(node.parameters.method, "POST");
	assert.equal(node.parameters.url, "={{ $json.url }}");
	const header = node.parameters.headerParameters.parameters.find((p) => p.name === "X-Enrichment-Secret");
	assert.equal(header.value, "={{ $env.ENRICHMENT_SECRET }}");
});

test("Send digest email forwards the fetched artifact's Subject/HTML/Text verbatim, with no selection or rendering logic", () => {
	const node = workflow.nodes.find((n) => n.name === "Send digest email");
	assert.equal(node.type, "n8n-nodes-base.emailSend");
	assert.equal(node.parameters.subject, "={{ $json.Subject }}");
	assert.equal(node.parameters.html, "={{ $json.HTML }}");
	assert.equal(node.parameters.text, "={{ $json.Text }}");
	assert.equal(JSON.stringify(node).includes("@"), false, "no recipient address committed in the node");
});

test("Classify send failure counts attempts via its own prior run, recovering date/url from Build digest request", async () => {
	// A successful "Get digest" re-fetch (the retry target) replaces $json
	// with the HTTP response body, which carries no attempt field — so the
	// error item itself can never be trusted for attempt, only self-reference
	// to this node's last output can, which is what this test pins down.
	const node = workflow.nodes.find((n) => n.name === "Classify send failure");
	const fn = new AsyncFunction("$input", "$", "$env", "require", node.parameters.jsCode);
	const built = { date: "2026-09-14", url: "https://agregado.example.com/api/private/digests/2026-09-14" };

	let lastClassifyOutput = null;
	const dollar = (name) => {
		if (name === "Build digest request") return { first: () => ({ json: built }) };
		if (name === "Classify send failure") {
			if (!lastClassifyOutput) throw new Error("Classify send failure has not run in this execution");
			return { first: () => ({ json: lastClassifyOutput }) };
		}
		return { first: () => ({ json: {} }) };
	};

	// First failure: no prior run of this node exists yet in the execution.
	const first = await fn({ first: () => ({ json: { error: "ECONNREFUSED" } }) }, dollar, {}, require);
	assert.equal(first[0].json.attempt, 1);
	assert.equal(first[0].json.date, built.date);
	assert.equal(first[0].json.url, built.url);
	lastClassifyOutput = first[0].json;

	// Second failure, after a Get-digest re-fetch whose success wiped the
	// item's json (simulated here by the error item carrying none of it).
	const second = await fn({ first: () => ({ json: { error: "ECONNREFUSED" } }) }, dollar, {}, require);
	assert.equal(second[0].json.attempt, 2);
	lastClassifyOutput = second[0].json;

	const third = await fn({ first: () => ({ json: { error: "ECONNREFUSED" } }) }, dollar, {}, require);
	assert.equal(third[0].json.attempt, 3);
});

test("a retryable send failure loops back to Get digest (re-fetch), not directly back to Send digest email", () => {
	assert.equal(workflow.connections["Retry send?"].main[0][0].node, "Wait before retry");
	assert.equal(workflow.connections["Wait before retry"].main[0][0].node, "Get digest");
});

test("exhausted retries and a digest-fetch failure both reach the alert branch, which always ends in Fail execution", () => {
	assert.equal(workflow.connections["Digest fetch failed"].main[0][0].node, "Alert claim query");
	assert.equal(workflow.connections["Retry send?"].main[1][0].node, "Alert claim query");
	assert.equal(workflow.connections["First failure?"].main[1][0].node, "Fail execution");
	assert.equal(workflow.connections["Notify"].main[0][0].node, "Fail execution");
});

test("Build digest request fails the execution loudly on error rather than routing into a branch with nothing to report", () => {
	const node = workflow.nodes.find((n) => n.name === "Build digest request");
	assert.equal(node.onError, "stopWorkflow");
	// Only one output is wired: stopWorkflow nodes never produce an error-output item,
	// so a second connections entry would be unreachable dead wiring.
	assert.equal(workflow.connections["Build digest request"].main.length, 1);
});

test("a successful send has no further connection — it never loops back into the retry branch", () => {
	assert.deepEqual(workflow.connections["Send digest email"].main[0], []);
});

test("Alert claim query keys the alert on the digest date, so same-day failures claim once", async () => {
	const node = workflow.nodes.find((n) => n.name === "Alert claim query");
	const fn = new AsyncFunction("$input", "$", "$env", "require", node.parameters.jsCode);
	const out = await fn({ first: () => ({ json: { date: "2026-09-14", message: "boom" } }) }, () => {}, {}, require);
	assert.deepEqual(out[0].json.query.params, ["digest-2026-09-14", out[0].json.query.params[1]]);
});

test("no credentials, tokens, recipient address, or provider-specific configuration are committed in the workflow JSON", () => {
	const node = workflow.nodes.find((n) => n.name === "Send digest email");
	assert.equal(node.parameters.toEmail, "={{ $env.DIGEST_RECIPIENT_EMAIL }}");
	assert.equal(node.parameters.fromEmail, "={{ $env.DIGEST_FROM_EMAIL }}");
	assert.equal("credentials" in node, false);
	assert.equal(JSON.stringify(workflow).includes("credentials"), false);
});

test("committed workflow JSON matches the generator (run `npm run build`)", () => {
	const committed = fs.readFileSync(path.join(__dirname, "..", "workflows", "daily-digest.json"), "utf8");
	assert.equal(committed, JSON.stringify(workflow, null, 2) + "\n");
});
