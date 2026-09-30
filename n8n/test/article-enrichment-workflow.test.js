const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { workflow } = require("../scripts/build-article-enrichment");

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

test("Parse entries fans a Miniflux new_entries payload out into one item per entry", async () => {
	const node = workflow.nodes.find((n) => n.name === "Parse entries");
	const fn = new AsyncFunction("$input", "$", "$env", "require", node.parameters.jsCode);
	const payload = {
		event_type: "new_entries",
		entries: [
			{ id: 1, url: "https://example.com/a", title: "A" },
			{ id: 2, url: "https://example.com/b", title: "B" },
		],
	};
	const out = await fn({ first: () => ({ json: { payload } }) }, () => {}, {}, require);
	assert.equal(out.length, 2);
	assert.equal(out[0].json.entry.id, 1);
	assert.equal(out[1].json.entry.id, 2);
});

test("Build enrich request produces the enrich endpoint's contract, with and without bridge_content", async () => {
	const node = workflow.nodes.find((n) => n.name === "Build enrich request");
	const fn = new AsyncFunction("$input", "$", "$env", "require", node.parameters.jsCode);

	const ordinary = await fn(
		{ first: () => ({ json: { entry: { id: 1, url: "https://example.com/a", title: "A" } } }) },
		() => {},
		{},
		require
	);
	assert.deepEqual(ordinary[0].json.body, { entry_id: 1, canonical_url: "https://example.com/a", title: "A" });

	const bridged = await fn(
		{ first: () => ({ json: { entry: { id: 2, url: "https://bridge.example.com/p/uuid", title: "B" }, bridgeContent: "<p>x</p>" } }) },
		() => {},
		{},
		require
	);
	assert.equal(bridged[0].json.body.bridge_content, "<p>x</p>");
});

test("Classify enrich failure recovers entry/body from Build enrich request and buckets the failure", async () => {
	const node = workflow.nodes.find((n) => n.name === "Classify enrich failure");
	const fn = new AsyncFunction("$input", "$", "$env", "require", node.parameters.jsCode);
	const built = { entry: { id: 9, url: "https://example.com/a", title: "A" }, body: { entry_id: 9 } };
	const dollar = (name) => (name === "Build enrich request" ? { first: () => ({ json: built }) } : { first: () => ({ json: {} }) });

	const retryable = await fn({ first: () => ({ json: { error: "connect ETIMEDOUT" } }) }, dollar, {}, require);
	assert.equal(retryable[0].json.classification, "retryable");
	assert.equal(retryable[0].json.attempt, 1);
	assert.deepEqual(retryable[0].json.entry, built.entry);

	const terminal = await fn({ first: () => ({ json: { error: "Request failed with status code 400" } }) }, dollar, {}, require);
	assert.equal(terminal[0].json.classification, "terminal");
});

test("Classify enrich failure counts attempts across a retry loop when the error item preserves them", async () => {
	const node = workflow.nodes.find((n) => n.name === "Classify enrich failure");
	const fn = new AsyncFunction("$input", "$", "$env", "require", node.parameters.jsCode);
	const built = { entry: { id: 9, url: "https://example.com/a", title: "A" }, body: { entry_id: 9 } };
	const dollar = () => ({ first: () => ({ json: built }) });

	const second = await fn({ first: () => ({ json: { entry: built.entry, body: built.body, attempt: 1, error: "connect ETIMEDOUT" } }) }, dollar, {}, require);
	assert.equal(second[0].json.attempt, 2);
});

test("the failure branch is reachable from every fallible node", () => {
	const fallible = workflow.nodes.filter((n) => n.onError === "continueErrorOutput").map((n) => n.name);
	assert.ok(fallible.length >= 3);
	for (const name of fallible) {
		const errorBranch = workflow.connections[name].main[1];
		assert.ok(errorBranch && errorBranch.length > 0, `${name} has no error branch`);
	}
	// Every path a failure can take eventually reaches Fail execution.
	assert.equal(workflow.connections["Entry failed"].main[0][0].node, "Alert claim query");
	assert.equal(workflow.connections["Retry enrich?"].main[1][0].node, "Alert claim query");
	assert.equal(workflow.connections["First failure?"].main[1][0].node, "Fail execution");
	assert.equal(workflow.connections["Notify"].main[0][0].node, "Fail execution");
});

test("a retryable classification loops back to Call enrich API via a backoff wait", () => {
	assert.equal(workflow.connections["Retry enrich?"].main[0][0].node, "Wait before retry");
	assert.equal(workflow.connections["Wait before retry"].main[0][0].node, "Call enrich API");
});

test("Miniflux gets an immediate ack independent of enrichment's outcome (no webhook redelivery to rely on)", () => {
	assert.equal(workflow.connections["Signature OK?"].main[0].map((l) => l.node).includes("Respond 200 accepted"), true);
	assert.equal(workflow.connections["Signature OK?"].main[0].map((l) => l.node).includes("Parse entries"), true);
});

test("committed workflow JSON matches the generator (run `npm run build`)", () => {
	const committed = fs.readFileSync(path.join(__dirname, "..", "workflows", "article-enrichment.json"), "utf8");
	assert.equal(committed, JSON.stringify(workflow, null, 2) + "\n");
});
