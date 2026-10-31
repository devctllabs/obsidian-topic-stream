import type { Item } from "../../api/contracts";
import { fromMarkdown } from "mdast-util-from-markdown";

export interface ReaderImageReference { token: string; image: Item["images"][number] | undefined; alt: string; }
export interface PreparedMarkdown { markdown: string; images: ReaderImageReference[]; }

type MarkdownNode = ReturnType<typeof fromMarkdown> | ReturnType<typeof fromMarkdown>["children"][number];
interface Preparation { source: string; base: string; scope: string; serverImages: Item["images"]; images: ReaderImageReference[]; definitions: ReadonlyMap<string, string>; }

/** Remove active embeds before host rendering, preserving source formatting outside changed nodes. */
export function prepareMarkdown(item: Pick<Item, "contentMarkdown" | "canonicalUrl" | "images">, scope: string): PreparedMarkdown {
  const tree = fromMarkdown(item.contentMarkdown);
  const context: Preparation = { source: item.contentMarkdown, base: item.canonicalUrl, scope, serverImages: item.images, images: [], definitions: collectDefinitions(tree) };
  return { markdown: renderNode(tree, context), images: context.images };
}

function collectDefinitions(node: MarkdownNode, result = new Map<string, string>()): Map<string, string> {
  if (node.type === "definition") result.set(node.identifier, node.url);
  if ("children" in node) for (const child of node.children) collectDefinitions(child, result);
  return result;
}

function renderNode(node: MarkdownNode, context: Preparation): string {
  const raw = sourceText(node, context.source);
  switch (node.type) {
    // Host plugins can execute language-specific fences. Reader treats every source block as code.
    case "code": return node.lang ? raw.replace(/^([ \t]*(?:`{3,}|~{3,}))[^\r\n]*/, "$1") : raw;
    case "inlineCode": return raw;
    case "text": return raw.replaceAll("[[", "&#91;[");
    case "html": return raw.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");
    case "image": case "imageReference": return renderImage(node, context);
    case "link": case "linkReference": return renderLink(node, context);
    default: return renderChildren(node, context);
  }
}

function renderImage(node: Extract<MarkdownNode, { type: "image" | "imageReference" }>, context: Preparation): string {
  const url = node.type === "image" ? node.url : context.definitions.get(node.identifier);
  const image = context.serverImages.find((entry) => entry.url === url);
  const token = `https://topic-stream.invalid/reader-image/${encodeURIComponent(context.scope)}/${context.images.length}`;
  context.images.push({ token, image, alt: node.alt ?? image?.altText ?? "Image" });
  return `[Image](${token})`;
}

function renderLink(node: Extract<MarkdownNode, { type: "link" | "linkReference" }>, context: Preparation): string {
  const previousImages = context.images.length;
  const label = node.children.map((child) => renderNode(child, context)).join("");
  const url = node.type === "link" ? node.url : context.definitions.get(node.identifier);
  const destination = safeLink(url, context.base);
  if (!destination || context.images.length !== previousImages) return label;
  return `[${label}](<${destination.replaceAll(">", "%3E").replaceAll("<", "%3C")}>)`;
}

function safeLink(value: string | undefined, base: string): string | null {
  if (!value) return null;
  if (value.startsWith("#")) return value;
  try { const url = new URL(value, base); return ["https:", "http:"].includes(url.protocol) ? url.href : null; }
  catch { return null; }
}

function renderChildren(node: MarkdownNode, context: Preparation): string {
  if (!("children" in node)) return sourceText(node, context.source);
  const { start, end } = offsets(node);
  let position = start;
  let result = "";
  for (const child of node.children) {
    const span = offsets(child);
    result += context.source.slice(position, span.start) + renderNode(child, context);
    position = span.end;
  }
  return result + context.source.slice(position, end);
}

function sourceText(node: MarkdownNode, source: string): string {
  const { start, end } = offsets(node);
  return source.slice(start, end);
}

function offsets(node: MarkdownNode): { start: number; end: number } {
  const start = node.position?.start.offset;
  const end = node.position?.end.offset;
  if (start === undefined || end === undefined) throw new Error("Could not prepare the source text.");
  return { start, end };
}
