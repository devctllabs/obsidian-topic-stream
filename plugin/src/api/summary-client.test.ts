import { describe, expect, it, vi } from "vitest";
import { TopicStreamClient } from "./client";
import type { HttpTransport } from "./transport";
import { fixtureId, makeSummary } from "../test/storybook/fixtures";

describe("Item summary API", () => {
  it("reads an exact text version and sends only explicit canonical or captured-Digest requests", async () => {
    const transport = vi.fn<HttpTransport>().mockResolvedValueOnce({ status: 200, json: makeSummary() }).mockResolvedValue({ status: 202, json: { jobId: fixtureId(400) } });
    const client = new TopicStreamClient("https://backend.example", transport);
    await client.getItemSummary(fixtureId(300), "version /+?");
    await client.requestItemSummary(fixtureId(300));
    await client.requestDigestItemSummary(fixtureId(500), fixtureId(300));
    expect(transport.mock.calls.map(([request]) => request)).toEqual([
      { url: `https://backend.example/api/v1/items/${fixtureId(300)}/ai-summary?sourceVersion=version+%2F%2B%3F`, method: "GET" },
      { url: `https://backend.example/api/v1/items/${fixtureId(300)}/ai-summary`, method: "POST", contentType: "application/json", body: "{}" },
      { url: `https://backend.example/api/v1/digest-issues/${fixtureId(500)}/items/${fixtureId(300)}/ai-summary`, method: "POST", contentType: "application/json", body: "{}" }
    ]);
  });
});
