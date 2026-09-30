#!/usr/bin/env node
// Generates workflows/article-enrichment.json from src/ (agregado#144).
// Rerun `npm run build` after editing src/ and commit the diff. Import it
// with `n8n import:workflow`, or paste it into the editor.
const path = require("node:path");
const { codeNode: codeNodeFor, respond, d1Node, link, writeWorkflow } = require("./lib/nodes");

const SRC = path.join(__dirname, "..", "src");
const OUT = path.join(__dirname, "..", "workflows", "article-enrichment.json");

const codeNode = (name, position, needs, body) => codeNodeFor(SRC, name, position, needs, body);

const MAX_ENRICH_RETRIES = 3;

const nodes = [
	{
		parameters: {
			httpMethod: "POST",
			// Replace the suffix once at deploy time; it must match the tunnel
			// ingress rule and the URL configured as Miniflux's webhook endpoint.
			// Not derivable from anything public.
			path: "miniflux-new-entries-REPLACE_WITH_RANDOM_SUFFIX",
			responseMode: "responseNode",
			options: { rawBody: true },
		},
		id: "webhook",
		name: "Miniflux Webhook",
		type: "n8n-nodes-base.webhook",
		typeVersion: 2,
		position: [0, 300],
		webhookId: "agregado-miniflux-new-entries",
	},
	codeNode("Verify signature", [220, 300], ["miniflux"], `
const item = $input.first();
const headers = item.json.headers || {};
const buf = await this.helpers.getBinaryDataBuffer(0, 'data');
const rawBody = buf.toString('utf8');
const verified = __mods.miniflux.verifyMinifluxSignature({ secret: $env.MINIFLUX_WEBHOOK_SECRET, signatureHeader: headers['x-miniflux-signature'], rawBody });
if (!verified) return [{ json: { verified: false } }];
return [{ json: { verified: true, payload: JSON.parse(rawBody) } }];
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
	// Miniflux delivers new_entries fire-and-forget with no redelivery on a
	// non-2xx response (docs/architecture/ecosystem.md's reliability model),
	// so an immediate ack here — with retries happening inside n8n rather
	// than relying on Miniflux to resend — is the only way to get the
	// backoff the issue asks for.
	respond("Respond 200 accepted", [660, 200], 200, '={{ { "ok": true } }}'),
	// No separate GET back to Miniflux for entry details: new_entries already
	// embeds id/url/title/author/published_at per entry (n8n/README.md's
	// "Deliberate deviations from the plan").
	codeNode("Parse entries", [660, 380], ["miniflux"], `
const { payload } = $input.first().json;
const entries = __mods.miniflux.parseNewEntries(payload);
return entries.map((entry) => ({ json: { entry } }));
`),
	codeNode("Determine bridge match", [880, 380], ["miniflux"], `
const { entry } = $input.first().json;
const permalinkUuid = __mods.miniflux.matchBridgePermalink(entry.url, $env.BRIDGE_ORIGIN);
return [{ json: { entry, permalinkUuid, isBridgePermalink: permalinkUuid !== null } }];
`),
	{
		parameters: { conditions: { boolean: [{ value1: "={{ $json.isBridgePermalink }}", value2: true }] } },
		id: "is-bridge-permalink",
		name: "Is bridge permalink?",
		type: "n8n-nodes-base.if",
		typeVersion: 1,
		position: [1100, 380],
	},
	codeNode("Bridge content query", [1320, 260], ["sql"], `
const item = $input.first().json;
return [{ json: { ...item, query: __mods.sql.lookupBridgeContentQuery(item.permalinkUuid) } }];
`),
	d1Node("Lookup bridge content", [1540, 260], "={{ $json.query }}"),
	codeNode("Attach bridge content", [1760, 260], [], `
const item = $input.first().json;
const row = item.result && item.result[0] && item.result[0].results && item.result[0].results[0];
// The D1 write from newsletter-ingest happens before Miniflux ever polls the
// feed, so a missing row here is a bug or lag, not an expected case — it
// fails loudly into the alert branch rather than silently enriching without
// bridge_content.
if (!row) throw new Error('no bridge_content found for permalink ' + item.permalinkUuid);
return [{ json: { entry: item.entry, bridgeContent: row.readable_content } }];
`),
	codeNode("Entry failed", [1760, 500], [], `
// Same n8n error-output uncertainty as "Classify enrich failure" — entry
// falls back to the earliest per-entry node's output when this item's own
// input was not preserved across the error.
const err = $input.first().json || {};
const fallbackEntry = $('Determine bridge match').first().json && $('Determine bridge match').first().json.entry;
const entry = err.entry || fallbackEntry;
const message = (err.error && (err.error.message || err.error)) || err.message || 'article enrichment failed before the API call';
return [{ json: { entry, message } }];
`),
	codeNode("Build enrich request", [1980, 380], ["enrich"], `
const item = $input.first().json;
return [{ json: { entry: item.entry, body: __mods.enrich.buildEnrichRequest(item.entry, item.bridgeContent) } }];
`),
	{
		parameters: {
			method: "POST",
			url: "={{ $env.AGREGADO_BASE_URL }}/api/private/articles/enrich",
			sendHeaders: true,
			headerParameters: { parameters: [{ name: "X-Enrichment-Secret", value: "={{ $env.ENRICHMENT_SECRET }}" }] },
			sendBody: true,
			specifyBody: "json",
			jsonBody: "={{ $json.body }}",
			options: {},
		},
		id: "call-enrich-api",
		name: "Call enrich API",
		type: "n8n-nodes-base.httpRequest",
		typeVersion: 4.2,
		position: [2200, 380],
		onError: "continueErrorOutput",
	},
	codeNode("Classify enrich failure", [2200, 620], ["miniflux"], `
// n8n's error output for a failed HTTP node is not guaranteed to carry the
// original input item forward, so entry/body/attempt fall back to the node
// that built them rather than being trusted from this item unconditionally
// (agregado#144, unverified against live n8n — see n8n/README.md's
// Verification status).
const err = $input.first().json || {};
const built = $('Build enrich request').first().json;
const entry = err.entry || built.entry;
const body = err.body || built.body;
const priorAttempt = err.attempt || 0;
const message = (err.error && (err.error.message || err.error)) || err.message || 'enrich request failed';
const classification = __mods.miniflux.classifyEnrichFailure(message);
return [{ json: { entry, body, attempt: priorAttempt + 1, message, classification } }];
`),
	{
		parameters: {
			conditions: {
				boolean: [{ value1: "={{ $json.classification === 'retryable' }}", value2: true }],
				number: [{ value1: "={{ $json.attempt }}", operation: "smallerEqual", value2: MAX_ENRICH_RETRIES }],
			},
			combineOperation: "all",
		},
		id: "retry-enrich",
		name: "Retry enrich?",
		type: "n8n-nodes-base.if",
		typeVersion: 1,
		position: [2420, 620],
	},
	{
		// Exponential backoff: 2s, 4s, 8s for attempts 1-3.
		parameters: { amount: "={{ 2 ** $json.attempt }}", unit: "seconds" },
		id: "wait-before-retry",
		name: "Wait before retry",
		type: "n8n-nodes-base.wait",
		typeVersion: 1.1,
		position: [2640, 500],
	},
	codeNode("Alert claim query", [2640, 740], ["sql"], `
const { entry, message } = $input.first().json;
return [{ json: { entry, message, query: __mods.sql.claimAlertQuery('enrich-' + entry.id, Math.floor(Date.now() / 1000)) } }];
`),
	d1Node("Claim alert", [2860, 740], "={{ $json.query }}"),
	{
		parameters: { conditions: { number: [{ value1: "={{ $json.result[0].meta.changes }}", operation: "larger", value2: 0 }] } },
		id: "first-failure",
		name: "First failure?",
		type: "n8n-nodes-base.if",
		typeVersion: 1,
		position: [3080, 740],
	},
	{
		parameters: {
			method: "POST",
			// A plain-text notification endpoint (e.g. an ntfy topic URL) — the
			// same alert pattern as newsletter-ingest.json.
			url: "={{ $env.BRIDGE_ALERT_URL }}",
			sendBody: true,
			contentType: "raw",
			rawContentType: "text/plain",
			body: "=Article enrichment failed for Miniflux entry {{ $('Alert claim query').first().json.entry.id }}: {{ $('Alert claim query').first().json.message }}. Check n8n execution {{ $execution.id }}.",
			options: {},
		},
		id: "notify",
		name: "Notify",
		type: "n8n-nodes-base.httpRequest",
		typeVersion: 4.2,
		position: [3300, 680],
	},
	{
		parameters: { errorMessage: "article enrichment failed" },
		id: "fail",
		name: "Fail execution",
		type: "n8n-nodes-base.stopAndError",
		typeVersion: 1,
		position: [3520, 740],
	},
];
// Alert-branch code nodes must not themselves swallow errors silently. Nodes
// that run before any entry exists — signature verification, splitting the
// payload into entries — have no entry to blame in an alert, so they fail
// the execution loudly instead (mirrors newsletter-ingest.json's "Verify
// Svix" known gap); everything after "Parse entries" has an entry and routes
// into the per-entry alert branch like newsletter-ingest.json's "Alias
// query"/"Build entry" do.
for (const n of nodes) if (["Alert claim query", "Claim alert", "Verify signature", "Parse entries", "Classify enrich failure", "Entry failed"].includes(n.name)) n.onError = "stopWorkflow";

const connections = {
	"Miniflux Webhook": { main: [[link("Verify signature")]] },
	"Verify signature": { main: [[link("Signature OK?")]] },
	"Signature OK?": { main: [[link("Respond 200 accepted"), link("Parse entries")], [link("Respond 401")]] },
	"Parse entries": { main: [[link("Determine bridge match")]] },
	"Determine bridge match": { main: [[link("Is bridge permalink?")], [link("Entry failed")]] },
	"Is bridge permalink?": { main: [[link("Bridge content query")], [link("Build enrich request")]] },
	"Bridge content query": { main: [[link("Lookup bridge content")], [link("Entry failed")]] },
	"Lookup bridge content": { main: [[link("Attach bridge content")], [link("Entry failed")]] },
	"Attach bridge content": { main: [[link("Build enrich request")], [link("Entry failed")]] },
	"Entry failed": { main: [[link("Alert claim query")]] },
	"Build enrich request": { main: [[link("Call enrich API")], [link("Entry failed")]] },
	"Call enrich API": { main: [[], [link("Classify enrich failure")]] },
	"Classify enrich failure": { main: [[link("Retry enrich?")]] },
	"Retry enrich?": { main: [[link("Wait before retry")], [link("Alert claim query")]] },
	"Wait before retry": { main: [[link("Call enrich API")]] },
	"Alert claim query": { main: [[link("Claim alert")]] },
	"Claim alert": { main: [[link("First failure?")]] },
	"First failure?": { main: [[link("Notify")], [link("Fail execution")]] },
	Notify: { main: [[link("Fail execution")]] },
};

const workflow = {
	name: "Article enrichment (Miniflux new_entries → Agregado)",
	nodes,
	connections,
	active: false,
	settings: { executionOrder: "v1" },
	pinData: {},
	meta: { templateCredsSetupCompleted: false },
};

module.exports = { workflow };

if (require.main === module) writeWorkflow(workflow, OUT);
