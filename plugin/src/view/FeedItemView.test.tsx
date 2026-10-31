import type { WorkspaceLeaf } from "obsidian";
import type { ReactElement } from "react";
import { describe, expect, it, vi } from "vitest";

const host = vi.hoisted(() => ({ empty: vi.fn(), render: vi.fn(), unmount: vi.fn(), migrationDisposed: vi.fn(), migrate: (): void => undefined, signals: [] as AbortSignal[] }));
vi.mock("obsidian", () => ({
  ItemView: class { public contentEl = { empty: host.empty, onWindowMigrated: (listener: () => void) => { host.migrate = listener; return host.migrationDisposed; } }; public app = { vault: {}, fileManager: {} }; },
  requestUrl: vi.fn()
}));
vi.mock("../obsidian/services/event-stream", () => ({ browserEventStream: () => (request: { signal: AbortSignal }) => new Promise<void>((_resolve, reject) => {
  host.signals.push(request.signal);
  request.signal.addEventListener("abort", () => reject(new DOMException("Closed", "AbortError")), { once: true });
}) }));
vi.mock("react-dom/client", () => ({ createRoot: () => ({ render: host.render, unmount: host.unmount }) }));

import type { ViewConnection } from "../features/connection/session";
import { SettingsStore } from "../features/settings/persistence";
import { FeedItemView } from "./FeedItemView";

describe("Obsidian view lifecycle boundary", () => {
  it("updates an open view on Apply and releases subscriptions/root on close exactly once", async () => {
    const settings = await SettingsStore.load({
      loadPreferences: async () => null, savePreferences: async () => undefined,
      loadDevice: () => null, saveDevice: () => undefined
    });
    const view = new FeedItemView({} as WorkspaceLeaf, settings);
    await view.onOpen();
    const element = host.render.mock.calls.at(-1)?.[0] as ReactElement<{ connection: ViewConnection }>;
    const connection = element.props.connection;
    connection.getSnapshot().events!.subscribe(() => undefined);
    expect(host.signals).toHaveLength(1);
    host.migrate();
    expect(host.signals).toHaveLength(2);
    expect(host.signals[0]!.aborted).toBe(true);
    settings.applyServerUrl("https://other.example");
    expect(connection.getSnapshot().serverUrl).toBe("https://other.example");
    expect(host.render).toHaveBeenCalledTimes(1);
    connection.getSnapshot().navigation.open("search");
    connection.getSnapshot().navigation.select("search", "saved-selection");
    connection.getSnapshot().navigation.rememberPosition("search:list", 230);
    await view.onClose();
    view.dispose();
    settings.applyServerUrl("https://after-close.example");
    expect(connection.getSnapshot().serverUrl).toBe("https://other.example");
    expect(host.unmount).toHaveBeenCalledTimes(1);
    expect(host.migrationDisposed).toHaveBeenCalledTimes(1);
    expect(host.signals.every((signal) => signal.aborted)).toBe(true);
    expect(settings.readConnection("https://other.example", null).navigation).toMatchObject({
      screen: "search", selections: { search: "saved-selection" }, positions: { "search:list": 230 }
    });
    settings.applyServerUrl("https://other.example");
    await view.onOpen();
    const reopened = host.render.mock.calls.at(-1)?.[0] as ReactElement<{ connection: ViewConnection }>;
    expect(reopened.props.connection.getSnapshot().navigation.getSnapshot()).toMatchObject({ screen: "search", selections: { search: "saved-selection" } });
    expect(reopened.props.connection.getSnapshot().navigation.position("search:list")).toBe(230);
    await view.onClose();
    settings.dispose();
  });
});
