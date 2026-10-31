import { describe, expect, it, vi } from "vitest";
import { TopicStreamClient } from "./client";
import { FIXTURE_TIME, fixtureId } from "../test/storybook/fixtures";

describe("Feed publication API", () => {
  it("keeps scoped publication separate from opaque snapshot reads", async () => {
    const page = { items: [], nextCursor: null, snapshot: "opaque-snapshot", generatedAt: FIXTURE_TIME };
    const transport = vi.fn().mockResolvedValue({ status: 200, json: page });
    const client = new TopicStreamClient("https://backend.example", transport);
    await expect(client.refreshFeed({ limit: 25 })).resolves.toEqual(page);
    await client.refreshFeed({ limit: 25, topicId: fixtureId(201) });
    await client.getFeed({ limit: 25, topicId: fixtureId(201), cursor: "older/cursor", snapshot: "opaque-snapshot" });
    expect(transport.mock.calls).toEqual([
      [{ url: "https://backend.example/api/v1/feed/refresh", method: "POST", contentType: "application/json", body: '{"limit":25}' }],
      [{ url: "https://backend.example/api/v1/feed/refresh", method: "POST", contentType: "application/json", body: JSON.stringify({ limit: 25, topicId: fixtureId(201) }) }],
      [{ url: `https://backend.example/api/v1/feed?limit=25&topicId=${fixtureId(201)}&cursor=older%2Fcursor&snapshot=opaque-snapshot`, method: "GET" }]
    ]);
  });
  it("does not retry an uncertain publication response", async () => {
    const transport = vi.fn().mockRejectedValue(new Error("Connection lost after send"));
    const client = new TopicStreamClient("https://backend.example", transport);
    await expect(client.refreshFeed({ limit: 25 })).rejects.toThrow("Could not connect");
    expect(transport).toHaveBeenCalledTimes(1);
  });
  it("writes reading, global visibility and scoped membership through their own operations", async () => {
    const transport = vi.fn().mockResolvedValue({ status: 204, text: "" });
    const client = new TopicStreamClient("https://backend.example", transport);
    await client.setReadingState(fixtureId(300), "read");
    await client.setVisibility(fixtureId(300), "hidden");
    await client.setTopicCorrection(fixtureId(300), { topicId: fixtureId(201), membership: "exclude" });
    expect(transport.mock.calls).toEqual([
      [{ url: `https://backend.example/api/v1/items/${fixtureId(300)}/reading-state`, method: "PUT", contentType: "application/json", body: '{"state":"read"}' }],
      [{ url: `https://backend.example/api/v1/items/${fixtureId(300)}/visibility`, method: "PUT", contentType: "application/json", body: '{"visibility":"hidden"}' }],
      [{ url: `https://backend.example/api/v1/items/${fixtureId(300)}/topic-corrections`, method: "PUT", contentType: "application/json", body: JSON.stringify({ topicId: fixtureId(201), membership: "exclude" }) }]
    ]);
  });
});
