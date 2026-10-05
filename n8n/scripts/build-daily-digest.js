#!/usr/bin/env node
// Generates workflows/daily-digest.json from src/ (agregado#145). Rerun
// `npm run build` after editing src/ and commit the diff. Import it with
// `n8n import:workflow`, or paste it into the editor.
const path = require("node:path");
const { codeNode: codeNodeFor, d1Node, link, writeWorkflow } = require("./lib/nodes");

const SRC = path.join(__dirname, "..", "src");
const OUT = path.join(__dirname, "..", "workflows", "daily-digest.json");

const codeNode = (name, position, needs, body) => codeNodeFor(SRC, name, position, needs, body);

// Matches DIGEST_SCHEDULE's default (internal/config/config.go) — the legacy
// scheduler this workflow replaces (#85) used the same cron string.
const DIGEST_CRON = "0 8 * * *";
const MAX_SEND_RETRIES = 3;

const nodes = [
	{
		parameters: { rule: { interval: [{ field: "cronExpression", expression: DIGEST_CRON }] } },
		id: "digest-schedule",
		name: "Digest schedule",
		type: "n8n-nodes-base.scheduleTrigger",
		typeVersion: 1.2,
		position: [0, 300],
	},
	codeNode("Build digest request", [220, 300], ["digest"], `
const { date, url } = __mods.digest.buildDigestRequest(Date.now(), $env.AGREGADO_BASE_URL);
return [{ json: { date, url, attempt: 0 } }];
`),
	{
		// Idempotent per date (#79's AC8): re-calling it for the same date
		// re-returns the already-persisted Subject/HTML/Text/ID rather than
		// another model call, so this same node is also the retry's re-fetch —
		// a send-failure retry loops back here rather than resending whatever
		// this workflow happened to be holding in memory.
		parameters: {
			method: "POST",
			url: "={{ $json.url }}",
			sendHeaders: true,
			headerParameters: { parameters: [{ name: "X-Enrichment-Secret", value: "={{ $env.ENRICHMENT_SECRET }}" }] },
			options: {},
		},
		id: "get-digest",
		name: "Get digest",
		type: "n8n-nodes-base.httpRequest",
		typeVersion: 4.2,
		position: [440, 300],
		onError: "continueErrorOutput",
	},
	codeNode("Digest fetch failed", [660, 480], [], `
const err = $input.first().json || {};
const built = $('Build digest request').first().json;
const date = err.date || built.date;
const message = (err.error && (err.error.message || err.error)) || err.message || 'digest fetch failed';
return [{ json: { date, message } }];
`),
	{
		// Sends the artifact's Subject/HTML/Text verbatim (issue #145's
		// acceptance criteria) — no selection, ranking, or rendering logic
		// here, that is all #79's. Recipient and from address are
		// credential/env placeholders, never committed (n8n/README.md
		// pattern); the exact SMTP/email provider is explicitly deferred
		// (docs/architecture/ecosystem.md).
		parameters: {
			fromEmail: "={{ $env.DIGEST_FROM_EMAIL }}",
			toEmail: "={{ $env.DIGEST_RECIPIENT_EMAIL }}",
			subject: "={{ $json.Subject }}",
			html: "={{ $json.HTML }}",
			text: "={{ $json.Text }}",
			options: {},
			// credentials: select an SMTP credential (e.g. "Digest SMTP") after
			// import — the provider itself is an explicitly deferred decision.
		},
		id: "send-digest-email",
		name: "Send digest email",
		type: "n8n-nodes-base.emailSend",
		typeVersion: 2.1,
		position: [660, 200],
		onError: "continueErrorOutput",
	},
	// Runs only after a successful send. "Get digest" is read with last():
	// a send-failure retry re-runs it, and the artifact that was actually
	// sent is the latest fetch.
	codeNode("Check empty digest", [880, 120], ["digest"], `
const alert = __mods.digest.emptyDigestAlert($('Get digest').last().json, $('Build digest request').first().json.date);
return alert ? [{ json: alert }] : [];
`),
	codeNode("Classify send failure", [880, 400], [], `
const err = $input.first().json || {};
const built = $('Build digest request').first().json;
const date = err.date || built.date;
const url = err.url || built.url;
// attempt cannot be recovered from the error item: the retry loop goes
// through "Get digest" to re-fetch the artifact (issue #145's acceptance
// criteria), and a successful fetch replaces $json with the HTTP response
// body, which carries no attempt field. This node's own last output in the
// execution is the counter of record instead — self-reference throws on the
// first failure (it has not run yet this execution), which is exactly when
// the count should start at 0.
let priorAttempt = 0;
try { priorAttempt = $('Classify send failure').first().json.attempt || 0; } catch (e) { priorAttempt = 0; }
const message = (err.error && (err.error.message || err.error)) || err.message || 'digest send failed';
return [{ json: { date, url, attempt: priorAttempt + 1, message } }];
`),
	{
		parameters: { conditions: { number: [{ value1: "={{ $json.attempt }}", operation: "smallerEqual", value2: MAX_SEND_RETRIES }] } },
		id: "retry-send",
		name: "Retry send?",
		type: "n8n-nodes-base.if",
		typeVersion: 1,
		position: [1100, 400],
	},
	{
		// Exponential backoff: 2s, 4s, 8s for attempts 1-3, same shape as
		// article-enrichment.json's "Wait before retry".
		parameters: { amount: "={{ 2 ** $json.attempt }}", unit: "seconds" },
		id: "wait-before-retry",
		name: "Wait before retry",
		type: "n8n-nodes-base.wait",
		typeVersion: 1.1,
		position: [1320, 300],
	},
	codeNode("Alert claim query", [1320, 620], ["sql"], `
const { date, message } = $input.first().json;
return [{ json: { date, message, query: __mods.sql.claimAlertQuery('digest-' + date, Math.floor(Date.now() / 1000)) } }];
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
			// A plain-text notification endpoint (e.g. an ntfy topic URL) — the
			// same single-notification-channel pattern as newsletter-ingest.json
			// and article-enrichment.json (docs/architecture/ecosystem.md's
			// reliability model: terminal/exhausted failures reach one alert).
			url: "={{ $env.BRIDGE_ALERT_URL }}",
			sendBody: true,
			contentType: "raw",
			rawContentType: "text/plain",
			body: "=Daily digest for {{ $('Alert claim query').first().json.date }}: {{ $('Alert claim query').first().json.message }}. Check n8n execution {{ $execution.id }}.",
			options: {},
		},
		id: "notify",
		name: "Notify",
		type: "n8n-nodes-base.httpRequest",
		typeVersion: 4.2,
		position: [1980, 560],
	},
	{
		parameters: { errorMessage: "daily digest failed or was empty" },
		id: "fail",
		name: "Fail execution",
		type: "n8n-nodes-base.stopAndError",
		typeVersion: 1,
		position: [2200, 620],
	},
];
// Alert-branch code nodes must not themselves swallow errors silently (same
// reasoning as newsletter-ingest.json/article-enrichment.json). "Build digest
// request" runs before any fetched artifact exists to alert about — same
// precedent as article-enrichment.json's "Verify signature"/"Parse entries"
// — so it fails the execution loudly instead of routing into a branch with
// nothing to report.
for (const n of nodes) if (["Alert claim query", "Claim alert", "Digest fetch failed", "Classify send failure", "Check empty digest", "Build digest request"].includes(n.name)) n.onError = "stopWorkflow";

const connections = {
	"Digest schedule": { main: [[link("Build digest request")]] },
	"Build digest request": { main: [[link("Get digest")]] },
	"Get digest": { main: [[link("Send digest email")], [link("Digest fetch failed")]] },
	"Digest fetch failed": { main: [[link("Alert claim query")]] },
	"Send digest email": { main: [[link("Check empty digest")], [link("Classify send failure")]] },
	"Check empty digest": { main: [[link("Alert claim query")]] },
	"Classify send failure": { main: [[link("Retry send?")]] },
	"Retry send?": { main: [[link("Wait before retry")], [link("Alert claim query")]] },
	// The retry loops back to "Get digest", not to "Send digest email" — a
	// retry re-fetches the persisted artifact rather than resending whatever
	// this execution happened to be holding (issue #145's acceptance
	// criteria: "re-fetches the same persisted artifact ... no regenerated
	// content"). Since the endpoint is idempotent per date, the re-fetch
	// yields the identical artifact; looping here rather than at the email
	// node is what makes that the literal behavior, not just an invariant
	// that happens to hold.
	"Wait before retry": { main: [[link("Get digest")]] },
	"Alert claim query": { main: [[link("Claim alert")]] },
	"Claim alert": { main: [[link("First failure?")]] },
	"First failure?": { main: [[link("Notify")], [link("Fail execution")]] },
	Notify: { main: [[link("Fail execution")]] },
};

const workflow = {
	name: "Daily digest (schedule → Agregado → email)",
	nodes,
	connections,
	active: false,
	settings: { executionOrder: "v1" },
	pinData: {},
	meta: { templateCredsSetupCompleted: false },
};

module.exports = { workflow };

if (require.main === module) writeWorkflow(workflow, OUT);
