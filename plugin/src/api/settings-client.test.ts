import { describe, expect, it, vi } from "vitest";
import { TopicStreamClient } from "./client";
import { makeJob, makeSource, makeTopic } from "../test/storybook/fixtures";

describe("versioned settings API", () => {
  it("cancels through the public Job acknowledgement and retries through its successor reference", async () => {
    const job = makeJob({ cancellationRequested: true });
    const transport = vi.fn().mockResolvedValueOnce({ status: 202, json: job }).mockResolvedValue({ status: 202, json: { jobId: "successor" } });
    const client = new TopicStreamClient("https://backend.example", transport);
    await expect(client.cancelJob("job/id")).resolves.toEqual(job);
    await expect(client.retryJob("job/id")).resolves.toEqual({ jobId: "successor" });
    expect(transport.mock.calls).toEqual([
      [{ url: "https://backend.example/api/v1/jobs/job%2Fid/cancel", method: "POST" }],
      [{ url: "https://backend.example/api/v1/jobs/job%2Fid/retry", method: "POST" }]
    ]);
  });
  it("reads public model options and retrieval state", async () => {
    const transport = vi.fn().mockResolvedValueOnce({ status: 200, json: { items: [] } }).mockResolvedValue({ status: 200, json: { desiredModelId: "model", servingModelId: null, transitionJobId: null } });
    const client = new TopicStreamClient("https://backend.example", transport);
    await expect(client.listEmbeddingModelOptions()).resolves.toEqual([]);
    await client.getRetrievalStatus();
    expect(transport.mock.calls).toEqual([
      [{ url: "https://backend.example/api/v1/embedding-model-options", method: "GET" }],
      [{ url: "https://backend.example/api/v1/retrieval-status", method: "GET" }]
    ]);
  });
  it("reads non-secret Workspace settings and patches with the captured version", async () => {
    const transport = vi.fn().mockResolvedValue({ status: 200, json: {} });
    const client = new TopicStreamClient("https://backend.example", transport);
    await client.getWorkspaceSettings();
    await client.updateWorkspaceSettings({ summary: { language: "es", maxInputChars: 48000, maxSummaryCharacters: 800 } }, 7);
    expect(transport.mock.calls).toEqual([
      [{ url: "https://backend.example/api/v1/workspace-settings", method: "GET" }],
      [{ url: "https://backend.example/api/v1/workspace-settings", method: "PATCH", headers: { "If-Match": '"7"' }, contentType: "application/json", body: JSON.stringify({ summary: { language: "es", maxInputChars: 48000, maxSummaryCharacters: 800 } }) }]
    ]);
  });
  it("uses Digest version preconditions and a stable explicit Build request identity", async () => {
    const transport = vi.fn().mockResolvedValue({ status: 200, json: { items: [] } });
    const client = new TopicStreamClient("https://backend.example", transport);
    await expect(client.listDigests()).resolves.toEqual([]);
    await client.updateDigest("digest/id", { enabled: false }, 4);
    transport.mockResolvedValueOnce({ status: 204 });
    await client.deleteDigest("digest/id", 5);
    transport.mockResolvedValueOnce({ status: 202, json: { jobId: makeJob().id } });
    await client.generateDigest("digest/id", "fixed-request");
    expect(transport.mock.calls).toEqual([
      [{ url: "https://backend.example/api/v1/digests", method: "GET" }],
      [{ url: "https://backend.example/api/v1/digests/digest%2Fid", method: "PATCH", headers: { "If-Match": '"4"' }, contentType: "application/json", body: '{"enabled":false}' }],
      [{ url: "https://backend.example/api/v1/digests/digest%2Fid", method: "DELETE", headers: { "If-Match": '"5"' } }],
      [{ url: "https://backend.example/api/v1/digests/digest%2Fid/generate", method: "POST", headers: { "Idempotency-Key": "fixed-request" } }]
    ]);
  });
  it("reads the profile and explicitly patches with the captured If-Match version", async () => {
    const profile = { prefer: "", avoid: "", version: 4, revision: 1, updatedAt: "2026-10-27T09:00:00Z" };
    const transport = vi.fn().mockResolvedValue({ status: 200, json: profile });
    const client = new TopicStreamClient("https://backend.example", transport);
    await expect(client.getPreferenceProfile()).resolves.toEqual(profile);
    await client.updatePreferenceProfile({ prefer: "Sistemas fiables", avoid: "" }, 3);
    expect(transport.mock.calls).toEqual([
      [{ url: "https://backend.example/api/v1/preference-profile", method: "GET" }],
      [{ url: "https://backend.example/api/v1/preference-profile", method: "PATCH", headers: { "If-Match": '"3"' }, contentType: "application/json", body: JSON.stringify({ prefer: "Sistemas fiables", avoid: "" }) }]
    ]);
  });
  it("uses separate Source Sync and public Job requests without a preflight read", async () => {
    const job = makeJob();
    const transport = vi.fn().mockResolvedValueOnce({ status: 202, json: { jobId: job.id } }).mockResolvedValue({ status: 200, json: job });
    const client = new TopicStreamClient("https://backend.example", transport);
    await expect(client.syncSource("source/id")).resolves.toEqual({ jobId: job.id });
    await expect(client.getJob(job.id)).resolves.toEqual(job);
    expect(transport.mock.calls).toEqual([
      [{ url: "https://backend.example/api/v1/sources/source%2Fid/sync", method: "POST" }],
      [{ url: `https://backend.example/api/v1/jobs/${job.id}`, method: "GET" }]
    ]);
  });
  it("patches Source fields and accepts a bodyless 204 deletion response", async () => {
    const source = makeSource();
    const transport = vi.fn().mockResolvedValueOnce({ status: 200, json: source }).mockResolvedValue({ status: 204, get json() { throw new Error("No response body"); } });
    const client = new TopicStreamClient("https://backend.example", transport);
    await expect(client.updateSource(source.id, { enabled: false })).resolves.toEqual(source);
    await expect(client.deleteSource(source.id)).resolves.toBeUndefined();
    expect(transport.mock.calls).toEqual([
      [{ url: `https://backend.example/api/v1/sources/${source.id}`, method: "PATCH", contentType: "application/json", body: JSON.stringify({ enabled: false }) }],
      [{ url: `https://backend.example/api/v1/sources/${source.id}`, method: "DELETE" }]
    ]);
  });
  it("updates and deletes a Topic through its public resource without inventing a version header", async () => {
    const topic = makeTopic();
    const transport = vi.fn().mockResolvedValueOnce({ status: 200, json: topic }).mockResolvedValue({ status: 204 });
    const client = new TopicStreamClient("https://backend.example", transport);
    await expect(client.updateTopic(topic.id, { include: "Sistemas fiables" })).resolves.toEqual(topic);
    await expect(client.deleteTopic(topic.id)).resolves.toBeUndefined();
    expect(transport.mock.calls).toEqual([
      [{ url: `https://backend.example/api/v1/topics/${topic.id}`, method: "PATCH", contentType: "application/json", body: JSON.stringify({ include: "Sistemas fiables" }) }],
      [{ url: `https://backend.example/api/v1/topics/${topic.id}`, method: "DELETE" }]
    ]);
  });
});
