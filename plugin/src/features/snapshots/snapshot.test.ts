import { describe, expect, it, vi } from "vitest";
import { makeContentCases, makeEntry, makeIssue, makeItem, makeSummary } from "../../test/storybook/fixtures";
import { saveSnapshot, type SnapshotPlan } from "./snapshot";

const signal = new AbortController().signal;
const options = { folder: "Reading//Weekly/", filename: "Saved article.md", content: "compact", images: "links" } as const;
function vault() { const plans: SnapshotPlan[] = []; return { plans, vault: { write: async (plan: SnapshotPlan) => { plans.push(plan); } }, images: { downloadItemImage: async () => ({ bytes: new ArrayBuffer(4), contentType: "image/png" }) }, sanitizeSvg: (bytes: ArrayBuffer) => bytes }; }

describe("explicit independent snapshots", () => {
  it("saves a normalized Compact Item with fixed escaped metadata and an already-ready annotation", async () => {
    const item = makeItem(0, { title: 'Notes: "quoted"\nSecond line', excerpt: "Original ordinary excerpt", contentMarkdown: "Full article body" });
    const output = vault();
    const result = await saveSnapshot({ kind: "item", item, aiSummary: makeSummary({ state: "ready", canGenerate: false, unavailableReason: "generation_disabled" }) }, options, output, signal);
    expect(result.path).toBe("Reading/Weekly/Saved article.md");
    const text = output.plans[0]!.markdown;
    expect(text).toMatch(/^---\ntopic_stream_schema: 1\ntopic_stream_kind: item\n/);
    expect(text).toContain(`topic_stream_id: "${item.id}"`);
    expect(text).toContain('title: "Notes: \\"quoted\\"\\nSecond line"');
    expect(text).toMatch(/saved_at: "\d{4}-\d{2}-\d{2}T.*Z"/);
    expect(text).toContain("Original ordinary excerpt\n\n## AI summary\n\nSynthetic AI summary fixture.");
    expect(text).not.toContain("Full article body");
    expect(output.plans[0]!.attachments).toEqual([]);
  });
  it("keeps captured Topic order, excerpts, source links and exact annotations in a Compact Digest", async () => {
    const issue = makeIssue([makeEntry(0), makeEntry(1)]);
    issue.sections[0]!.items[0]!.summary = "Captured ordinary text";
    issue.sections[0]!.items[0]!.aiSummary = makeSummary({ state: "ready", canGenerate: false, unavailableReason: "historical_input" });
    const output = vault();
    await saveSnapshot({ kind: "digest_issue", issue }, options, output, signal);
    const text = output.plans[0]!.markdown;
    expect(text).toContain("topic_stream_kind: digest_issue");
    expect(text).toContain(`## ${issue.sections[0]!.topicName}\n\n### ${issue.sections[0]!.items[0]!.item.title}`);
    expect(text).toContain("Captured ordinary text\n\n#### AI summary\n\nSynthetic AI summary fixture.");
    expect(text.indexOf("Systems 1")).toBeLessThan(text.indexOf("Retrieval 2"));
    expect(text).toContain("https://example.org/items/1");
  });
  it("requires an explicit Compact switch for description-only inputs and consent for a mixed Full Digest", async () => {
    const item = makeItem(1, { contentOrigin: "paper_abstract", contentMarkdown: "Only an abstract", summarySupported: false });
    const output = vault();
    await expect(saveSnapshot({ kind: "item", item, aiSummary: null }, { ...options, content: "full" }, output, signal)).rejects.toThrow(/full text.*Compact/i);
    const issue = makeIssue([makeEntry(0, { contentMarkdown: "Captured full article" }), makeEntry(1, item)]);
    await expect(saveSnapshot({ kind: "digest_issue", issue }, { ...options, content: "full" }, output, signal)).rejects.toThrow(/1 Item.*confirmation/);
    expect(output.plans).toHaveLength(0);
    await saveSnapshot({ kind: "digest_issue", issue }, { ...options, content: "full", confirmMixed: true }, output, signal);
    expect(output.plans[0]!.markdown).toContain("Captured full article");
    expect(output.plans[0]!.markdown).toContain("Excerpt only\n\nA synthetic excerpt");
    expect(output.plans[0]!.markdown).not.toContain("Only an abstract");
  });
  it.each(["compact", "full"] as const)("omits absent, pending, failed and mismatched annotations in %s without waiting or later modification", async (content) => {
    for (const annotation of [null, makeSummary(), makeSummary({ state: "pending" }), makeSummary({ state: "failed" }), makeSummary({ state: "ready", sourceVersion: "b".repeat(64) })]) {
      const output = vault();
      await saveSnapshot({ kind: "item", item: makeItem(), aiSummary: annotation }, { ...options, content }, output, signal);
      const saved = output.plans[0]!.markdown;
      expect(saved).not.toContain("AI summary");
      if (annotation) annotation.state = "ready";
      expect(output.plans[0]!.markdown).toBe(saved);
    }
  });
  it("retains exact ready Full annotation and original content without executing host Markdown plugins", async () => {
    const output = vault();
    const item = makeItem(0, { contentMarkdown: "**Original**\n\n```dataviewjs\nthrow new Error()\n```\n\n![[Private note]]<script>bad()</script>" });
    await saveSnapshot({ kind: "item", item, aiSummary: makeSummary({ state: "ready", canGenerate: false }) }, { ...options, content: "full" }, output, signal);
    expect(output.plans[0]!.markdown).toContain("**Original**");
    expect(output.plans[0]!.markdown).toContain("\n```\nthrow new Error()");
    expect(output.plans[0]!.markdown).not.toContain("![[");
    expect(output.plans[0]!.markdown).not.toContain("<script>");
    expect(output.plans[0]!.markdown).toContain("## AI summary");
  });
  it.each(["../outside", "/absolute", "C:\\Users", "safe/../../private", "bad\u0000path"])("rejects unsafe folder %s before writes", async (folder) => {
    const output = vault();
    await expect(saveSnapshot({ kind: "item", item: makeItem(), aiSummary: null }, { ...options, folder }, output, signal)).rejects.toThrow(/folder/);
    expect(output.plans).toEqual([]);
  });
  it("round-trips multiline and YAML-like strings without adding metadata fields", async () => {
    const output = vault();
    const title = 'yes\nsource_url: "http://secret"\n---\n\t\\ café: # value';
    await saveSnapshot({ kind: "item", item: makeItem(0, { title }), aiSummary: null }, options, output, signal);
    const lines = output.plans[0]!.markdown.split("---\n")[1]!.trim().split("\n");
    const fields = Object.fromEntries(lines.map((line) => { const index = line.indexOf(": "); return [line.slice(0, index), line.slice(index + 2)]; }));
    expect(JSON.parse(fields.title!)).toBe(title);
    expect(Object.keys(fields)).toEqual(["topic_stream_schema", "topic_stream_kind", "topic_stream_id", "title", "saved_at", "source_url"]);
  });
  it.each(["links", "remote", "local"] as const)("composes %s images through captured descriptors only", async (images) => {
    const output = vault();
    const download = vi.spyOn(output.images, "downloadItemImage");
    const url = "https://example.org/assets/reading-chart.png";
    const item = makeItem(0, { contentMarkdown: `![](${url})\n\n![Historical](https://example.org/old.png)`, images: [{ id: "captured-image", url, altText: null }] });
    const result = await saveSnapshot({ kind: "item", item, aiSummary: null }, { ...options, content: "full", images }, output, signal);
    const plan = output.plans[0]!;
    expect(plan.markdown).not.toContain("topic-stream.invalid");
    if (images === "local") {
      expect(download).toHaveBeenCalledExactlyOnceWith(item.id, "captured-image");
      expect(plan.attachments).toHaveLength(1);
      expect(plan.attachments[0]!.path).toMatch(/Saved article\.assets\/00000300-1-[a-f0-9]{8}\.png$/);
      expect(plan.markdown).toMatch(/!\[reading chart\]\(<Saved%20article.assets\//);
      expect(plan.markdown).toContain("[Historical](<https://example.org/old.png>)");
      expect(result.linkedImages).toBe(1);
    } else {
      expect(download).not.toHaveBeenCalled();
      expect(plan.attachments).toEqual([]);
      expect(plan.markdown).toContain(`${images === "remote" ? "!" : ""}[reading chart](<${url}>)`);
    }
  });
  it("keeps failed and over-budget downloads as links, with the 30-file limit shared across a Digest", async () => {
    const output = vault();
    const entries = Array.from({ length: 33 }, (_, index) => { const url = `https://example.org/${index}.png`; return makeEntry(index, { contentMarkdown: `![Image ${index}](${url})`, images: [{ id: `image-${index}`, url, altText: null }] }); });
    output.images.downloadItemImage = vi.fn(async (_itemId?: string, imageId?: string) => {
      if (imageId === "image-0") throw new Error("No longer available");
      return { bytes: new ArrayBuffer(imageId === "image-1" ? 10 * 1024 * 1024 + 1 : 4), contentType: "image/png" };
    });
    const result = await saveSnapshot({ kind: "digest_issue", issue: makeIssue(entries) }, { ...options, content: "full", images: "local" }, output, signal);
    expect(result.localImages).toBe(30);
    expect(result.linkedImages).toBe(3);
    expect(output.plans[0]!.attachments).toHaveLength(30);
    expect(output.plans[0]!.markdown).toContain("[Image 0](<https://example.org/0.png>)");
    expect(output.plans[0]!.markdown).toContain("[Image 1](<https://example.org/1.png>)");
  });
  it("enforces the 100 MB aggregate before any vault writes and ignores results after dismissal", async () => {
    const output = vault();
    const bytes = new ArrayBuffer(10 * 1024 * 1024);
    output.images.downloadItemImage = async () => ({ bytes, contentType: "image/png" });
    const entries = Array.from({ length: 11 }, (_, index) => { const url = `https://example.org/${index}.png`; return makeEntry(index, { contentMarkdown: `![](${url})`, images: [{ id: `image-${index}`, url, altText: null }] }); });
    const result = await saveSnapshot({ kind: "digest_issue", issue: makeIssue(entries) }, { ...options, content: "full", images: "local" }, output, signal);
    expect(result).toMatchObject({ localImages: 10, linkedImages: 1 });
    expect(output.plans[0]!.attachments.reduce((sum, asset) => sum + asset.bytes.byteLength, 0)).toBe(100 * 1024 * 1024);
    const controller = new AbortController();
    output.images.downloadItemImage = async () => { controller.abort(); return { bytes, contentType: "image/png" }; };
    await expect(saveSnapshot({ kind: "item", item: entries[0]!.item, aiSummary: null }, { ...options, content: "full", images: "local" }, output, controller.signal)).rejects.toThrow();
    expect(output.plans).toHaveLength(1);
  });
  it("retains encoded historical source URLs when the captured descriptor is no longer downloadable", async () => {
    const output = vault(); const url = "https://example.org/old%20diagram.svg?revision=a%2Fb";
    const item = makeItem(0, { contentMarkdown: `![Original diagram](${url})`, images: [{ id: "historical-descriptor", url, altText: null }] });
    output.images.downloadItemImage = vi.fn(async () => { throw new Error("Captured descriptor no longer exists"); });
    const result = await saveSnapshot({ kind: "item", item, aiSummary: null }, { ...options, content: "full", images: "local" }, output, signal);
    expect(output.images.downloadItemImage).toHaveBeenCalledExactlyOnceWith(item.id, "historical-descriptor");
    expect(output.plans[0]!.markdown).toContain(`[Original diagram](<${url}>)`);
    expect(result).toMatchObject({ localImages: 0, linkedImages: 1 });
  });
  it("keeps available repository and Digest date metadata as ordinary text outside fixed frontmatter", async () => {
    const output = vault(); const item = makeContentCases().readme;
    item.url = "https://example.org/original"; item.canonicalUrl = "https://example.org/canonical";
    await saveSnapshot({ kind: "item", item, aiSummary: null }, { ...options, content: "full" }, output, signal);
    const text = output.plans[0]!.markdown;
    expect(text).toContain("[Original](<https://example.org/original>)");
    expect(text).toContain("[Canonical](<https://example.org/canonical>)");
    expect(text).toContain("Programming language: TypeScript");
    expect(text).toContain("Stars: 100");
    expect(text).toContain("Open issues: 2");
    const issue = { ...makeIssue(), scheduledFor: "2026-10-20T09:00:00Z" };
    await saveSnapshot({ kind: "digest_issue", issue }, options, output, signal);
    expect(output.plans[1]!.markdown).toContain("Scheduled for: 2026-10-20T09:00:00Z");
  });
});
