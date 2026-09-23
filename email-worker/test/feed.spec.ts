import { env, createExecutionContext, waitOnExecutionContext } from "cloudflare:test";
import { describe, expect, it } from "vitest";
import worker from "../src";
import { hashSecret } from "../src/basicAuth";

const THIRTY_DAYS_SECONDS = 30 * 24 * 60 * 60;
const now = Math.floor(Date.now() / 1000);

// D1/R2 storage isn't reset between tests within this file (observed, not
// configured), so every source id must be unique per test rather than
// relying on isolation — otherwise a second INSERT of the same literal id
// hits sources' PRIMARY KEY constraint.
function uniqueSourceId(): string {
	return `source-${crypto.randomUUID()}`;
}

async function seedSource(id: string, opts: { secret?: string } = {}) {
	await env.BRIDGE_DB.prepare(
		"INSERT INTO sources (id, display_name, basic_auth_secret_hash, status) VALUES (?, ?, ?, 'active')"
	)
		.bind(id, id.toUpperCase(), await hashSecret(opts.secret ?? "secret"))
		.run();
}

async function seedEntry(sourceId: string, overrides: Partial<{ id: string; canonicalUrl: string | null; publishedAt: number }> = {}) {
	const id = overrides.id ?? crypto.randomUUID();
	await env.BRIDGE_DB.prepare(
		"INSERT INTO entries (id, source_id, canonical_url, permalink_uuid, title, readable_content, published_at) VALUES (?, ?, ?, ?, ?, ?, ?)"
	)
		.bind(id, sourceId, overrides.canonicalUrl ?? null, crypto.randomUUID(), `Entry ${id}`, "<p>content</p>", overrides.publishedAt ?? now)
		.run();
	return id;
}

async function fetchFeed(path: string, auth?: string): Promise<Response> {
	const request = new Request(`https://bridge.example.com${path}`, {
		headers: auth ? { Authorization: auth } : undefined,
	});
	const ctx = createExecutionContext();
	const response = await worker.fetch(request, env, ctx);
	await waitOnExecutionContext(ctx);
	return response;
}

function basic(username: string, password: string): string {
	return `Basic ${btoa(`${username}:${password}`)}`;
}

describe("GET /feed/:sourceId.atom", () => {
	it("returns 404 for an unknown source", async () => {
		const response = await fetchFeed(`/feed/${uniqueSourceId()}.atom`, basic("unknown", "whatever"));
		expect(response.status).toBe(404);
	});

	it("returns 401 with no Authorization header", async () => {
		const sourceId = uniqueSourceId();
		await seedSource(sourceId);
		const response = await fetchFeed(`/feed/${sourceId}.atom`);
		expect(response.status).toBe(401);
		expect(response.headers.get("WWW-Authenticate")).toContain("Basic");
	});

	it("returns 401 for a wrong secret", async () => {
		const sourceId = uniqueSourceId();
		await seedSource(sourceId, { secret: "correct" });
		const response = await fetchFeed(`/feed/${sourceId}.atom`, basic(sourceId, "wrong"));
		expect(response.status).toBe(401);
	});

	it("returns 200 with a valid Atom document for correct credentials", async () => {
		const sourceId = uniqueSourceId();
		await seedSource(sourceId, { secret: "correct" });
		await seedEntry(sourceId);
		const response = await fetchFeed(`/feed/${sourceId}.atom`, basic(sourceId, "correct"));
		expect(response.status).toBe(200);
		expect(response.headers.get("Content-Type")).toContain("application/atom+xml");
		const body = await response.text();
		expect(body).toContain("<feed");
		expect(body).toContain(sourceId.toUpperCase());
	});

	it("excludes entries older than the rolling 30-day window", async () => {
		const sourceId = uniqueSourceId();
		await seedSource(sourceId, { secret: "correct" });
		const recentId = await seedEntry(sourceId, { publishedAt: now - 1000 });
		const staleId = await seedEntry(sourceId, { publishedAt: now - THIRTY_DAYS_SECONDS - 1000 });
		const response = await fetchFeed(`/feed/${sourceId}.atom`, basic(sourceId, "correct"));
		const body = await response.text();
		expect(body).toContain(recentId);
		expect(body).not.toContain(staleId);
	});

	it("does not leak another source's entries into this feed", async () => {
		const mineSourceId = uniqueSourceId();
		const otherSourceId = uniqueSourceId();
		await seedSource(mineSourceId, { secret: "correct" });
		await seedSource(otherSourceId, { secret: "correct" });
		const mineId = await seedEntry(mineSourceId);
		const otherId = await seedEntry(otherSourceId);
		const response = await fetchFeed(`/feed/${mineSourceId}.atom`, basic(mineSourceId, "correct"));
		const body = await response.text();
		expect(body).toContain(mineId);
		expect(body).not.toContain(otherId);
	});
});
