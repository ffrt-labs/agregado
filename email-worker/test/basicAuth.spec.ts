import { describe, expect, it } from "vitest";
import { hashSecret, parseBasicAuth, verifySecret } from "../src/basicAuth";

describe("hashSecret / verifySecret", () => {
	it("verifies a secret against its own hash", async () => {
		const hash = await hashSecret("correct-horse-battery-staple");
		expect(await verifySecret("correct-horse-battery-staple", hash)).toBe(true);
	});

	it("rejects a wrong secret", async () => {
		const hash = await hashSecret("correct-horse-battery-staple");
		expect(await verifySecret("wrong-secret", hash)).toBe(false);
	});

	it("produces a deterministic, hex-encoded hash", async () => {
		const hash = await hashSecret("same-input");
		expect(await hashSecret("same-input")).toBe(hash);
		expect(hash).toMatch(/^[0-9a-f]{64}$/);
	});
});

describe("parseBasicAuth", () => {
	it("returns null when there is no Authorization header", () => {
		const request = new Request("https://example.com/feed/tldr.atom");
		expect(parseBasicAuth(request)).toBeNull();
	});

	it("returns null for a non-Basic scheme", () => {
		const request = new Request("https://example.com/feed/tldr.atom", {
			headers: { Authorization: "Bearer some-token" },
		});
		expect(parseBasicAuth(request)).toBeNull();
	});

	it("decodes username and password from a Basic header", () => {
		const request = new Request("https://example.com/feed/tldr.atom", {
			headers: { Authorization: `Basic ${btoa("tldr:s3cret")}` },
		});
		expect(parseBasicAuth(request)).toEqual({ username: "tldr", password: "s3cret" });
	});

	it("returns null for malformed base64", () => {
		const request = new Request("https://example.com/feed/tldr.atom", {
			headers: { Authorization: "Basic not-valid-base64!!" },
		});
		expect(parseBasicAuth(request)).toBeNull();
	});
});
