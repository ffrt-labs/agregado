// The feed entry carries the readable content (agregado#95's settled
// note): <content type="html"> holds the extraction, not the sanitized
// original — that lives behind the permalink instead (agregado#101's two
// policies off one sanitizer). This module only renders what's already in
// D1; it never fetches R2 or re-sanitizes anything.

export interface FeedEntry {
	id: string;
	permalinkUuid: string;
	canonicalUrl: string | null;
	title: string;
	readableContent: string;
	/** Unix seconds. */
	publishedAt: number;
}

export interface FeedSource {
	sourceId: string;
	displayName: string;
}

function escapeXml(value: string): string {
	return value
		.replace(/&/g, "&amp;")
		.replace(/</g, "&lt;")
		.replace(/>/g, "&gt;")
		.replace(/"/g, "&quot;")
		.replace(/'/g, "&apos;");
}

function toIso(unixSeconds: number): string {
	return new Date(unixSeconds * 1000).toISOString();
}

// The digest for an email-only Article *is* the Index's permanent key
// (agregado#95's settled note), so the entry link must point at whichever
// URL the Article Index is keyed on: the canonical URL if recovered, the
// bridge's own permalink if not — never a fallback chain, since the
// permalink always resolves (agregado#108).
function entryLink(entry: FeedEntry, bridgeOrigin: string): string {
	return entry.canonicalUrl ?? `${bridgeOrigin}/p/${entry.permalinkUuid}`;
}

export function buildAtomFeed(source: FeedSource, entries: FeedEntry[], bridgeOrigin: string): string {
	const feedId = `${bridgeOrigin}/feed/${source.sourceId}.atom`;
	const updated = toIso(entries[0]?.publishedAt ?? Math.floor(Date.now() / 1000));

	const entryXml = entries
		.map(
			(entry) => `\t<entry>
		<id>${escapeXml(entry.id)}</id>
		<title>${escapeXml(entry.title)}</title>
		<updated>${toIso(entry.publishedAt)}</updated>
		<link href="${escapeXml(entryLink(entry, bridgeOrigin))}"/>
		<content type="html">${escapeXml(entry.readableContent)}</content>
	</entry>`
		)
		.join("\n");

	return `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
	<id>${escapeXml(feedId)}</id>
	<title>${escapeXml(source.displayName)}</title>
	<updated>${updated}</updated>
${entryXml}
</feed>
`;
}
