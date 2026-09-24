// Entry identity is hash(Message-ID) end to end (agregado#100): the Atom
// <id>, the D1 primary key and the R2 object key. Redelivery of the same
// message therefore lands on the same rows.
const crypto = require("crypto");

function entryId(messageId) {
	const normalised = String(messageId || "").trim().replace(/^<|>$/g, "").trim();
	if (!normalised) throw new Error("missing Message-ID");
	return crypto.createHash("sha256").update(normalised).digest("hex");
}

// The permalink UUID is deterministic (a retry must reproduce it) but keyed:
// the publisher knows the Message-ID, so a bare hash of it would make the
// "unguessable" permalink guessable by the sender.
function permalinkUuid(id, secret) {
	if (!secret) throw new Error("permalink secret is required");
	const b = crypto.createHmac("sha256", secret).update(id).digest().subarray(0, 16);
	b[6] = (b[6] & 0x0f) | 0x40;
	b[8] = (b[8] & 0x3f) | 0x80;
	const h = b.toString("hex");
	return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}

module.exports = { entryId, permalinkUuid };
