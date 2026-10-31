import { describe, expect, it } from "vitest";
import { fromMarkdown } from "mdast-util-from-markdown";
import { fixtureId, makeItem } from "../../test/storybook/fixtures";
import { prepareMarkdown } from "./prepare-markdown";

describe("canonical Markdown preparation", () => {
  it("withholds inline/reference images and active HTML before host rendering while retaining prose and code", () => {
    const image = { id: fixtureId(800), url: "https://images.example/figure.png", altText: "A source diagram" };
    const code = "```md\n![Example](https://images.example/code-only.png)\n![[Vault example]]\n```";
    const item = makeItem(0, { images: [image], contentMarkdown: `A paragraph with **emphasis**.\n\n![A source diagram](https://images.example/figure.png)\n\n![Second][figure]\n\n[figure]: https://images.example/figure.png\n\n<img src="https://images.example/raw.png">\n\n![[Private note]] and [[Another note]]\n\n${code}` });
    const prepared = prepareMarkdown(item, "reader-test");
    expect(prepared.markdown).toContain("A paragraph with **emphasis**.");
    const parsed = fromMarkdown(prepared.markdown);
    expect(parsed.children.filter((node) => node.type === "code")).toMatchObject([{ lang: null, value: "![Example](https://images.example/code-only.png)\n![[Vault example]]" }]);
    const active: string[] = [];
    function inspect(node: typeof parsed | (typeof parsed)["children"][number]): void {
      if (["image", "imageReference", "html"].includes(node.type)) active.push(node.type);
      if ("children" in node) node.children.forEach(inspect);
    }
    inspect(parsed);
    expect(active).toEqual([]);
    expect(prepared.markdown).not.toContain("![[Private note]]");
    expect(prepared.markdown).not.toContain("[[Another note]]");
    expect(prepared.images).toHaveLength(2);
    expect(prepared.images.map((entry) => entry.image?.id)).toEqual([image.id, image.id]);
    expect(new Set(prepared.images.map((entry) => entry.token)).size).toBe(2);
  });

  it.each([
    "```topic-stream-fixture\nThis is source text, not an instruction.\n```",
    "~~~dataviewjs\nThis is source text, not an instruction.\n~~~",
    "> ```mermaid title=example\n> This is source text, not an instruction.\n> ```"
  ])("keeps fenced source code inert even when a host plugin handles its language: %s", (contentMarkdown) => {
    const prepared = prepareMarkdown(makeItem(0, { contentMarkdown }), "code-reader");
    const tree = fromMarkdown(prepared.markdown);
    const code: { value: string; lang: string | null | undefined }[] = [];
    function inspect(node: typeof tree | (typeof tree)["children"][number]): void {
      if (node.type === "code") code.push({ value: node.value, lang: node.lang });
      if ("children" in node) node.children.forEach(inspect);
    }
    inspect(tree);
    expect(code).toEqual([{ value: "This is source text, not an instruction.", lang: null }]);
  });

  it("preserves inline code and indented code that contains a literal fence example", () => {
    const contentMarkdown = "    ```dataviewjs\n    A literal fence example.\n    ```\n\n`topic-stream-fixture`";
    expect(prepareMarkdown(makeItem(0, { contentMarkdown }), "literal-code").markdown).toBe(contentMarkdown);
  });

  it("keeps images without server IDs unavailable and resolves web links without opening vault paths", () => {
    const item = makeItem(0, { canonicalUrl: "https://example.org/guide/", contentMarkdown: "![Unavailable](https://images.example/missing.png)\n\n[More](chapter) and [Unsafe](obsidian://new?vault=private)." });
    const prepared = prepareMarkdown(item, "other-reader");
    expect(prepared.images).toHaveLength(1);
    expect(prepared.images[0]?.image).toBeUndefined();
    expect(prepared.images[0]?.alt).toBe("Unavailable");
    expect(prepared.markdown).toContain("https://example.org/guide/chapter");
    expect(prepared.markdown).not.toContain("obsidian://");
    expect(prepared.markdown).toContain("Unsafe");
  });
});
