/**
 * The bridge's two read paths (agregado#97, #98, #101): a per-Source
 * private Atom feed and a permanent UUID permalink. Storage/serving stays
 * Cloudflare-native; writing to D1/R2 is n8n's job at ingest time
 * (ADR-0007), not this Worker's — there is no write path here.
 */

import PostalMime from "postal-mime";
import { buildAtomFeed, type FeedEntry } from "./atom";
import { parseBasicAuth, verifySecret } from "./basicAuth";

const FEED_WINDOW_SECONDS = 30 * 24 * 60 * 60;

interface SourceRow {
	id: string;
	display_name: string;
	basic_auth_secret_hash: string;
	status: string;
}

interface EntryRow {
	id: string;
	canonical_url: string | null;
	permalink_uuid: string;
	title: string;
	readable_content: string;
	published_at: number;
}

function unauthorized(): Response {
	return new Response("Unauthorized", {
		status: 401,
		headers: { "WWW-Authenticate": 'Basic realm="bridge feed"' },
	});
}

async function handleFeedRequest(request: Request, env: Env, sourceId: string): Promise<Response> {
	const source = await env.BRIDGE_DB.prepare("SELECT * FROM sources WHERE id = ?")
		.bind(sourceId)
		.first<SourceRow>();
	// Unknown source: 404, not 401 — matching agregado#98's decision not to
	// confirm/deny a source's existence differently from any other missing
	// route, since (unlike the permalink) this URL is not itself the secret.
	if (!source) return new Response("Not Found", { status: 404 });

	const credentials = parseBasicAuth(request);
	if (!credentials) return unauthorized();
	if (!(await verifySecret(credentials.password, source.basic_auth_secret_hash))) return unauthorized();

	const cutoff = Math.floor(Date.now() / 1000) - FEED_WINDOW_SECONDS;
	const { results } = await env.BRIDGE_DB.prepare(
		"SELECT id, canonical_url, permalink_uuid, title, readable_content, published_at FROM entries WHERE source_id = ? AND published_at >= ? ORDER BY published_at DESC"
	)
		.bind(sourceId, cutoff)
		.all<EntryRow>();

	const entries: FeedEntry[] = results.map((row) => ({
		id: row.id,
		permalinkUuid: row.permalink_uuid,
		canonicalUrl: row.canonical_url,
		title: row.title,
		readableContent: row.readable_content,
		publishedAt: row.published_at,
	}));

	const bridgeOrigin = new URL(request.url).origin;
	const xml = buildAtomFeed({ sourceId: source.id, displayName: source.display_name }, entries, bridgeOrigin);
	return new Response(xml, { headers: { "Content-Type": "application/atom+xml; charset=utf-8" } });
}

async function handlePermalinkRequest(env: Env, uuid: string): Promise<Response> {
	const entry = await env.BRIDGE_DB.prepare("SELECT id FROM entries WHERE permalink_uuid = ?")
		.bind(uuid)
		.first<Pick<EntryRow, "id">>();
	if (!entry) return new Response("Not Found", { status: 404 });

	// entries.id doubles as the R2 key (agregado#100: one identity axis,
	// hash(Message-ID), end-to-end — no separate stored key column).
	const object = await env.BRIDGE_BUCKET.get(entry.id);
	if (!object) return new Response("Not Found", { status: 404 });

	return new Response(object.body, { headers: { "Content-Type": "text/html; charset=utf-8" } });
}

export default {
	async fetch(request, env, ctx): Promise<Response> {
		const url = new URL(request.url);

		const feedMatch = url.pathname.match(/^\/feed\/([^/]+)\.atom$/);
		if (feedMatch) return handleFeedRequest(request, env, feedMatch[1]);

		const permalinkMatch = url.pathname.match(/^\/p\/([^/]+)$/);
		if (permalinkMatch) return handlePermalinkRequest(env, permalinkMatch[1]);

		switch (url.pathname) {
			case '/message':
				return new Response('Hello, World!');
			case '/random':
				return new Response(crypto.randomUUID());
			default:
				return new Response('Not Found', { status: 404 });
		}
	},
	// Superseded by ADR-0007 (Resend + n8n take over receiving/parsing/writes)
	// — deletion tracked separately as agregado#124, kept here unmodified
	// until that ticket lands so this change stays scoped to the read paths.
	async email(message, env, ctx) {
		const result = await PostalMime.parse(message.raw)

		const headers: Record<string, string> = {};
		result.headers.forEach(({key, value}) => {
			headers[key] = value
		})

		const payload = {
			"from": message.from,
	    "to": message.to,
	    "subject": result.subject,
			headers,
	    "text": result.text,
			"html": result.html,
		}

		try {
			const response = await fetch(env.WEBHOOK_URL, {
				method: 'POST',
				headers: {
					'X-Webhook-Secret': env.WEBHOOK_SECRET,
					'Content-Type': "application/json"
				},
				body: JSON.stringify(payload),
			})

			if (!response.ok) {
				throw new Error(`Webhook failed: ${response.status}`)
			}
		} catch (err) {
			throw new Error(`Webhook failed: ${err}`)
		}

		// Temporary subscription-confirm helper: forward a copy of every incoming
		// email to a secondary inbox so confirmation links can be clicked manually.
		// Best-effort only — a forward failure must NOT bounce/retry the message,
		// so unlike the webhook above we log and swallow rather than re-throw.
		if (env.FORWARD_EMAIL) {
			try {
				await message.forward(env.FORWARD_EMAIL)
			} catch (err) {
				console.error(`Forward to ${env.FORWARD_EMAIL} failed: ${err}`)
			}
		}
	}
} satisfies ExportedHandler<Env>;
