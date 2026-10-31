import { describe, expect, it, vi } from "vitest";
import { TopicStreamClient } from "./client";
import type { HttpTransport } from "./transport";
import { fixtureId } from "../test/storybook/fixtures";

describe("archive Search API", () => {
  it("reads archive and hybrid filtered pages without publication or cursor interpretation", async () => {
    const page = { items: [], nextCursor: "an/opaque+cursor" };
    const transport = vi.fn<HttpTransport>().mockResolvedValue({ status: 200, json: page });
    const client = new TopicStreamClient("https://backend.example", transport);
    await expect(client.searchItems({ limit: 20 })).resolves.toEqual(page);
    await client.searchItems({ limit: 20, q: "notas & systems", mode: "hybrid", cursor: "an/opaque+cursor", topicId: fixtureId(200), sourceId: fixtureId(100), publishedAfter: "2026-01-01T00:00:00Z", publishedBefore: "2026-10-28T23:59:59Z" });
    const first = transport.mock.calls[0]![0];
    expect(first).toEqual({ url: "https://backend.example/api/v1/search?limit=20", method: "GET" });
    const second = transport.mock.calls[1]![0];
    const url = new URL(second.url);
    expect(second.method).toBe("GET");
    expect(url.pathname).toBe("/api/v1/search");
    expect(Object.fromEntries(url.searchParams)).toEqual({ limit: "20", q: "notas & systems", mode: "hybrid", cursor: "an/opaque+cursor", topicId: fixtureId(200), sourceId: fixtureId(100), publishedAfter: "2026-01-01T00:00:00Z", publishedBefore: "2026-10-28T23:59:59Z" });
    expect(transport).toHaveBeenCalledTimes(2);
  });
});
