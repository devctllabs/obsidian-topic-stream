import { fromMarkdown } from "mdast-util-from-markdown";
import type { Item } from "../../api/contracts";
import { prepareMarkdown } from "../reader/prepare-markdown";

type Node = ReturnType<typeof fromMarkdown> | ReturnType<typeof fromMarkdown>["children"][number];
export interface SnapshotImage { token: string; url: string | null; alt: string; descriptor: Item["images"][number] | undefined; }
export function hasFullText(item: Item): boolean { return ["article_body", "repository_readme"].includes(item.contentOrigin) && Boolean(item.contentMarkdown.trim()); }

/** Reader's inert Markdown rules also protect a saved note from active host embeds/fences. */
export function snapshotMarkdown(item: Item): { markdown: string; images: SnapshotImage[] } {
  const source = figuresToMarkdown(item.contentMarkdown);
  const tree = fromMarkdown(source);
  const definitions = new Map<string, string>();
  visit(tree, (node) => { if (node.type === "definition") definitions.set(node.identifier, node.url); });
  const descriptors = [...item.images];
  visit(tree, (node) => {
    if (node.type !== "image" && node.type !== "imageReference") return;
    const url = node.type === "image" ? node.url : definitions.get(node.identifier);
    if (url && !descriptors.some((image) => image.url === url)) descriptors.push({ id: "", url, altText: null });
  });
  const prepared = prepareMarkdown({ ...item, contentMarkdown: source, images: descriptors }, `snapshot-${item.id}`);
  const images = prepared.images.map((reference, index) => {
    const url = safeImageUrl(reference.image?.url, item.canonicalUrl);
    const descriptor = item.images.find((image) => safeImageUrl(image.url, item.canonicalUrl) === url);
    return { token: reference.token, url, descriptor, alt: reference.alt.trim() || descriptor?.altText?.trim() || filenameAlt(url) || `Image ${index + 1}` };
  });
  return { markdown: prepared.markdown, images };
}

function visit(node: Node, action: (node: Node) => void): void {
  action(node);
  if ("children" in node) node.children.forEach((child) => visit(child, action));
}
function safeImageUrl(value: string | undefined, base: string): string | null {
  if (!value) return null;
  try { const url = new URL(value, base); return ["https:", "http:"].includes(url.protocol) ? url.href : null; }
  catch { return null; }
}
function filenameAlt(url: string | null): string {
  if (!url) return "";
  try { return decodeURIComponent(new URL(url).pathname.split("/").at(-1) ?? "").replace(/\.[a-z0-9]{2,5}$/i, "").replace(/[-_]+/g, " ").trim(); }
  catch { return ""; }
}
function attribute(tag: string, name: string): string {
  const match = new RegExp(`\\s${name}\\s*=\\s*(?:"([^"]*)"|'([^']*)'|([^\\s>]+))`, "i").exec(tag);
  return (match?.[1] ?? match?.[2] ?? match?.[3] ?? "").replaceAll("&amp;", "&").replaceAll("&quot;", '"').replaceAll("&#39;", "'");
}
function figuresToMarkdown(source: string): string {
  return source.replace(/<figure\b[^>]*>([\s\S]*?)<\/figure\s*>/gi, (_figure, body: string) => {
    const caption = /<figcaption\b[^>]*>([\s\S]*?)<\/figcaption\s*>/i.exec(body)?.[1]?.replace(/<[^>]*>/g, "").trim() ?? "";
    return body.replace(/<figcaption\b[^>]*>[\s\S]*?<\/figcaption\s*>/gi, "").replace(/<img\b[^>]*>/gi, (tag) => {
      const url = attribute(tag, "src").replaceAll("<", "%3C").replaceAll(">", "%3E");
      const alt = (attribute(tag, "alt").trim() || caption).replace(/[\\[\]]/g, "\\$&").replaceAll("\n", " ");
      return url ? `![${alt}](<${url}>)` : "";
    });
  });
}
