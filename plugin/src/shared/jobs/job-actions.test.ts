import { describe, expect, it, vi } from "vitest";
import { JobActions } from "./job-actions";
import { makeJob, fixtureId } from "../../test/storybook/fixtures";
import { deferred } from "../../test/storybook/deferred";
describe("shared public Job actions", () => {
  it("coalesces concurrent cancellation and keeps the public cancellation acknowledgement", async () => {
    const job = makeJob();
    const response = deferred<typeof job>();
    const service = { cancelJob: vi.fn(() => response.promise), retryJob: vi.fn() };
    const actions = new JobActions(service);
    const first = actions.cancel(job);
    const second = actions.cancel(job);
    response.resolve({ ...job, cancellationRequested: true });
    await expect(first).resolves.toMatchObject({ cancellationRequested: true });
    await second;
    expect(service.cancelJob).toHaveBeenCalledExactlyOnceWith(job.id);
  });
  it("admits Retry only for the verified current transition and retains its durable successor identity", async () => {
    const transition = { modelId: fixtureId(600), selectionGeneration: 5, jobId: fixtureId(400), status: "dead" as const, isCurrent: true, canRetry: true };
    const job = makeJob({ status: "dead", modelTransition: transition });
    const service = { cancelJob: vi.fn(), retryJob: vi.fn(async () => ({ jobId: fixtureId(401) })) };
    const actions = new JobActions(service);
    await expect(actions.retry({ ...job, modelTransition: { ...transition, isCurrent: false } })).rejects.toThrow();
    await expect(actions.retry({ ...job, modelTransition: { ...transition, canRetry: false } })).rejects.toThrow();
    const results = await Promise.all([actions.retry(job), actions.retry(job)]);
    expect(results).toEqual([{ jobId: fixtureId(401) }, { jobId: fixtureId(401) }]);
    expect(service.retryJob).toHaveBeenCalledExactlyOnceWith(job.id);
  });
  it("rejects historical reindex Retry without a public model binding while allowing ordinary failed work", async () => {
    const service = { cancelJob: vi.fn(), retryJob: vi.fn(async () => ({ jobId: fixtureId(401) })) };
    const actions = new JobActions(service);
    await expect(actions.retry(makeJob({ status: "dead" }))).rejects.toThrow();
    expect(service.retryJob).not.toHaveBeenCalled();
    await expect(actions.retry(makeJob({ kind: "generate-digest", status: "dead" }))).resolves.toEqual({ jobId: fixtureId(401) });
  });
  it("recognizes the actual public reindex-workspace Job kind when no verified binding exists", async () => {
    const service = { cancelJob: vi.fn(), retryJob: vi.fn(async () => ({ jobId: fixtureId(401) })) };
    await expect(new JobActions(service).retry(makeJob({ kind: "reindex-workspace", status: "dead" }))).rejects.toThrow();
    expect(service.retryJob).not.toHaveBeenCalled();
  });
  it("sends no cancellation for an acknowledged request or after disposal", async () => {
    const service = { cancelJob: vi.fn(), retryJob: vi.fn() };
    const actions = new JobActions(service);
    const job = makeJob({ cancellationRequested: true });
    await expect(actions.cancel(job)).resolves.toEqual(job);
    actions.dispose();
    await expect(actions.retry(makeJob({ status: "dead" }))).rejects.toThrow();
    expect(service.cancelJob).not.toHaveBeenCalled();
    expect(service.retryJob).not.toHaveBeenCalled();
  });
});
