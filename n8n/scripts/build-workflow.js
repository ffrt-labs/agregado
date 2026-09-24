#!/usr/bin/env node
// Generates workflows/newsletter-ingest.json from src/ so the Code-node logic
// that ships to n8n is exactly the logic the tests cover. The committed JSON
// is the reviewable artefact (ADR-0007); rerun `npm run build` after editing
// src/ and commit the diff. Import it with `n8n import:workflow`, or paste it
// into the editor.
const fs = require("node:fs");
const path = require("node:path");

const SRC = path.join(__dirname, "..", "src");
const OUT = path.join(__dirname, "..", "workflows", "newsletter-ingest.json");

// Dependency-ordered so each module's `require("./x")` is already defined.
const MODULES = ["identity", "svix", "extract", "sanitize", "assets", "entry", "sql"];

function bundle(names) {
	const parts = [
		"const __mods = {};",
		"const __req = (p) => (p.startsWith('./') ? __mods[p.slice(2)] : require(p));",
	];
	for (const name of names) {
		const src = fs.readFileSync(path.join(SRC, `${name}.js`), "utf8");
		parts.push(`__mods.${name} = (function (require) { const module = { exports: {} }; const exports = module.exports;\n${src}\nreturn module.exports; })(__req);`);
	}
	return parts.join("\n");
}

const codeNode = (name, position, needs, body) => ({
	parameters: { mode: "runOnceForAllItems", language: "javaScript", jsCode: `${bundle(needs)}\n\n${body}` },
	id: name.toLowerCase().replace(/\W+/g, "-"),
	name,
	type: "n8n-nodes-base.code",
	typeVersion: 2,
	position,
	onError: "continueErrorOutput",
});

const respond = (name, position, code, body) => ({
	parameters: { respondWith: "json", responseBody: body, options: { responseCode: code } },
	id: name.toLowerCase().replace(/\W+/g, "-"),
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
	id: name.toLowerCase().replace(/\W+/g, "-"),
	name,
	type: "n8n-nodes-base.httpRequest",
	typeVersion: 4.2,
	position,
	onError: "continueErrorOutput",
	// credentials: create an "HTTP Header Auth" credential `Cloudflare API`
	// (Authorization: Bearer <token with D1 edit>) and select it after import.
});

