import { describe, expect, it } from "vitest";
import { buildAtomFeed, type FeedEntry } from "../src/atom";

const baseEntry: FeedEntry = {
	id: "deadbeef",
	permalinkUuid: "0f6f6e2a-0000-4000-8000-000000000000",
	canonicalUrl: null,
	title: "An Article",
	readableContent: "<p>Hello</p>",
	publishedAt: 1_700_000_000,
};

describe("buildAtomFeed", () => {
	it("produces a well-formed Atom document with the source's id and title", () => {
		const xml = buildAtomFeed({ sourceId: "tldr", displayName: "TLDR" }, [baseEntry], "https://bridge.example.com");
		expect(xml).toContain('<?xml version="1.0" encoding="UTF-8"?>');
		expect(xml).toContain("<title>TLDR</title>");
		expect(xml).toContain("<id>https://bridge.example.com/feed/tldr.atom</id>");
	});

	it("carries a feed-level author, per RFC 4287 §4.1.1", () => {
		const xml = buildAtomFeed({ sourceId: "tldr", displayName: "TLDR" }, [baseEntry], "https://bridge.example.com");
		expect(xml).toContain("<author>\n\t\t<name>TLDR</name>\n\t</author>");
	});

	it("links an entry to its canonical URL when one was recovered", () => {
		const entry = { ...baseEntry, canonicalUrl: "https://publisher.example.com/article" };
		const xml = buildAtomFeed({ sourceId: "tldr", displayName: "TLDR" }, [entry], "https://bridge.example.com");
		expect(xml).toContain('<link href="https://publisher.example.com/article"/>');
	});

	it("falls back to the /p/{uuid} permalink when there is no canonical URL", () => {
		const xml = buildAtomFeed({ sourceId: "tldr", displayName: "TLDR" }, [baseEntry], "https://bridge.example.com");
		expect(xml).toContain(`<link href="https://bridge.example.com/p/${baseEntry.permalinkUuid}"/>`);
	});

	it("carries the readable content as escaped HTML text, not raw markup", () => {
		const xml = buildAtomFeed({ sourceId: "tldr", displayName: "TLDR" }, [baseEntry], "https://bridge.example.com");
		expect(xml).toContain('<content type="html">&lt;p&gt;Hello&lt;/p&gt;</content>');
	});

	it("escapes a title containing XML-significant characters", () => {
		const entry = { ...baseEntry, title: "Cats & Dogs <3" };
		const xml = buildAtomFeed({ sourceId: "tldr", displayName: "TLDR" }, [entry], "https://bridge.example.com");
		expect(xml).toContain("<title>Cats &amp; Dogs &lt;3</title>");
	});

	it("renders no <entry> elements for an empty feed", () => {
		const xml = buildAtomFeed({ sourceId: "tldr", displayName: "TLDR" }, [], "https://bridge.example.com");
		expect(xml).not.toContain("<entry>");
	});

	it("renders entries in the order given (caller sorts newest-first)", () => {
		const older = { ...baseEntry, id: "older", publishedAt: 1_600_000_000 };
		const newer = { ...baseEntry, id: "newer", publishedAt: 1_700_000_000 };
		const xml = buildAtomFeed({ sourceId: "tldr", displayName: "TLDR" }, [newer, older], "https://bridge.example.com");
		expect(xml.indexOf("newer")).toBeLessThan(xml.indexOf("older"));
	});
});
