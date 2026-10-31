import { describe, expect, it, vi } from "vitest";
import { makeJob, makeSource } from "../../test/storybook/fixtures";
import { deferred } from "../../test/storybook/deferred";
import type { JobAccepted } from "../../api/contracts";
import { SourceActions, type SourceWorkflowService } from "./source-actions";

describe("Source creation and session-local Sync", () => {
  it("keeps the created Source after failed initial Sync and retries only Sync", async () => {
    const source = makeSource();
    const service: SourceWorkflowService = {
      createSource: vi.fn(async () => source),
      syncSource: vi.fn(async () => { throw new Error("Sync unavailable"); }),
      getJob: vi.fn()
    };
    const actions = new SourceActions(service);
    const result = await actions.create({ name: source.name, config: source.config, enabled: source.enabled, pollIntervalMinutes: source.pollIntervalMinutes });
    expect(result).toEqual(source);
    await vi.waitFor(() => expect(actions.getSnapshot()[source.id]).toMatchObject({ status: "failed", jobId: null }));
    await actions.sync(source.id);
    expect(service.createSource).toHaveBeenCalledTimes(1);
    expect(service.syncSource).toHaveBeenCalledTimes(2);
    expect(service.getJob).not.toHaveBeenCalled();
  });

  it("tracks the accepted Job without duplicate Sync and forgets it after the session closes", async () => {
    const source = makeSource();
    const accepted = deferred<JobAccepted>();
    const service: SourceWorkflowService = {
      createSource: vi.fn(async () => source), syncSource: vi.fn(() => accepted.promise),
      getJob: vi.fn(async () => makeJob({ id: "accepted-job", kind: "sync_source", status: "running" }))
    };
    const actions = new SourceActions(service);
    await actions.create({ name: source.name, config: source.config, enabled: source.enabled, pollIntervalMinutes: source.pollIntervalMinutes });
    await vi.waitFor(() => expect(service.syncSource).toHaveBeenCalledTimes(1));
    const first = actions.sync(source.id);
    const second = actions.sync(source.id);
    expect(first).toBe(second);
    accepted.resolve({ jobId: "accepted-job" });
    await first;
    expect(service.getJob).toHaveBeenCalledWith("accepted-job");
    expect(actions.getSnapshot()[source.id]).toMatchObject({ status: "running", jobId: "accepted-job" });
    expect(service.syncSource).toHaveBeenCalledTimes(1);
    actions.dispose();
    const reopened = new SourceActions(service);
    expect(reopened.getSnapshot()).toEqual({});
    expect(source.lastSyncedAt).toBeDefined();
    reopened.dispose();
  });
  it("retains the accepted Job when status cannot be read and refreshes it without another Sync", async () => {
    const service: SourceWorkflowService = {
      createSource: vi.fn(), syncSource: vi.fn(async () => ({ jobId: "accepted-job" })),
      getJob: vi.fn().mockRejectedValueOnce(new Error("Read unavailable")).mockResolvedValue(makeJob({ id: "accepted-job", status: "succeeded" }))
    };
    const actions = new SourceActions(service);
    await actions.sync("source");
    expect(actions.getSnapshot().source).toMatchObject({ status: "unavailable", jobId: "accepted-job" });
    await actions.refresh("source");
    expect(actions.getSnapshot().source).toMatchObject({ status: "succeeded", jobId: "accepted-job" });
    expect(service.syncSource).toHaveBeenCalledTimes(1);
  });
  it("starts no Sync after failed creation or after the connected session closes", async () => {
    const service: SourceWorkflowService = {
      createSource: vi.fn(async () => { throw new Error("Creation failed"); }),
      syncSource: vi.fn(async () => ({ jobId: "job" })), getJob: vi.fn(async () => makeJob())
    };
    const actions = new SourceActions(service);
    const source = makeSource();
    await expect(actions.create({ name: source.name, config: source.config, enabled: true, pollIntervalMinutes: 60 })).rejects.toThrow("Creation failed");
    expect(actions.getSnapshot()).toEqual({});
    const pending = actions.sync("source");
    actions.dispose();
    await pending;
    expect(service.syncSource).not.toHaveBeenCalled();
    expect(service.getJob).not.toHaveBeenCalled();
  });
});
