import { describe, expect, it } from "vitest";

import { normalizePreferences } from "./preferences";

describe("vault-scoped preferences", () => {
  const defaults = { version: 1, feedFilter: "all", snapshot: { content: "compact", images: "links" } };

  it.each([null, undefined, [], "invalid", { version: 999 }])("defaults malformed or unsupported data: %j", (input) => {
    expect(normalizePreferences(input)).toEqual(defaults);
  });

  it("migrates legacy data without synchronizing the device URL or unknown fields", () => {
    expect(normalizePreferences({ serverUrl: "http://desktop:7331", secret: "do not copy" })).toEqual(defaults);
  });

  it("keeps valid choices, repairs invalid fields and is idempotent", () => {
    const normalized = normalizePreferences({ version: 1, feedFilter: "unread", snapshot: { content: "full", images: "invalid" } });
    expect(normalized).toEqual({ ...defaults, feedFilter: "unread", snapshot: { content: "full", images: "links" } });
    expect(normalizePreferences(normalized)).toEqual(normalized);
  });
});
