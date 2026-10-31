import { describe, expect, it, vi } from "vitest";

import { SettingsStore } from "./persistence";

function storage() {
  return {
    loadPreferences: vi.fn().mockResolvedValue({ serverUrl: "http://desktop:7331" }),
    savePreferences: vi.fn().mockResolvedValue(undefined),
    loadDevice: vi.fn().mockReturnValue(null),
    saveDevice: vi.fn()
  };
}

describe("settings storage boundary", () => {
  it("moves a legacy URL to device storage and saves only normalized vault preferences", async () => {
    const port = storage();
    const settings = await SettingsStore.load(port);
    expect(settings.serverUrl).toBe("http://desktop:7331");
    expect(port.saveDevice).toHaveBeenCalledWith({ version: 1, serverUrl: "http://desktop:7331", connections: {} });
    expect(port.savePreferences).toHaveBeenCalledWith({ version: 1, feedFilter: "all", snapshot: { content: "compact", images: "links" } });
  });

  it("applies only a validated persisted URL, retaining the old connection on failure", async () => {
    const port = storage();
    const settings = await SettingsStore.load(port);
    expect(() => settings.applyServerUrl("file:///private/path")).toThrow("HTTP or HTTPS");
    expect(settings.serverUrl).toBe("http://desktop:7331");
    port.saveDevice.mockImplementation(() => { throw new Error("storage full"); });
    expect(() => settings.applyServerUrl("https://phone.example/")).toThrow("storage full");
    expect(settings.serverUrl).toBe("http://desktop:7331");
  });

  it("serializes preference updates and keeps the last saved state on failure", async () => {
    const port = storage();
    const settings = await SettingsStore.load(port);
    const writes = [
      settings.savePreferences((current) => ({ ...current, feedFilter: "unread" })),
      settings.savePreferences((current) => ({ ...current, snapshot: { ...current.snapshot, content: "full" } }))
    ];
    await Promise.all(writes);
    expect(settings.preferences).toMatchObject({ feedFilter: "unread", snapshot: { content: "full" } });
    port.savePreferences.mockRejectedValueOnce(new Error("save failed"));
    await expect(settings.savePreferences((current) => ({ ...current, feedFilter: "all" }))).rejects.toThrow("save failed");
    expect(settings.preferences.feedFilter).toBe("unread");
    await settings.savePreferences((current) => ({ ...current, feedFilter: "all" }));
    expect(settings.preferences.feedFilter).toBe("all");
  });

  it("restores device records across reopening and isolates endpoint/Workspace/resource identities", async () => {
    const port = storage();
    const settings = await SettingsStore.load(port);
    const watched = settings.readConnection(settings.serverUrl, "workspace");
    watched.unresolvedBuilds.digest = "request-id";
    watched.digest.seenIssueIds = ["issue"];
    watched.watchedTransitions.operation = { modelId: "model", selectionGeneration: 2, jobId: "job", notified: true };
    settings.saveConnection(settings.serverUrl, "workspace", watched);
    const device = port.saveDevice.mock.calls.at(-1)?.[0] as unknown;
    port.loadDevice.mockReturnValue(device);
    const reopened = await SettingsStore.load(port);
    expect(reopened.readConnection("HTTP://DESKTOP:7331/", "workspace")).toEqual(watched);
    expect(reopened.readConnection("http://other:7331", "workspace").unresolvedBuilds).toEqual({});
    expect(reopened.readConnection(settings.serverUrl, "another-workspace").digest.seenIssueIds).toEqual([]);
    expect(JSON.stringify(port.savePreferences.mock.calls)).not.toContain("request-id");
  });
});