const nodes = [
	{
		parameters: {
			httpMethod: "POST",
			// Replace the suffix once at deploy time; it must match the tunnel
			// ingress rule (plan §4) and the Resend webhook URL. Not derivable
			// from anything public.
			path: "resend-REPLACE_WITH_RANDOM_SUFFIX",
			responseMode: "responseNode",
			options: { rawBody: true },
		},
		id: "webhook",
		name: "Resend Webhook",
		type: "n8n-nodes-base.webhook",
		typeVersion: 2,
		position: [0, 300],
		webhookId: "bridge-resend-inbound",
	},
	codeNode("Verify Svix", [220, 300], ["identity", "svix"], `
const item = $input.first();
const headers = item.json.headers || {};
const buf = await this.helpers.getBinaryDataBuffer(0, 'data');
const rawBody = buf.toString('utf8');
const verified = __mods.svix.verifySvix({ secret: $env.RESEND_WEBHOOK_SECRET, headers, rawBody });
if (!verified) return [{ json: { verified: false } }];
const payload = JSON.parse(rawBody);
// Svix redelivery keeps the svix-id, so it is the stable key for once-per-message alerting
// even when the failure happens before the email's own Message-ID is known.
return [{ json: { verified: true, payload, alertKey: __mods.identity.entryId(headers['svix-id']) } }];
`),
	{
		parameters: { conditions: { boolean: [{ value1: "={{ $json.verified }}", value2: true }] } },
		id: "signature-ok",
		name: "Signature OK?",
		type: "n8n-nodes-base.if",
		typeVersion: 1,
		position: [440, 300],
	},
	respond("Respond 401", [660, 480], 401, '={{ { "error": "invalid signature" } }}'),
	{
		parameters: {
			url: "=https://api.resend.com/emails/receiving/{{ $json.payload.data.email_id }}",
			authentication: "genericCredentialType",
			genericAuthType: "httpHeaderAuth",
			options: {},
			// credentials: HTTP Header Auth `Resend API` (Authorization: Bearer re_...).
		},
		id: "fetch-email",
		name: "Fetch email",
		type: "n8n-nodes-base.httpRequest",
		typeVersion: 4.2,
		position: [660, 200],
		onError: "continueErrorOutput",
	},
	codeNode("Alias query", [880, 200], ["entry", "sql"], `
const email = $input.first().json;
const alias = __mods.entry.aliasFromRecipients(email.to);
if (!alias) throw new Error('email has no recipient');
return [{ json: { alias, query: __mods.sql.lookupSourceQuery(alias) } }];
`),
	d1Node("Lookup source", [1100, 200], "={{ $json.query }}"),
	codeNode("Build entry", [1320, 200], ["identity", "svix", "extract", "sanitize", "assets", "entry", "sql"], `
const email = $('Fetch email').first().json;
const lookup = $input.first().json;
const row = lookup.result && lookup.result[0] && lookup.result[0].results && lookup.result[0].results[0];
if (__mods.entry.gateSource(row) === 'skip') return [{ json: { skip: true } }];
const lower = {};
for (const [k, v] of Object.entries(email.headers || {})) lower[k.toLowerCase()] = v;
const entry = __mods.entry.buildEntry({
  email: { headers: lower, subject: email.subject, html: email.html, text: email.text },
  sourceId: row.id,
  secret: $env.BRIDGE_PERMALINK_SECRET,
  now: Math.floor(Date.now() / 1000),
});
return [{ json: { skip: false, entry } }];
`),
	{
		parameters: { conditions: { boolean: [{ value1: "={{ $json.skip }}", value2: true }] } },
		id: "skip",
		name: "Skip?",
		type: "n8n-nodes-base.if",
		typeVersion: 1,
		position: [1540, 200],
	},
	respond("Respond 200 no-op", [1760, 60], 200, '={{ { "ok": true, "skipped": true } }}'),
	codeNode("Capture assets", [1760, 300], ["assets"], `
const { entry } = $input.first().json;
const fetchAsset = async (url) => {
  const res = await this.helpers.httpRequest({ method: 'GET', url, encoding: 'arraybuffer', returnFullResponse: true, timeout: 10000 });
  return { contentType: res.headers['content-type'], bytes: Buffer.from(res.body) };
};
// A single asset failing is caught inside inlineAssets and never fails ingestion.
const { html, failed } = await __mods.assets.inlineAssets(entry.permalinkHtml, fetchAsset);
if (failed.length) console.log('bridge: skipped assets for ' + entry.id + ': ' + failed.join(' '));
return [{ json: { entry: { ...entry, permalinkHtml: html } } }];
`),
	{
		parameters: {
			method: "PUT",
			// R2 through its S3-compatible API; the object key is entries.id,
			// exactly what the Worker's /p/{uuid} path reads back.
			url: "=https://{{ $env.CF_ACCOUNT_ID }}.r2.cloudflarestorage.com/{{ $env.BRIDGE_R2_BUCKET }}/{{ $json.entry.id }}",
			authentication: "predefinedCredentialType",
			nodeCredentialType: "aws",
			sendHeaders: true,
			headerParameters: { parameters: [{ name: "Content-Type", value: "text/html; charset=utf-8" }] },
			sendBody: true,
			contentType: "raw",
			rawContentType: "text/html; charset=utf-8",
			body: "={{ $json.entry.permalinkHtml }}",
			options: {},
			// credentials: AWS credential with R2 access key/secret, region `auto`,
			// custom endpoint https://<account>.r2.cloudflarestorage.com.
		},
		id: "write-r2",
		name: "Write R2",
		type: "n8n-nodes-base.httpRequest",
		typeVersion: 4.2,
		position: [1980, 300],
		onError: "continueErrorOutput",
	},
	codeNode("Upsert query", [2200, 300], ["sql"], `
const { entry } = $('Capture assets').first().json;
return [{ json: { query: __mods.sql.upsertEntryQuery(entry) } }];
`),
	d1Node("Write D1", [2420, 300], "={{ $json.query }}"),
	respond("Respond 200 ok", [2640, 300], 200, '={{ { "ok": true } }}'),

	// Failure branch: every fallible node's error output lands here.
	codeNode("Alert claim query", [1320, 620], ["sql"], `
const key = $('Verify Svix').first().json.alertKey;
return [{ json: { query: __mods.sql.claimAlertQuery(key, Math.floor(Date.now() / 1000)) } }];
`),
	d1Node("Claim alert", [1540, 620], "={{ $json.query }}"),
	{
		parameters: { conditions: { number: [{ value1: "={{ $json.result[0].meta.changes }}", operation: "larger", value2: 0 }] } },
		id: "first-failure",
		name: "First failure?",
		type: "n8n-nodes-base.if",
		typeVersion: 1,
		position: [1760, 620],
	},
	{
		parameters: {
			method: "POST",
			// A plain-text notification endpoint (e.g. an ntfy topic URL).
			url: "={{ $env.BRIDGE_ALERT_URL }}",
			sendBody: true,
			contentType: "raw",
			rawContentType: "text/plain",
			body: "=Newsletter ingest failed (webhook {{ $('Verify Svix').first().json.alertKey }}). Resend will retry for ~27h; check n8n execution {{ $execution.id }}.",
			options: {},
		},
		id: "notify",
		name: "Notify",
		type: "n8n-nodes-base.httpRequest",
		typeVersion: 4.2,
		position: [1980, 560],
	},
	{
		parameters: { errorMessage: "newsletter ingest failed" },
		id: "fail",
		name: "Fail execution",
		type: "n8n-nodes-base.stopAndError",
		typeVersion: 1,
		position: [2200, 620],
	},
];
// Alert-branch code nodes must not themselves swallow errors silently.
for (const n of nodes) if (["Alert claim query", "Claim alert"].includes(n.name)) n.onError = "stopWorkflow";

