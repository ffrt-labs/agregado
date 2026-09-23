// Per-Source feed protection (agregado#98): a real credential does the
// work, so the feed URL itself doesn't need to be secret, unlike the
// permalink. The secret is stored hashed (never plaintext) on the bridge
// side; SHA-256 is sufficient here — Miniflux, the only client, sends the
// real secret on every request rather than a derived proof, so there's no
// offline-guessing surface a slow KDF would defend against that a random,
// sufficiently long secret doesn't already close.

function toHex(buffer: ArrayBuffer): string {
	return [...new Uint8Array(buffer)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

export async function hashSecret(secret: string): Promise<string> {
	const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(secret));
	return toHex(digest);
}

// Constant-time-ish comparison of two equal-length hex digests, so a wrong
// guess doesn't leak how many leading characters it got right via early
// string-inequality short-circuiting.
function timingSafeEqual(a: string, b: string): boolean {
	if (a.length !== b.length) return false;
	let diff = 0;
	for (let i = 0; i < a.length; i++) {
		diff |= a.charCodeAt(i) ^ b.charCodeAt(i);
	}
	return diff === 0;
}

export async function verifySecret(secret: string, hash: string): Promise<boolean> {
	return timingSafeEqual(await hashSecret(secret), hash);
}

export interface BasicAuthCredentials {
	username: string;
	password: string;
}

export function parseBasicAuth(request: Request): BasicAuthCredentials | null {
	const header = request.headers.get("Authorization");
	if (!header?.startsWith("Basic ")) return null;

	let decoded: string;
	try {
		decoded = atob(header.slice("Basic ".length));
	} catch {
		return null;
	}

	const separatorIndex = decoded.indexOf(":");
	if (separatorIndex === -1) return null;

	return {
		username: decoded.slice(0, separatorIndex),
		password: decoded.slice(separatorIndex + 1),
	};
}
