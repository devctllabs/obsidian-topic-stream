import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import { ItemSummaries } from "./item-summaries";
import { JobActions } from "../../shared/jobs/job-actions";
import { fixtureId, makeItem, makeIssue, makeEntry, makeJob, makeSummary, makeWorkspaceSettings } from "../../test/storybook/fixtures";
import { FixtureEvents } from "../../test/storybook/fake-services";
import { deferred } from "../../test/storybook/deferred";
import type { Schema } from "../../api/contracts";

function setup(state = makeSummary()) {
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  const client = {
    getItemSummary: vi.fn(async (_id: string, _version: string) => state),
    requestItemSummary: vi.fn(async (_id: string) => ({ jobId: fixtureId(401) })),
    getJob: vi.fn(async (id: string) => makeJob({ id, kind: "summarize-item", queue: "generation", status: "dead" })),
    retryJob: vi.fn(async (_id: string) => ({ jobId: fixtureId(402) })), cancelJob: vi.fn(async () => makeJob()),
    getDigestIssue: vi.fn(async (_id: string) => makeIssue([makeEntry()])),
    requestDigestItemSummary: vi.fn(async (_issueId: string, _itemId: string) => ({ jobId: fixtureId(403) }))
  };
  const model = new ItemSummaries(queries, client, new JobActions(client));
  return { queries, client, model, item: makeItem() };
}
describe("shared exact-version Item summaries", () => {
  it("treats a nullable saved annotation as absence, including authoritative reconnect reads", async () => {
    const { model, client } = setup();
    const issue = makeIssue([makeEntry()]);
    const input = { issueId: issue.id, captured: issue.sections[0]!.items[0]! };
    model.subscribe(input, () => undefined);
    expect(model.getResource(input)).toMatchObject({ data: null, status: "success", error: null });
    expect(client.getDigestIssue).not.toHaveBeenCalled();
    await model.refresh(input);
    expect(model.getResource(input)).toMatchObject({ data: null, status: "success", error: null });
    await expect(model.request(input)).rejects.toThrow("unavailable");
    expect(client.requestDigestItemSummary).not.toHaveBeenCalled();
  });
  it("reads status without generating and keeps existing ready text readable after generation is disabled", async () => {
    const { model, client, item } = setup(makeSummary({ state: "ready", canGenerate: false, unavailableReason: "generation_disabled" }));
    const unsubscribe = model.subscribe(item, () => undefined);
    await vi.waitFor(() => expect(model.getSnapshot(item)?.state).toBe("ready"));
    await model.request(item);
    expect(model.getSnapshot(item)?.result?.sourceVersion).toBe(item.summarySourceVersion);
    expect(client.requestItemSummary).not.toHaveBeenCalled();
    unsubscribe();
  });
  it("rejects an annotation from another source version instead of showing or generating it", async () => {
    const { model, client, item } = setup(makeSummary({ state: "ready", sourceVersion: "different-text" }));
    await expect(model.refresh(item)).rejects.toThrow("different source version");
    expect(model.getSnapshot(item)).toBeNull();
    await expect(model.request(item)).rejects.toThrow();
    expect(client.requestItemSummary).not.toHaveBeenCalled();
  });
  it("retries a failed summary through its eligible public Job once, without another generation POST", async () => {
    const { model, client, item } = setup(makeSummary({ state: "failed" }));
    await model.refresh(item);
    client.getItemSummary.mockResolvedValueOnce(makeSummary({ state: "failed" })).mockResolvedValue(makeSummary({ state: "pending", jobId: fixtureId(402) }));
    await Promise.all([model.request(item), model.request(item)]);
    expect(client.retryJob).toHaveBeenCalledExactlyOnceWith(fixtureId(401));
    expect(client.requestItemSummary).not.toHaveBeenCalled();
    expect(model.getSnapshot(item)?.jobId).toBe(fixtureId(402));
  });
  it("rechecks exact-version permission before starting new work", async () => {
    const { model, client, item } = setup();
    await model.refresh(item);
    client.getItemSummary.mockResolvedValue(makeSummary({ canGenerate: false, unavailableReason: "historical_input" }));
    await expect(model.request(item)).rejects.toThrow();
    expect(client.requestItemSummary).not.toHaveBeenCalled();
  });
  it("does not replay accepted work when its follow-up status read fails", async () => {
    const { model, client, item } = setup();
    await model.refresh(item);
    client.getItemSummary.mockResolvedValueOnce(makeSummary()).mockRejectedValue(new Error("Status unavailable."));
    await model.request(item);
    expect(model.getSnapshot(item)).toMatchObject({ state: "pending", jobId: fixtureId(401) });
    await model.request(item);
    expect(client.requestItemSummary).toHaveBeenCalledTimes(1);
  });
  it("discards late reads after the view is disposed", async () => {
    const { model, client, item } = setup();
    const pending = deferred<Schema<"ItemSummary">>();
    client.getItemSummary.mockImplementation(() => pending.promise);
    const close = model.connect();
    model.subscribe(item, () => undefined);
    close(); pending.resolve(makeSummary({ state: "ready" }));
    await pending.promise; await Promise.resolve(); await Promise.resolve();
    expect(model.getSnapshot(item)?.state).not.toBe("ready");
    expect(client.requestItemSummary).not.toHaveBeenCalled();
  });
  it("keeps canonical and captured annotations separate even for the same Item and never reads the capture through canonical state", async () => {
    const { model, client, item } = setup(makeSummary({ state: "ready" }));
    const captured = { issueId: fixtureId(500), captured: { item: { ...item, summarySourceVersion: "saved-text-v1" }, summary: "Saved edition excerpt", explanation: makeEntry().explanation, aiSummary: makeSummary({ sourceVersion: "saved-text-v1", state: "ready", canGenerate: false, unavailableReason: "historical_input" }) } };
    model.subscribe(captured, () => undefined);
    await model.refresh(item);
    expect(model.getSnapshot(captured)?.result?.sourceVersion).toBe("saved-text-v1");
    expect(model.getSnapshot(item)?.result?.sourceVersion).toBe(item.summarySourceVersion);
    await model.request(captured);
    expect(client.getItemSummary).toHaveBeenCalledExactlyOnceWith(item.id, item.summarySourceVersion);
    expect(client.getDigestIssue).not.toHaveBeenCalled();
    expect(client.requestItemSummary).not.toHaveBeenCalled();
  });
  it("starts and refreshes saved-Digest work through the saved Issue and preserves that exact input", async () => {
    const { model, client, item } = setup();
    const saved = { ...makeIssue([makeEntry()]).sections[0]!.items[0]!, item: { ...item, summarySourceVersion: "saved-text-v1" }, aiSummary: makeSummary({ sourceVersion: "saved-text-v1" }) };
    const input = { issueId: fixtureId(500), captured: saved };
    client.getDigestIssue.mockResolvedValue({ ...makeIssue([makeEntry()]), sections: [{ ...makeIssue([makeEntry()]).sections[0]!, items: [saved] }] });
    model.subscribe(input, () => undefined);
    await model.request(input);
    expect(client.requestDigestItemSummary).toHaveBeenCalledExactlyOnceWith(input.issueId, item.id);
    expect(client.getItemSummary).not.toHaveBeenCalled();
    expect(client.requestItemSummary).not.toHaveBeenCalled();
  });
  it("refreshes permission after Workspace settings change and reads advisories without generating", async () => {
    const { model, client, queries, item } = setup();
    const events = new FixtureEvents();
    const close = model.connect(events);
    model.subscribe(item, () => undefined);
    await vi.waitFor(() => expect(model.getSnapshot(item)).not.toBeNull());
    client.getItemSummary.mockResolvedValue(makeSummary({ canGenerate: false, unavailableReason: "generation_disabled" }));
    queries.setQueryData(["workspace-settings"], makeWorkspaceSettings());
    await vi.waitFor(() => expect(model.getSnapshot(item)?.canGenerate).toBe(false));
    events.emit({ type: "reconnected" });
    await vi.waitFor(() => expect(client.getItemSummary).toHaveBeenCalledTimes(3));
    expect(client.requestItemSummary).not.toHaveBeenCalled();
    close();
  });
});
