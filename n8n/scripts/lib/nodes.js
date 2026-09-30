// Shared node-builder helpers for the workflow generators
// (scripts/build-workflow.js, scripts/build-article-enrichment.js). Kept in
// one place so the Code-node bundling logic — the thing that decides what
// actually ships to n8n — cannot silently diverge between workflows.
const fs = require("node:fs");
const path = require("node:path");

function bundle(srcDir, names) {
	const parts = [
		"const __mods = {};",
		"const __req = (p) => (p.startsWith('./') ? __mods[p.slice(2)] : require(p));",
	];
	for (const name of names) {
		const src = fs.readFileSync(path.join(srcDir, `${name}.js`), "utf8");
		parts.push(`__mods.${name} = (function (require) { const module = { exports: {} }; const exports = module.exports;\n${src}\nreturn module.exports; })(__req);`);
	}
	return parts.join("\n");
}

const idFor = (name) => name.toLowerCase().replace(/\W+/g, "-");

const codeNode = (srcDir, name, position, needs, body) => ({
	parameters: { mode: "runOnceForAllItems", language: "javaScript", jsCode: `${bundle(srcDir, needs)}\n\n${body}` },
	id: idFor(name),
	name,
	type: "n8n-nodes-base.code",
	typeVersion: 2,
	position,
	onError: "continueErrorOutput",
});

const respond = (name, position, code, body) => ({
	parameters: { respondWith: "json", responseBody: body, options: { responseCode: code } },
	id: idFor(name),
	name,
	type: "n8n-nodes-base.respondToWebhook",
	typeVersion: 1.1,
	position,
});

const D1_URL = "=https://api.cloudflare.com/client/v4/accounts/{{ $env.CF_ACCOUNT_ID }}/d1/database/{{ $env.BRIDGE_D1_DATABASE_ID }}/query";

const d1Node = (name, position, bodyExpr) => ({
	parameters: {
		method: "POST",
		url: D1_URL,
		authentication: "genericCredentialType",
		genericAuthType: "httpHeaderAuth",
		sendBody: true,
		specifyBody: "json",
		jsonBody: bodyExpr,
		options: {},
	},
	id: idFor(name),
	name,
	type: "n8n-nodes-base.httpRequest",
	typeVersion: 4.2,
	position,
	onError: "continueErrorOutput",
	// credentials: create an "HTTP Header Auth" credential `Cloudflare API`
	// (Authorization: Bearer <token with D1 edit>) and select it after import.
});

const link = (to, index = 0) => ({ node: to, type: "main", index });

function writeWorkflow(workflow, outPath) {
	fs.writeFileSync(outPath, JSON.stringify(workflow, null, 2) + "\n");
	console.log(`wrote ${path.relative(process.cwd(), outPath)}`);
}

module.exports = { bundle, idFor, codeNode, respond, d1Node, D1_URL, link, writeWorkflow };
