import { describe, expect, it } from "vitest";

import { buildFeedUrl, FEED_PAGE_SIZE } from "./url";

describe("buildFeedUrl", () => {
  it("omits snapshot and cursor for an explicit refresh", () => {
    expect(FEED_PAGE_SIZE).toBe(25);
    expect(buildFeedUrl("http://127.0.0.1:7331", { limit: FEED_PAGE_SIZE, cursor: null, snapshot: null }))
      .toBe("http://127.0.0.1:7331/api/v1/feed?limit=25");
  });

  it("keeps the snapshot while requesting older items", () => {
    expect(buildFeedUrl("http://127.0.0.1:7331", {
      limit: FEED_PAGE_SIZE,
      cursor: "older:item/30",
      snapshot: "feed:2026-09-27"
    })).toBe("http://127.0.0.1:7331/api/v1/feed?limit=25&cursor=older%3Aitem%2F30&snapshot=feed%3A2026-09-27");
  });
});
