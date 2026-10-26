import { beforeEach, describe, expect, it, vi } from "vitest";

const { requestUrl } = vi.hoisted(() => ({ requestUrl: vi.fn() }));
vi.mock("obsidian", () => ({ requestUrl }));

import { TopicStreamClient } from "./client";

describe("HTTP error boundary", () => {
  const client = new TopicStreamClient("http://localhost:7331");
  beforeEach(() => { requestUrl.mockReset(); });

  it("keeps stable codes and all field errors while showing the safe detail", async () => {
    const problem = {
      type: "urn:topic-stream:problem:invalid-source", code: "invalid_source", status: 400,
      title: "Invalid source", detail: "Check the source fields and try again.",
      errors: [{ field: "name", message: "This field is required." }, { field: "config.url", message: "Enter a valid URL." }]
    };
    requestUrl.mockResolvedValue({ status: 400, text: JSON.stringify(problem) });

    await expect(client.listSources()).rejects.toMatchObject({
      message: problem.detail, status: 400, code: "invalid_source", type: problem.type, issues: problem.errors
    });
    expect(requestUrl).toHaveBeenCalledWith(expect.objectContaining({ throw: false }));
  });

  it("retains the committed outcome of a pending projection", async () => {
    requestUrl.mockResolvedValue({ status: 503, text: JSON.stringify({
      type: "urn:topic-stream:problem:projection-pending", code: "projection_pending", status: 503,
      title: "Retrieval projection pending", detail: "Your change was saved. Index updates are pending.",
      canonicalCommitted: true, projectionRevision: "revision"
    }) });
    await expect(client.setFeedback("item", "like", null)).rejects.toMatchObject({
      code: "projection_pending", canonicalCommitted: true, projectionRevision: "revision"
    });
    expect(requestUrl).toHaveBeenCalledTimes(1);
  });

  it.each(["<html>private proxy error</html>", "{}", '{"title":"private","status":400}'])(
    "uses a safe fallback for an invalid problem payload", async (text) => {
      requestUrl.mockResolvedValue({ status: 500, text });
      await expect(client.listSources()).rejects.toMatchObject({ message: "Topic Stream could not complete the request (HTTP 500).", status: 500 });
    }
  );

  it("uses the title when detail is absent and accepts future codes", async () => {
    requestUrl.mockResolvedValue({ status: 409, text: JSON.stringify({ type: "urn:topic-stream:problem:future", code: "future", title: "Refresh the resource", status: 409 }) });
    await expect(client.listSources()).rejects.toMatchObject({ message: "Refresh the resource", code: "future" });
  });

  it("normalizes transport exceptions without exposing their original text", async () => {
    requestUrl.mockRejectedValue(new Error("private connection details"));
    await expect(client.listSources()).rejects.toThrow("Could not connect to Topic Stream.");
  });

  it("preserves cancellation as control flow", async () => {
    const cancelled = Object.assign(new Error("cancelled"), { name: "AbortError" });
    requestUrl.mockRejectedValue(cancelled);
    await expect(client.listSources()).rejects.toBe(cancelled);
  });

  it("normalizes a malformed successful JSON response", async () => {
    requestUrl.mockResolvedValue({ status: 200, get json() { throw new Error("private decoder details"); } });
    await expect(client.listSources()).rejects.toThrow("Topic Stream returned an invalid response.");
  });

  it("keeps successful responses unchanged", async () => {
    requestUrl.mockResolvedValue({ status: 200, json: { items: [] } });
    await expect(client.listSources()).resolves.toEqual([]);
  });
});
