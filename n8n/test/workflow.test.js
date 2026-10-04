const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { workflow } = require("../scripts/build-workflow");

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

test("bundled Code nodes run against the real modules", async () => {
	const build = workflow.nodes.find((n) => n.name === "Build entry");
	const fixture = JSON.parse(fs.readFileSync(path.join(__dirname, "fixtures", "01-archived-at-header.json"), "utf8"));
	const fn = new AsyncFunction("$input", "$", "$env", "require", build.parameters.jsCode);
	const out = await fn(
		{ first: () => ({ json: { result: [{ results: [{ id: "dispatch", status: "active" }] }] } }) },
		() => ({ first: () => ({ json: { headers: { ...fixture.headers, "Message-ID": "<m@x>" }, subject: fixture.subject, html: fixture.html, text: fixture.text } }) }),
		{ BRIDGE_PERMALINK_SECRET: "s" },
		require
	);
	assert.equal(out[0].json.entry.canonicalUrl, "https://dispatch.example.com/p/weekly-dispatch-42");
	const skipped = await fn(
		{ first: () => ({ json: { result: [{ results: [{ id: "x", status: "pending" }] }] } }) },
		() => ({ first: () => ({ json: {} }) }),
		{},
		require
	);
	assert.deepEqual(out.length && skipped[0].json, { skip: true });
});

test("every Code node's bundled modules satisfy their own internal requires", async () => {
	// "run against the real modules" above only exercises Build entry. entry.js
	// requires ./identity unconditionally at load time regardless of which of
	// its exports is used, so any node bundling entry.js without identity.js
	// throws "Cannot destructure ... of 'require(...)' as it is undefined" the
	// moment the node runs, even though `new AsyncFunction(...)` above never
	// catches it (that only checks the wrapper compiles, not that its body
	// executes without throwing).
	for (const n of workflow.nodes.filter((n) => n.type === "n8n-nodes-base.code")) {
		const fn = new AsyncFunction("$input", "$", "$env", "require", n.parameters.jsCode);
		await assert.doesNotReject(
			fn({ first: () => ({ json: {} }) }, () => ({ first: () => ({ json: {} }) }), {}, require).catch((err) => {
				// Bundling bugs throw before any business logic runs (missing
				// module, bad require); business logic on empty input is
				// expected to throw its own, different errors (e.g. "email has
				// no recipient") and those are not what this test guards.
				if (/Cannot destructure property .* of .require\(\.\.\.\). as it is undefined/.test(err.message)) throw err;
				return null;
			}),
			n.name
		);
	}
});

test("the failure branch is reachable from every fallible node and always ends in Fail execution", () => {
	const fallible = workflow.nodes.filter((n) => n.onError === "continueErrorOutput").map((n) => n.name);
	assert.ok(fallible.length >= 6);
	for (const name of fallible) {
		const errorBranch = workflow.connections[name].main[1];
		assert.equal(errorBranch[0].node, "Alert claim query", name);
	}
	assert.equal(workflow.connections["First failure?"].main[1][0].node, "Fail execution");
	assert.equal(workflow.connections["Notify"].main[0][0].node, "Fail execution");
});

test("failure-branch nodes read Verify Svix's output 0 explicitly (they hang off its error output)", () => {
	const claim = workflow.nodes.find((n) => n.name === "Alert claim query").parameters.jsCode;
	assert.match(claim, /\$\('Verify Svix'\)\.first\(0\)/);
	assert.doesNotMatch(claim, /\$\('Verify Svix'\)\.first\(\)/);
	const notify = JSON.stringify(workflow.nodes.find((n) => n.name === "Notify").parameters);
	assert.doesNotMatch(notify, /Verify Svix/);
});

test("committed workflow JSON matches the generator (run `npm run build`)", () => {
	const committed = fs.readFileSync(path.join(__dirname, "..", "workflows", "newsletter-ingest.json"), "utf8");
	assert.equal(committed, JSON.stringify(workflow, null, 2) + "\n");
});
