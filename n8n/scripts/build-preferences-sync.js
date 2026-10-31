#!/usr/bin/env node
// Generates workflows/preferences-sync.json from src/ (agregado#191,
// ADR-0010). Rerun `npm run build` after editing src/ and commit the diff.
//
// Nightly: export the PREFERENCES Google Doc as Markdown → PUT it to Agregado
// → ask Agregado to regenerate it from the Reader's votes and Opens → when it
// changed, write the result back to the Doc and report what changed. Agregado
// never talks to Google; n8n never touches the host filesystem.
const path = require("node:path");
const { codeNode: codeNodeFor, link, writeWorkflow } = require("./lib/nodes");

const SRC = path.join(__dirname, "..", "src");
const OUT = path.join(__dirname, "..", "workflows", "preferences-sync.json");

const codeNode = (name, position, needs, body) => codeNodeFor(SRC, name, position, needs, body);

// 06:30, ahead of the 08:00 Digest (build-daily-digest.js), so the Articles
// enriched before the Digest are scored against tonight's profile. Same
// pinned timezone as the Digest.
const SYNC_CRON = "30 6 * * *";
const SYNC_TIMEZONE = "Europe/Berlin";

const secretHeader = { parameters: [{ name: "X-Enrichment-Secret", value: "={{ $env.ENRICHMENT_SECRET }}" }] };

// Google calls use n8n's Google Drive OAuth2 credential, selected after
// import. The HTTP node rather than the Google Drive node because only the
// raw Drive API offers the Markdown export and the Markdown upload that
// converts back into the same Doc.
const googleAuth = { authentication: "predefinedCredentialType", nodeCredentialType: "googleDriveOAuth2Api" };

const nodes = [
	{
		parameters: { rule: { interval: [{ field: "cronExpression", expression: SYNC_CRON }] } },
		id: "preferences-schedule",
		name: "Preferences schedule",
		type: "n8n-nodes-base.scheduleTrigger",
		typeVersion: 1.2,
		position: [0, 300],
	},
	codeNode("Build requests", [220, 300], ["preferences"], `
return [{ json: __mods.preferences.buildRequests($env) }];
`),
	{
		parameters: {
			method: "GET",
			url: "={{ $json.exportUrl }}",
			...googleAuth,
			options: { response: { response: { responseFormat: "text", outputPropertyName: "markdown" } } },
		},
		id: "export-doc",
		name: "Export Doc",
		type: "n8n-nodes-base.httpRequest",
		typeVersion: 4.2,
		position: [440, 300],
		onError: "continueErrorOutput",
	},
	{
		// A hand edit made on the phone takes effect here, even if the
		// regeneration below fails.
		parameters: {
			method: "PUT",
			url: "={{ $('Build requests').first().json.storeUrl }}",
			sendHeaders: true,
			headerParameters: secretHeader,
			sendBody: true,
			contentType: "raw",
			rawContentType: "text/markdown",
			body: "={{ $json.markdown }}",
			options: {},
		},
		id: "store-profile",
		name: "Store profile",
		type: "n8n-nodes-base.httpRequest",
		typeVersion: 4.2,
		position: [660, 300],
		onError: "continueErrorOutput",
	},
	{
		parameters: {
			method: "POST",
			url: "={{ $('Build requests').first().json.regenerateUrl }}",
			sendHeaders: true,
			headerParameters: secretHeader,
			options: {},
		},
		id: "regenerate",
		name: "Regenerate",
		type: "n8n-nodes-base.httpRequest",
		typeVersion: 4.2,
		position: [880, 300],
		onError: "continueErrorOutput",
	},
	// Ends the run quietly when nothing changed: no Doc write, no new Doc
	// revision, no notification.
	codeNode("Describe change", [1100, 200], ["preferences"], `
const result = $input.first().json;
if (!result.changed) return [];
const before = $('Export Doc').first().json.markdown;
return [{ json: { profile: result.profile, signals: result.signals, change: __mods.preferences.describeChange(before, result.profile) } }];
`),
	{
		parameters: {
			method: "PATCH",
			url: "={{ $('Build requests').first().json.uploadUrl }}",
			...googleAuth,
			sendBody: true,
			contentType: "raw",
			rawContentType: "text/markdown",
			body: "={{ $json.profile }}",
			options: {},
		},
		id: "write-doc",
		name: "Write Doc",
		type: "n8n-nodes-base.httpRequest",
		typeVersion: 4.2,
		position: [1320, 200],
		onError: "continueErrorOutput",
	},
	{
		parameters: {
			method: "POST",
			// The same plain-text notification channel as the other workflows.
			url: "={{ $env.BRIDGE_ALERT_URL }}",
			sendBody: true,
			contentType: "raw",
			rawContentType: "text/plain",
			body: "=PREFERENCES updated from {{ $('Describe change').first().json.signals }} signals:\n{{ $('Describe change').first().json.change }}\nUndo with File → Version history in the Doc.",
			options: {},
		},
		id: "notify-change",
		name: "Notify change",
		type: "n8n-nodes-base.httpRequest",
		typeVersion: 4.2,
		position: [1540, 200],
	},
	codeNode("Describe failure", [1100, 520], ["preferences"], `
return [{ json: { message: __mods.preferences.failureMessage($prevNode.name, $input.first().json) } }];
`),
	{
		parameters: {
			method: "POST",
			url: "={{ $env.BRIDGE_ALERT_URL }}",
			sendBody: true,
			contentType: "raw",
			rawContentType: "text/plain",
			body: "={{ $json.message }} Check n8n execution {{ $execution.id }}.",
			options: {},
		},
		id: "notify-failure",
		name: "Notify failure",
		type: "n8n-nodes-base.httpRequest",
		typeVersion: 4.2,
		position: [1320, 520],
	},
	{
		parameters: { errorMessage: "preferences sync failed" },
		id: "fail",
		name: "Fail execution",
		type: "n8n-nodes-base.stopAndError",
		typeVersion: 1,
		position: [1540, 520],
	},
];
// Code nodes fail the execution loudly rather than routing a broken item on.
for (const n of nodes) if (n.type === "n8n-nodes-base.code") n.onError = "stopWorkflow";

const connections = {
	"Preferences schedule": { main: [[link("Build requests")]] },
	"Build requests": { main: [[link("Export Doc")]] },
	"Export Doc": { main: [[link("Store profile")], [link("Describe failure")]] },
	"Store profile": { main: [[link("Regenerate")], [link("Describe failure")]] },
	Regenerate: { main: [[link("Describe change")], [link("Describe failure")]] },
	"Describe change": { main: [[link("Write Doc")]] },
	"Write Doc": { main: [[link("Notify change")], [link("Describe failure")]] },
	"Describe failure": { main: [[link("Notify failure")]] },
	"Notify failure": { main: [[link("Fail execution")]] },
};

const workflow = {
	name: "Preferences sync (Google Doc ↔ Agregado, nightly regeneration)",
	nodes,
	connections,
	active: false,
	settings: { executionOrder: "v1", timezone: SYNC_TIMEZONE },
	pinData: {},
	meta: { templateCredsSetupCompleted: false },
};

module.exports = { workflow };

if (require.main === module) writeWorkflow(workflow, OUT);
