import { env, createExecutionContext, waitOnExecutionContext } from "cloudflare:test";
import { describe, expect, it } from "vitest";
import worker from "../src";

// See feed.spec.ts's uniqueSourceId note: storage isn't reset between tests
// in this file, so every source id must be unique per test.
function uniqueSourceId(): string {
	return `source-${crypto.randomUUID()}`;
}

async function seedSource(sourceId: string) {
	await env.BRIDGE_DB.prepare(
		"INSERT INTO sources (id, display_name, basic_auth_secret_hash, status) VALUES (?, 'Source', 'unused', 'active')"
	)
		.bind(sourceId)
		.run();
}

async function seedEntry(sourceId: string): Promise<{ entryId: string; permalinkUuid: string }> {
	const entryId = crypto.randomUUID();
	const permalinkUuid = crypto.randomUUID();
	await env.BRIDGE_DB.prepare(
		"INSERT INTO entries (id, source_id, canonical_url, permalink_uuid, title, readable_content, published_at) VALUES (?, ?, NULL, ?, 'Title', '<p>readable</p>', ?)"
	)
		.bind(entryId, sourceId, permalinkUuid, Math.floor(Date.now() / 1000))
		.run();
	return { entryId, permalinkUuid };
}

async function fetchPermalink(uuid: string): Promise<Response> {
	const request = new Request(`https://bridge.example.com/p/${uuid}`);
	const ctx = createExecutionContext();
	const response = await worker.fetch(request, env, ctx);
	await waitOnExecutionContext(ctx);
	return response;
}

describe("GET /p/:uuid", () => {
	it("returns 404 for an unknown permalink UUID", async () => {
		const response = await fetchPermalink(crypto.randomUUID());
		expect(response.status).toBe(404);
	});

	it("serves the stored original HTML with an HTML content type", async () => {
		const sourceId = uniqueSourceId();
		await seedSource(sourceId);
		const { entryId, permalinkUuid } = await seedEntry(sourceId);
		await env.BRIDGE_BUCKET.put(entryId, "<html><body>original</body></html>");

		const response = await fetchPermalink(permalinkUuid);
		expect(response.status).toBe(200);
		expect(response.headers.get("Content-Type")).toContain("text/html");
		expect(await response.text()).toBe("<html><body>original</body></html>");
	});

	it("returns 404 when the D1 row exists but its R2 object is missing", async () => {
		const sourceId = uniqueSourceId();
		await seedSource(sourceId);
		const { permalinkUuid } = await seedEntry(sourceId);
		// deliberately no env.BRIDGE_BUCKET.put() for this entry's id
		const response = await fetchPermalink(permalinkUuid);
		expect(response.status).toBe(404);
	});
});
