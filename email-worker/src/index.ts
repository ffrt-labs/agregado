/**
 * Welcome to Cloudflare Workers! This is your first worker.
 *
 * - Run `npm run dev` in your terminal to start a development server
 * - Open a browser tab at http://localhost:8787/ to see your worker in action
 * - Run `npm run deploy` to publish your worker
 *
 * Bind resources to your worker in `wrangler.jsonc`. After adding bindings, a type definition for the
 * `Env` object can be regenerated with `npm run cf-typegen`.
 *
 * Learn more at https://developers.cloudflare.com/workers/
 */

import PostalMime from "postal-mime";

export default {
	async fetch(request, env, ctx): Promise<Response> {
		const url = new URL(request.url);
		switch (url.pathname) {
			case '/message':
				return new Response('Hello, World!');
			case '/random':
				return new Response(crypto.randomUUID());
			default:
				return new Response('Not Found', { status: 404 });
		}
	},
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
	}
} satisfies ExportedHandler<Env>;
