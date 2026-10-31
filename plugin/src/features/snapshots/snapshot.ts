import type { Item, Schema } from "../../api/contracts";
import { prepareSnapshotContent } from "./attachments";
import { hasFullText } from "./markdown";

export type SnapshotSource = { kind: "item"; item: Item; aiSummary: Schema<"ItemSummary"> | null } | { kind: "digest_issue"; issue: Schema<"DigestIssue"> };
export interface SnapshotOptions { folder: string; filename: string; content: "compact" | "full"; images: "links" | "remote" | "local"; confirmMixed?: boolean; }
export interface SnapshotAttachment { path: string; bytes: ArrayBuffer; }
export interface SnapshotPlan { path: string; markdown: string; attachments: SnapshotAttachment[]; }
export interface SnapshotVault { write: (plan: SnapshotPlan, signal: AbortSignal) => Promise<void>; }
export interface SnapshotResult { path: string; localImages: number; linkedImages: number; }
export interface SnapshotServices { vault: SnapshotVault; images: import("../reader/images").ReaderImageService; sanitizeSvg: (bytes: ArrayBuffer) => ArrayBuffer; }

export async function saveSnapshot(source: SnapshotSource, options: SnapshotOptions, services: SnapshotServices, signal: AbortSignal): Promise<SnapshotResult> {
  const path = snapshotPath(options.folder, options.filename);
  const missing = missingFullText(source);
  if (options.content === "full" && missing) {
    if (source.kind === "item") throw new Error("This Item has no full text or README. Choose Compact to save its excerpt.");
    if (!options.confirmMixed) throw new Error(`${missing} Item${missing === 1 ? "" : "s"} lack full text. Explicit confirmation is required.`);
  }
  const identity = source.kind === "item" ? source.item : source.issue;
  const metadata = { topic_stream_schema: 1, topic_stream_kind: source.kind, topic_stream_id: identity.id, title: identity.title, saved_at: new Date().toISOString(), ...(source.kind === "item" && sourceLink(source.item) ? { source_url: sourceLink(source.item) } : {}) };
  const frontmatter = Object.entries(metadata).map(([name, value]) => `${name}: ${typeof value === "number" || name === "topic_stream_kind" ? value : JSON.stringify(value)}`).join("\n");
  const prepared = await prepareSnapshotContent(source, options, { path, services, signal });
  const text = { full: options.content === "full", content: prepared.content };
  const body = source.kind === "item" ? itemText(source.item, source.item.excerpt, source.aiSummary, { ...text, level: 1 }) : digestText(source.issue, text);
  signal.throwIfAborted();
  await services.vault.write({ path, markdown: `---\n${frontmatter}\n---\n${body}\n`, attachments: prepared.attachments }, signal);
  return { path, localImages: prepared.attachments.length, linkedImages: prepared.linked };
}

export function snapshotPath(folder: string, filename: string): string {
  const segments = folder.trim().replaceAll("\\", "/").split("/").filter((part) => part && part !== ".");
  if (/^(?:[/\\]|[a-z]:)/i.test(folder.trim()) || segments.some((part) => part === ".." || /[\p{Cc}<>:"|?*]/u.test(part))) throw new Error("Choose a folder inside this vault, without parent paths or reserved characters.");
  const name = filename.trim().replace(/\.md$/i, "").trim();
  if (!name || name === "." || name === ".." || /[\p{Cc}/\\<>:"|?*#[\]^]/u.test(name)) throw new Error("Enter a filename without slashes or reserved characters.");
  return [...segments, `${name}.md`].join("/");
}

function sourceLink(item: Item): string | null { return webUrl(item.url) ?? webUrl(item.canonicalUrl); }
function webUrl(value: string): string | null {
  try { const url = new URL(value); return ["http:", "https:"].includes(url.protocol) ? url.href : null; }
  catch { return null; }
}
function plain(value: string): string { return value.replace(/[\\`*_[\]<>#]/g, "\\$&"); }
export function missingFullText(source: SnapshotSource): number {
  const items = source.kind === "item" ? [source.item] : source.issue.sections.flatMap((section) => section.items.map((saved) => saved.item));
  return items.filter((item) => !hasFullText(item)).length;
}
interface TextOptions { level: number; full: boolean; content: Map<Item, string>; }
function itemText(item: Item, excerpt: string, annotation: Schema<"ItemSummary"> | null, { level, full, content }: TextOptions): string {
  const summary = matchingSummary(item, annotation);
  return [`${"#".repeat(level)} ${plain(item.title.replaceAll("\n", " "))}`, itemMetadata(item), full && !hasFullText(item) ? "Excerpt only" : "", content.get(item) ?? plain(excerpt), summary ? `${"#".repeat(level + 1)} AI summary\n\n${plain(summary)}` : ""].filter(Boolean).join("\n\n");
}
const metadataLabels: Record<string, string> = { programmingLanguage: "Programming language", openIssues: "Open issues", defaultBranch: "Default branch", starsInPeriod: "Stars in period", observedAt: "Observed at", snapshotStatus: "Source snapshot status", channelId: "Channel ID", videoId: "Video ID", doi: "DOI" };
function itemMetadata(item: Item): string {
  const url = sourceLink(item);
  const canonical = webUrl(item.canonicalUrl);
  const links = [url ? `[Original](<${url.replaceAll(">", "%3E")}>)` : "", canonical && canonical !== url ? `[Canonical](<${canonical.replaceAll(">", "%3E")}>)` : ""].filter(Boolean).join(" · ");
  const fields = Object.entries(item.details).filter(([name]) => !["type", "description", "abstract"].includes(name)).map(([name, value]) => {
    const text = Array.isArray(value) ? value.join(", ") : value;
    if (text === null || text === "") return "";
    const label = metadataLabels[name] ?? name[0]!.toUpperCase() + name.slice(1);
    return `${label}: ${plain(String(text))}`;
  });
  return [links, `Kind: ${item.kind}`, item.authors.length ? `Authors: ${item.authors.map(plain).join(", ")}` : "", item.publishedAt ? `Published: ${item.publishedAt}` : "", `Discovered: ${item.discoveredAt}`, ...fields].filter(Boolean).join("\n\n");
}
function matchingSummary(item: Item, summary: Schema<"ItemSummary"> | null): string | null {
  return summary?.state === "ready" && summary.sourceVersion === item.summarySourceVersion && summary.result?.sourceVersion === item.summarySourceVersion ? summary.result.text : null;
}
function digestText(issue: Schema<"DigestIssue">, options: Omit<TextOptions, "level">): string {
  const sections = issue.sections.map((section) => [`## ${plain(section.topicName)}`, section.failure ? `Incomplete section: ${plain(section.failure)}` : "", ...section.items.map((saved) => itemText(saved.item, saved.summary || saved.item.excerpt, saved.aiSummary, { ...options, level: 3 }))].filter(Boolean).join("\n\n"));
  return [`# ${plain(issue.title)}`, `Digest definition: ${issue.digestId}\n\nGenerated: ${issue.generatedAt}\n\nStatus: ${issue.status}`, issue.scheduledFor ? `Scheduled for: ${issue.scheduledFor}` : "", ...sections].filter(Boolean).join("\n\n");
}
