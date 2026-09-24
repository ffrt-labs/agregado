// Svix webhook signature check (Resend signs deliveries with Svix). Hand-rolled
// per the plan: HMAC-SHA256 over `${svix-id}.${svix-timestamp}.${raw_body}`
// with the base64 key behind the `whsec_` secret. Needs the RAW body — any
// JSON re-serialisation changes the bytes and breaks the signature.
const crypto = require("crypto");

const TOLERANCE_SECONDS = 5 * 60;

function verifySvix({ secret, headers, rawBody, now = Math.floor(Date.now() / 1000) }) {
	const h = {};
	for (const [k, v] of Object.entries(headers || {})) h[k.toLowerCase()] = v;
	const id = h["svix-id"];
	const timestamp = h["svix-timestamp"];
	const signatures = h["svix-signature"];
	if (!id || !timestamp || !signatures || typeof rawBody !== "string" || !secret) return false;

	const ts = Number(timestamp);
	if (!Number.isFinite(ts) || Math.abs(now - ts) > TOLERANCE_SECONDS) return false;

	const key = Buffer.from(secret.replace(/^whsec_/, ""), "base64");
	const expected = crypto.createHmac("sha256", key).update(`${id}.${timestamp}.${rawBody}`).digest();

	return String(signatures)
		.split(" ")
		.some((part) => {
			const [version, sig] = part.split(",");
			if (version !== "v1" || !sig) return false;
			const given = Buffer.from(sig, "base64");
			return given.length === expected.length && crypto.timingSafeEqual(given, expected);
		});
}

module.exports = { verifySvix };
