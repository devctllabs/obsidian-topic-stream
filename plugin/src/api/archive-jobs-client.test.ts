import { describe, expect, it, vi } from "vitest";
import { TopicStreamClient } from "./client";
import type { HttpTransport } from "./transport";
import { fixtureId } from "../test/storybook/fixtures";

describe("Digest archive and Task Center API", () => {
  it("reads opaque Job pages through the public status filter without undefined query values", async () => {
    const transport = vi.fn<HttpTransport>().mockResolvedValue({ status: 200, json: { items: [], nextCursor: null } });
    const client = new TopicStreamClient("https://backend.example/", transport);
    await expect(client.listJobs({ status: "retry_wait", limit: 30, cursor: "opaque+/=next" })).resolves.toEqual({ items: [], nextCursor: null });
    expect(transport).toHaveBeenCalledWith({ method: "GET", url: "https://backend.example/api/v1/jobs?status=retry_wait&limit=30&cursor=opaque%2B%2F%3Dnext" });
  });
  it("reads saved Issues scoped to a Definition with an opaque continuation", async () => {
    const transport = vi.fn<HttpTransport>().mockResolvedValue({ status: 200, json: { items: [], nextCursor: null } });
    const client = new TopicStreamClient("https://backend.example", transport);
    await client.listDigestIssues({ digestId: fixtureId(900), limit: 20, cursor: "a+/=" });
    expect(transport).toHaveBeenCalledWith({ method: "GET", url: `https://backend.example/api/v1/digest-issues?digestId=${fixtureId(900)}&limit=20&cursor=a%2B%2F%3D` });
  });
});
