import { describe, expect, it, vi } from "vitest";
import { DigestBuilds } from "./digest-builds";
import { normalizeConnectionState } from "./device-state";
import { makeJob } from "../../test/storybook/fixtures";

function setup() {
  let saved = normalizeConnectionState(null);
  const storage = {
    read: () => Promise.resolve(structuredClone(saved)),
    update: (change: (value: typeof saved) => void) => { change(saved); return Promise.resolve(); }
  };
  const job = makeJob({ status: "succeeded" });
  const service = { generateDigest: vi.fn(async (_id: string, _requestId: string) => ({ jobId: job.id })), getJob: vi.fn(async () => job) };
  return { storage, service, getSaved: () => saved, reopen: () => { saved = structuredClone(saved); return new DigestBuilds(service, storage, () => "second-request"); } };
}
describe("safe manual Digest Builds", () => {
  it("restores an unresolved request without submitting and offers its same identity on deliberate retry", async () => {
    const { service, storage } = setup();
    await storage.update((value) => { value.unresolvedBuilds.digest = "persisted-request"; });
    const builds = new DigestBuilds(service, storage, () => "new-request");
    await builds.restore("digest");
    expect(builds.getSnapshot().digest).toMatchObject({ status: "uncertain" });
    expect(service.generateDigest).not.toHaveBeenCalled();
    await builds.build("digest");
    expect(service.generateDigest).toHaveBeenCalledExactlyOnceWith("digest", "persisted-request");
  });
  it("persists the request before sending, coalesces double clicks and reads a terminal acknowledgement", async () => {
    const { service, storage, getSaved } = setup();
    service.generateDigest.mockImplementation(async () => {
      expect(getSaved().unresolvedBuilds.digest).toBe("first-request");
      return { jobId: makeJob().id };
    });
    const builds = new DigestBuilds(service, storage, () => "first-request");
    await Promise.all([builds.build("digest"), builds.build("digest")]);
    expect(service.generateDigest).toHaveBeenCalledExactlyOnceWith("digest", "first-request");
    expect(builds.getSnapshot().digest).toMatchObject({ status: "succeeded", jobId: makeJob().id });
    expect(getSaved().unresolvedBuilds).toEqual({});
  });
  it("reuses the stored identity after an uncertain response and reopening", async () => {
    const { service, storage, reopen } = setup();
    service.generateDigest.mockRejectedValueOnce(new Error("Response lost"));
    const builds = new DigestBuilds(service, storage, () => "first-request");
    await builds.build("digest");
    expect(builds.getSnapshot().digest).toMatchObject({ status: "uncertain" });
    builds.dispose();
    const next = reopen();
    await next.build("digest");
    expect(service.generateDigest.mock.calls).toEqual([["digest", "first-request"], ["digest", "first-request"]]);
  });
  it("does not submit a replacement while an accepted Job is active or after a status read failed", async () => {
    const { service, storage } = setup();
    service.getJob.mockRejectedValueOnce(new Error("Read failed"));
    const builds = new DigestBuilds(service, storage, () => "first-request");
    await builds.build("digest");
    await builds.build("digest");
    expect(service.generateDigest).toHaveBeenCalledTimes(1);
    expect(builds.getSnapshot().digest).toMatchObject({ status: "succeeded" });
  });
  it("contains a failed local completion write during a shared background Job observation", async () => {
    const { service, storage } = setup();
    service.getJob.mockResolvedValue(makeJob({ status: "running" }));
    const builds = new DigestBuilds(service, storage, () => "first-request");
    await builds.build("digest");
    vi.spyOn(storage, "update").mockRejectedValueOnce(new Error("Device storage unavailable"));
    await expect(builds.observe(makeJob({ status: "succeeded" }))).resolves.toBeUndefined();
    expect(builds.getSnapshot().digest).toMatchObject({ status: "unavailable", jobId: makeJob().id });
    expect(service.generateDigest).toHaveBeenCalledTimes(1);
  });
});
