// Entry identity is hash(Message-ID) end to end (agregado#100): the Atom
// <id>, the D1 primary key and the R2 object key. The Message-ID is the one
// Resend delivers (rewritten on inbound, e.g. `...@eu-west-1.amazonses.com`),
// so the id is not derivable from the mail as sent (agregado#138). Resend
// redeliveries replay the same payload, so they land on the same rows.
const crypto = require("crypto");

function entryId(messageId) {
	const normalised = String(messageId || "").trim().replace(/^<|>$/g, "").trim();
	if (!normalised) throw new Error("missing Message-ID");
	return crypto.createHash("sha256").update(normalised).digest("hex");
}

// The permalink UUID is deterministic (a retry must reproduce it) but keyed:
// the bare hash is also the public Atom <id>, so on its own it would make the
// "unguessable" permalink derivable from it.
function permalinkUuid(id, secret) {
	if (!secret) throw new Error("permalink secret is required");
	const b = crypto.createHmac("sha256", secret).update(id).digest().subarray(0, 16);
	b[6] = (b[6] & 0x0f) | 0x40;
	b[8] = (b[8] & 0x3f) | 0x80;
	const h = b.toString("hex");
	return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}

module.exports = { entryId, permalinkUuid };
