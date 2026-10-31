import { Component, MarkdownRenderer, type App } from "obsidian";
import type { MarkdownRendererPort } from "../../features/reader/markdown";

/** Obsidian renders server text directly into the current view; no vault file is created. */
export function obsidianMarkdown(app: App, owner: Component): MarkdownRendererPort {
  return { render(markdown, target) {
    const child = owner.addChild(new Component());
    let disposed = false;
    const ready = Promise.resolve().then(async () => {
      if (!disposed) await MarkdownRenderer.render(app, markdown, target, "", child);
    }).finally(() => { if (disposed) child.unload(); });
    return { ready, dispose: () => { if (!disposed) { disposed = true; owner.removeChild(child); } } };
  } };
}
