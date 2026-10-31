import { Component, type App } from "obsidian";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { deferred } from "../../test/storybook/deferred";

type RenderArguments = [app: unknown, text: string, target: unknown, path: string, owner: { register: (cleanup: () => void) => void }];
const host = vi.hoisted(() => ({ render: vi.fn<(...args: RenderArguments) => Promise<void>>() }));
vi.mock("obsidian", () => ({
  Component: class {
    private children = new Set<Component>();
    private cleanups = new Set<() => void>();
    public addChild(child: Component) { this.children.add(child); return child; }
    public removeChild(child: Component) { this.children.delete(child); child.unload(); }
    public register(cleanup: () => void) { this.cleanups.add(cleanup); }
    public unload() { this.children.forEach((child) => child.unload()); this.children.clear(); this.cleanups.forEach((cleanup) => cleanup()); this.cleanups.clear(); }
  },
  MarkdownRenderer: { render: host.render }
}));

import { obsidianMarkdown } from "./markdown";

describe("Obsidian Markdown host boundary", () => {
  beforeEach(() => { host.render.mockReset(); host.render.mockResolvedValue(); });
  it("renders in a child Component without a temporary vault path and disposes ownership once", async () => {
    const app = {} as App;
    const owner = new Component();
    const remove = vi.spyOn(owner, "removeChild");
    const target = {} as HTMLElement;
    const rendering = obsidianMarkdown(app, owner).render("## Canonical source", target);
    await rendering.ready;
    expect(host.render).toHaveBeenCalledWith(app, "## Canonical source", target, "", expect.any(Component));
    rendering.dispose(); rendering.dispose();
    expect(remove).toHaveBeenCalledTimes(1);
  });
  it("releases registrations made after the view disposed a pending render", async () => {
    const pending = deferred<void>();
    const release = vi.fn();
    host.render.mockImplementationOnce(async (...args) => { await pending.promise; args[4].register(release); });
    const owner = new Component();
    const rendering = obsidianMarkdown({} as App, owner).render("Late source", {} as HTMLElement);
    await Promise.resolve();
    rendering.dispose();
    pending.resolve();
    await rendering.ready;
    expect(release).toHaveBeenCalledTimes(1);
  });
  it("does not start the host renderer after immediate cancellation", async () => {
    const owner = new Component();
    const rendering = obsidianMarkdown({} as App, owner).render("Cancelled source", {} as HTMLElement);
    rendering.dispose();
    await rendering.ready;
    expect(host.render).not.toHaveBeenCalled();
  });
});
