// Hand-rolled AWS Signature Version 4 for a single PUT with a known, whole
// (non-streamed) body — not a general SigV4 client. Needed because n8n's
// built-in AWS credential signer doesn't reliably add x-amz-content-sha256
// for S3-compatible hosts outside *.amazonaws.com (Cloudflare R2), and R2
// rejects requests missing it. Hand-rolling it here follows the same
// approach already used for Resend's Svix signature (svix.js).
const crypto = require("crypto");

function sha256Hex(data) {
	return crypto.createHash("sha256").update(data, "utf8").digest("hex");
}

function hmac(key, data) {
	return crypto.createHmac("sha256", key).update(data, "utf8").digest();
}

function amzDate(now) {
	return now.toISOString().replace(/[:-]|\.\d{3}/g, "");
}

// Returns the exact URL and headers to send for a PUT of `body` to
// `https://{host}/{bucket}/{key}`, signed for the given (access key,
// secret, region) — region `auto` for R2.
function signS3Put({ accessKeyId, secretAccessKey, region, host, bucket, key, body, contentType, now = new Date() }) {
	if (!accessKeyId || !secretAccessKey) throw new Error("R2 access key id/secret are required");

	const date = amzDate(now);
	const dateStamp = date.slice(0, 8);
	const canonicalUri = `/${bucket}/${encodeURIComponent(key)}`;
	const payloadHash = sha256Hex(body);

	// Header names must be lowercase and sorted for both the signed-headers
	// list and the canonical-headers block (each line, including the last,
	// ends with \n — the join("\n") below then supplies the blank-line
	// separator the spec requires before SignedHeaders).
	const headers = {
		"content-type": contentType,
		host,
		"x-amz-content-sha256": payloadHash,
		"x-amz-date": date,
	};
	const sortedNames = Object.keys(headers).sort();
	const signedHeaders = sortedNames.join(";");
	const canonicalHeaders = sortedNames.map((k) => `${k}:${headers[k]}\n`).join("");

	const canonicalRequest = ["PUT", canonicalUri, "", canonicalHeaders, signedHeaders, payloadHash].join("\n");

	const credentialScope = `${dateStamp}/${region}/s3/aws4_request`;
	const stringToSign = ["AWS4-HMAC-SHA256", date, credentialScope, sha256Hex(canonicalRequest)].join("\n");

	const kDate = hmac(`AWS4${secretAccessKey}`, dateStamp);
	const kRegion = hmac(kDate, region);
	const kService = hmac(kRegion, "s3");
	const kSigning = hmac(kService, "aws4_request");
	const signature = hmac(kSigning, stringToSign).toString("hex");

	const authorization = `AWS4-HMAC-SHA256 Credential=${accessKeyId}/${credentialScope}, SignedHeaders=${signedHeaders}, Signature=${signature}`;

	return {
		url: `https://${host}${canonicalUri}`,
		headers: {
			"Content-Type": contentType,
			"x-amz-content-sha256": payloadHash,
			"x-amz-date": date,
			Authorization: authorization,
		},
	};
}

module.exports = { signS3Put };