const link = (to, index = 0) => ({ node: to, type: "main", index });
// Nodes with onError=continueErrorOutput have output 0 = success, 1 = error.
const failure = link("Alert claim query");
const connections = {
	"Resend Webhook": { main: [[link("Verify Svix")]] },
	"Verify Svix": { main: [[link("Signature OK?")], [failure]] },
	"Signature OK?": { main: [[link("Fetch email")], [link("Respond 401")]] },
	"Fetch email": { main: [[link("Alias query")], [failure]] },
	"Alias query": { main: [[link("Lookup source")], [failure]] },
	"Lookup source": { main: [[link("Build entry")], [failure]] },
	"Build entry": { main: [[link("Skip?")], [failure]] },
	"Skip?": { main: [[link("Respond 200 no-op")], [link("Capture assets")]] },
	"Capture assets": { main: [[link("Write R2")], [failure]] },
	"Write R2": { main: [[link("Upsert query")], [failure]] },
	"Upsert query": { main: [[link("Write D1")], [failure]] },
	"Write D1": { main: [[link("Respond 200 ok")], [failure]] },
	"Alert claim query": { main: [[link("Claim alert")]] },
	"Claim alert": { main: [[link("First failure?")]] },
	"First failure?": { main: [[link("Notify")], [link("Fail execution")]] },
	Notify: { main: [[link("Fail execution")]] },
};

// A failed Verify Svix node has no alertKey yet; that path is a code bug, not
// a delivery failure, and still fails the execution loudly via the claim node.
const workflow = {
	name: "Newsletter ingest (Resend → R2/D1)",
	nodes,
	connections,
	active: false,
	settings: { executionOrder: "v1" },
	pinData: {},
	meta: { templateCredsSetupCompleted: false },
};

module.exports = { workflow };

if (require.main === module) {
	fs.writeFileSync(OUT, JSON.stringify(workflow, null, 2) + "\n");
	console.log(`wrote ${path.relative(process.cwd(), OUT)}`);
}
