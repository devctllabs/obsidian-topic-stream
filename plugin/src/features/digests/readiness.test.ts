import { QueryClient } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DigestReadiness } from "./readiness";
import { FixtureScenario } from "../../test/storybook/scenario";
import { memoryWorkspaceState } from "../../test/storybook/local-state";
import { fixtureId, makeIssue } from "../../test/storybook/fixtures";
import { deferred } from "../../test/storybook/deferred";
import type { Schema } from "../../api/contracts";
import { SettingsStore } from "../settings/persistence";
import { WorkspaceState } from "../connection/workspace-state";

function setup() {
  const client = new FixtureScenario();
  const local = memoryWorkspaceState();
  const notify = vi.fn();
  const create = () => new DigestReadiness(client, local, new QueryClient(), notify);
  return { client, local, notify, create };
}
const newer = (n: number) => ({ ...makeIssue([]), id: fixtureId(550 + n), generatedAt: new Date(Date.parse("2026-10-28T09:00:00Z") + n * 1000).toISOString() });

describe("device-local Digest readiness", () => {
  afterEach(() => { vi.useRealTimers(); });
  it("still treats a live event during the first-ever head as existing history", async () => {
    vi.useFakeTimers();
    const { client, create, notify } = setup();
    const head = deferred<Schema<"DigestIssueList">>();
    vi.spyOn(client, "listDigestIssues").mockImplementationOnce(() => head.promise);
    const getIssue = vi.spyOn(client, "getDigestIssue");
    const model = create(); const close = model.connect(client.events);
    const reading = model.refresh(); const issue = newer(1); client.issues = [issue];
    client.events.emit({ type: "digestReady", issueId: issue.id });
    head.resolve({ items: [issue], nextCursor: null }); await reading;
    await vi.advanceTimersByTimeAsync(0);
    expect(getIssue).toHaveBeenCalledExactlyOnceWith(issue.id);
    expect(model.getSnapshot().unseen).toEqual([]);
    expect(notify).not.toHaveBeenCalled(); close();
  });
  it("keeps a live readiness Notice when another view is still reconciling its initial head", async () => {
    const { client, create, notify } = setup();
    const first = create(); const closeFirst = first.connect(client.events); await first.refresh();
    const head = deferred<Schema<"DigestIssueList">>();
    const live = deferred<Schema<"DigestIssue">>();
    vi.spyOn(client, "listDigestIssues").mockImplementationOnce(() => head.promise);
    vi.spyOn(client, "getDigestIssue").mockImplementationOnce(() => live.promise);
    const second = create(); const closeSecond = second.connect(client.events);
    const reading = second.refresh();
    const issue = newer(3); client.issues.unshift(issue);
    client.events.emit({ type: "digestReady", issueId: issue.id });
    await vi.waitFor(() => expect(client.getDigestIssue).toHaveBeenCalledWith(issue.id));
    head.resolve({ items: [issue], nextCursor: null }); await reading;
    live.resolve(issue);
    await vi.waitFor(() => expect(notify).toHaveBeenCalledExactlyOnceWith(`Digest ready: ${issue.title}`));
    expect(first.getSnapshot().unseen).toEqual([issue.id]);
    expect(second.getSnapshot().unseen).toEqual([issue.id]);
    closeFirst(); closeSecond();
  });
  it("baselines the first successful unfiltered head once and reconstructs all later pages without catch-up Notices", async () => {
    const { client, local, create, notify } = setup();
    client.listDigestIssues = vi.fn(client.listDigestIssues);
    client.getDigestIssue = vi.fn(client.getDigestIssue);
    client.issues = Array.from({ length: 45 }, (_, n) => ({ ...makeIssue([]), id: fixtureId(1000 + n) }));
    const first = create(); const close = first.connect(client.events); await first.refresh();
    const baseline = (await local.read()).digest.baseline;
    expect(first.getSnapshot().unseen).toEqual([]);
    expect(client.listDigestIssues).toHaveBeenCalledTimes(1);
    close();
    const offline = Array.from({ length: 41 }, (_, n) => newer(n));
    client.issues.unshift(...offline);
    const reopened = create(); const stop = reopened.connect(client.events); await reopened.refresh();
    expect(new Set(reopened.getSnapshot().unseen)).toEqual(new Set(offline.map((issue) => issue.id)));
    expect((await local.read()).digest.baseline).toEqual(baseline);
    expect(notify).not.toHaveBeenCalled();
    client.events.emit({ type: "digestReady", issueId: offline[0]!.id });
    await vi.waitFor(() => expect(client.getDigestIssue).toHaveBeenCalledWith(offline[0]!.id));
    await reopened.refresh();
    expect(notify).not.toHaveBeenCalled();
    await reopened.open(offline[0]!.id); await reopened.refresh();
    expect(reopened.getSnapshot().unseen).toHaveLength(40);
    stop();
  });
  it("shares one live Notice and synchronizes opened badges across concurrent views on the same device", async () => {
    const { client, create, notify } = setup();
    const first = create(); const second = create();
    const closeFirst = first.connect(client.events); const closeSecond = second.connect(client.events);
    await Promise.all([first.refresh(), second.refresh()]);
    const issue = newer(1); client.issues.unshift(issue);
    client.events.emit({ type: "digestReady", issueId: issue.id });
    client.events.emit({ type: "digestReady", issueId: issue.id });
    await vi.waitFor(() => {
      expect(first.getSnapshot().unseen).toEqual([issue.id]);
      expect(second.getSnapshot().unseen).toEqual([issue.id]);
    });
    expect(notify).toHaveBeenCalledTimes(1);
    await first.open(issue.id);
    await vi.waitFor(() => expect(second.getSnapshot().unseen).toEqual([]));
    closeFirst(); closeSecond();
  });
  it("keeps failed initialization retryable and treats its first later success as history", async () => {
    const { client, local, create, notify } = setup();
    const original = client.listDigestIssues;
    client.listDigestIssues = vi.fn(original).mockRejectedValueOnce(new Error("Head unavailable"));
    const model = create(); const close = model.connect(client.events); await model.refresh();
    expect((await local.read()).digest.baseline).toBeNull();
    expect(model.getSnapshot().error?.message).toBe("Head unavailable");
    client.issues.unshift(newer(0)); await model.refresh();
    expect((await local.read()).digest.baseline?.issueId).toBe(newer(0).id);
    expect(model.getSnapshot()).toMatchObject({ unseen: [], error: null });
    expect(notify).not.toHaveBeenCalled(); close();
  });
  it("initializes an empty archive and ignores old or opened advisory duplicates after reconnect", async () => {
    const { client, create, notify } = setup(); client.issues = [];
    const model = create(); const close = model.connect(client.events); await model.refresh();
    const issue = newer(0); client.issues.push(issue);
    client.events.emit({ type: "digestReady", issueId: issue.id });
    await vi.waitFor(() => expect(model.getSnapshot().unseen).toEqual([issue.id]));
    expect(notify).toHaveBeenCalledTimes(1);
    await model.open(issue.id); client.events.emit({ type: "reconnected" }); await model.refresh();
    client.events.emit({ type: "digestReady", issueId: issue.id }); await model.refresh();
    expect(model.getSnapshot().unseen).toEqual([]); expect(notify).toHaveBeenCalledTimes(1); close();
  });
  it("disposes event and storage listeners and discards an in-flight initial head", async () => {
    const { client, local, create, notify } = setup();
    const pending = deferred<Schema<"DigestIssueList">>();
    client.listDigestIssues = vi.fn(() => pending.promise);
    client.getDigestIssue = vi.fn(client.getDigestIssue);
    const model = create(); const close = model.connect(client.events); const reading = model.refresh();
    await vi.waitFor(() => expect(client.listDigestIssues).toHaveBeenCalledTimes(1)); close();
    pending.resolve({ items: [newer(0)], nextCursor: null }); await reading;
    client.events.emit({ type: "digestReady", issueId: newer(0).id });
    expect((await local.read()).digest.baseline).toBeNull();
    expect(client.getDigestIssue).not.toHaveBeenCalled(); expect(notify).not.toHaveBeenCalled();
  });
  it("keeps an unseen badge and reports failed local acknowledgement without an unhandled rejection", async () => {
    const { client, local, create } = setup();
    const model = create(); const close = model.connect(client.events); await model.refresh();
    const issue = newer(1); client.issues.unshift(issue); client.events.emit({ type: "digestReady", issueId: issue.id });
    await vi.waitFor(() => expect(model.getSnapshot().unseen).toEqual([issue.id]));
    vi.spyOn(local, "update").mockRejectedValueOnce(new Error("Device storage unavailable"));
    await expect(model.open(issue.id)).resolves.toBeUndefined();
    expect(model.getSnapshot().unseen).toEqual([issue.id]);
    expect(model.getSnapshot().error?.message).toBe("Device storage unavailable"); close();
  });
  it("isolates baseline, seen and Notice records by device, backend and public Workspace identity", async () => {
    const loadSettings = () => SettingsStore.load({ loadPreferences: async () => null, savePreferences: async () => undefined, loadDevice: () => null, saveDevice: () => undefined });
    const firstDevice = await loadSettings(); const secondDevice = await loadSettings();
    const configs = [
      { store: firstDevice, endpoint: "https://first.example", workspace: fixtureId(1) },
      { store: firstDevice, endpoint: "https://second.example", workspace: fixtureId(1) },
      { store: firstDevice, endpoint: "https://first.example", workspace: fixtureId(2) },
      { store: secondDevice, endpoint: "https://first.example", workspace: fixtureId(1) }
    ];
    const views = configs.map(({ store, endpoint, workspace }) => {
      const client = new FixtureScenario(); client.systemStatus.workspaceId = workspace;
      const storage = new WorkspaceState(store, endpoint, client);
      const notify = vi.fn(); const model = new DigestReadiness(client, storage, new QueryClient(), notify);
      return { client, storage, model, notify, close: model.connect(client.events) };
    });
    await Promise.all(views.map(({ model }) => model.refresh()));
    const issue = newer(1); for (const view of views) view.client.issues.unshift(issue);
    views[0]!.client.events.emit({ type: "digestReady", issueId: issue.id });
    await vi.waitFor(() => expect(views[0]!.notify).toHaveBeenCalledTimes(1));
    await Promise.all(views.slice(1).map(({ model }) => model.refresh()));
    await views[0]!.model.open(issue.id);
    for (const view of views.slice(1)) {
      expect(view.model.getSnapshot().unseen).toEqual([issue.id]);
      expect((await view.storage.read()).digest.seenIssueIds).toEqual([]);
      expect(view.notify).not.toHaveBeenCalled();
    }
    for (const view of views) view.close();
  });
});
