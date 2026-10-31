import { describe, expect, it, vi } from "vitest";
import { TransitionNotifications } from "./transition-notifications";
import { memoryWorkspaceState } from "../../test/storybook/local-state";
import { makeRetrievalStatus, makeJob, fixtureId } from "../../test/storybook/fixtures";
import { deferred } from "../../test/storybook/deferred";
function setup() {
  const storage = memoryWorkspaceState();
  const status = makeRetrievalStatus();
  const service = { getRetrievalStatus: vi.fn(async () => status) };
  const notify = vi.fn();
  const watch = new TransitionNotifications(service, storage, notify);
  return { storage, status, service, notify, watch };
}
describe("current model transition notices", () => {
  it("uses the verified terminal recovery reference after the full Job has been cleaned up", async () => {
    const { status, notify, watch } = setup();
    await watch.watch(fixtureId(601));
    await watch.observe({ ...status, servingModelId: fixtureId(601), latestTransition: { ...status.latestTransition!, status: "succeeded" } }, undefined);
    expect(notify).toHaveBeenCalledExactlyOnceWith("Embedding model is ready.");
  });
  it("retains a deliberate selection across a failed status read and reopening until public linkage is confirmed", async () => {
    const { storage, status, service, notify, watch } = setup();
    service.getRetrievalStatus.mockRejectedValueOnce(new Error("Response unavailable"));
    await expect(watch.watch(fixtureId(601))).rejects.toThrow("Response unavailable");
    watch.dispose();
    const reopened = new TransitionNotifications(service, storage, notify);
    await reopened.observe({ ...status, servingModelId: fixtureId(601), latestTransition: { ...status.latestTransition!, status: "succeeded" } }, makeJob({ status: "succeeded" }));
    expect(notify).toHaveBeenCalledExactlyOnceWith("Embedding model is ready.");
  });
  it("notifies once only after Job success and confirmed activation, including a reopened observer", async () => {
    const { storage, status, service, notify, watch } = setup();
    await watch.watch(fixtureId(601));
    const complete = { ...status, latestTransition: { ...status.latestTransition!, status: "succeeded" as const } };
    await watch.observe(complete, makeJob({ status: "succeeded" }));
    expect(notify).not.toHaveBeenCalled();
    const activated = { ...complete, servingModelId: fixtureId(601) };
    await Promise.all([watch.observe(activated, makeJob({ status: "succeeded" })), watch.observe(activated, makeJob({ status: "succeeded" }))]);
    expect(notify).toHaveBeenCalledExactlyOnceWith("Embedding model is ready.");
    watch.dispose();
    await new TransitionNotifications(service, storage, notify).observe(activated, makeJob({ status: "succeeded" }));
    expect(notify).toHaveBeenCalledTimes(1);
  });
  it("suppresses automatic retry, cancellation and superseded work; final failure is reported once", async () => {
    const { status, notify, watch } = setup();
    await watch.watch(fixtureId(601));
    await watch.observe({ ...status, latestTransition: { ...status.latestTransition!, status: "retry_wait" } }, makeJob({ status: "retry_wait" }));
    await watch.observe({ ...status, latestTransition: { ...status.latestTransition!, status: "cancelled" } }, makeJob({ status: "cancelled" }));
    await watch.observe({ ...status, latestTransition: { ...status.latestTransition!, status: "dead", isCurrent: false } }, makeJob({ status: "dead" }));
    expect(notify).not.toHaveBeenCalled();
    const failed = { ...status, latestTransition: { ...status.latestTransition!, status: "dead" as const } };
    await watch.observe(failed, makeJob({ status: "dead" }));
    await watch.observe(failed, makeJob({ status: "dead" }));
    expect(notify).toHaveBeenCalledExactlyOnceWith("Embedding model preparation failed. Open Tasks for details.");
  });
  it("keeps the last deliberate A→B→A selection when a superseded status response arrives late", async () => {
    const { storage, status, service, watch } = setup();
    const earlier = deferred<typeof status>();
    service.getRetrievalStatus.mockReturnValueOnce(earlier.promise);
    const old = watch.watch(fixtureId(601));
    await vi.waitFor(() => expect(service.getRetrievalStatus).toHaveBeenCalledTimes(1));
    const latest = { ...status, desiredModelId: fixtureId(600), latestTransition: { ...status.latestTransition!, modelId: fixtureId(600), selectionGeneration: 3 } };
    service.getRetrievalStatus.mockResolvedValueOnce(latest);
    await watch.watch(fixtureId(600));
    earlier.resolve(status); await old;
    expect((await storage.read()).watchedTransitions.current).toMatchObject({ modelId: fixtureId(600), selectionGeneration: 3 });
  });
});
