import { fromMarkdown } from "mdast-util-from-markdown";
import type { MarkdownRendererPort } from "../../features/reader/markdown";

type MarkdownNode = ReturnType<typeof fromMarkdown> | ReturnType<typeof fromMarkdown>["children"][number];
const tags: Partial<Record<MarkdownNode["type"], keyof HTMLElementTagNameMap>> = { paragraph: "p", emphasis: "em", strong: "strong", blockquote: "blockquote", listItem: "li", inlineCode: "code", link: "a", thematicBreak: "hr", break: "br" };

/** External host-port fixture; real Obsidian remains the renderer/lifecycle oracle in E2E. */
export const fixtureMarkdown: MarkdownRendererPort = { render(markdown, target) {
  target.replaceChildren(renderNode(fromMarkdown(markdown), target.ownerDocument));
  return { ready: Promise.resolve(), dispose: () => { target.replaceChildren(); } };
} };

function renderNode(node: MarkdownNode, document: Document): Node {
  if (node.type === "text") return document.createTextNode(node.value);
  if (["image", "imageReference"].includes(node.type)) throw new Error("A remote image reached the host before consent.");
  if (["definition", "footnoteDefinition"].includes(node.type)) return document.createDocumentFragment();
  if (node.type === "root") { const fragment = document.createDocumentFragment(); node.children.forEach((child) => fragment.appendChild(renderNode(child, document))); return fragment; }
  if (node.type === "code") { const pre = document.createElement("pre"); const code = document.createElement("code"); code.textContent = node.value; pre.append(code); return pre; }
  const element = document.createElement(tagName(node));
  if (node.type === "link") element.setAttribute("href", node.url);
  if ("children" in node) node.children.forEach((child) => element.appendChild(renderNode(child, document)));
  else if ("value" in node) element.textContent = node.value;
  return element;
}

function tagName(node: MarkdownNode): keyof HTMLElementTagNameMap {
  if (node.type === "heading") return `h${node.depth}`;
  if (node.type === "list") return node.ordered ? "ol" : "ul";
  return tags[node.type] ?? "span";
}
