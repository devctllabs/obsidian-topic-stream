import { describe, expect, it } from "vitest";

import { connectionScope, DEFAULT_SERVER_URL, normalizeConnectionState, normalizeDeviceState } from "./device-state";

describe("device-local records", () => {
  it("retains only a valid pending selection boundary without inventing a transition identity", () => {
    expect(normalizeConnectionState({ pendingTransition: { modelId: "model", afterGeneration: 2, jobArgs: "drop" } }).pendingTransition)
      .toEqual({ modelId: "model", afterGeneration: 2 });
    expect(normalizeConnectionState({ pendingTransition: { modelId: "model", afterGeneration: -1 } }).pendingTransition).toBeUndefined();
  });
  it.each([undefined, null, [], "invalid", { version: 999, serverUrl: "https://unknown.example" }])("repairs unsupported local records: %j", (input) => {
    expect(normalizeDeviceState(input)).toEqual({ version: 1, serverUrl: DEFAULT_SERVER_URL, connections: {} });
  });

  it("migrates the old synchronized URL only when no device record exists", () => {
    expect(normalizeDeviceState(null, { serverUrl: " HTTP://DESKTOP:7331/ " }).serverUrl).toBe("http://desktop:7331");
    expect(normalizeDeviceState({ version: 1, serverUrl: "http://phone:7331", connections: {} }, { serverUrl: "http://desktop:7331" }).serverUrl)
      .toBe("http://phone:7331");
  });

  it("isolates equivalent normalized endpoints and distinct Workspace identities", () => {
    expect(connectionScope("HTTP://EXAMPLE.ORG:80/", "workspace")).toBe(connectionScope("http://example.org", "workspace"));
    expect(connectionScope("https://example.org", "workspace")).not.toBe(connectionScope("http://example.org", "workspace"));
    expect(connectionScope("http://example.org", "other")).not.toBe(connectionScope("http://example.org", "workspace"));
  });

  it("keeps resource identities while dropping malformed fields and unknown data", () => {
    const normalized = normalizeConnectionState({
      digest: { baseline: { generatedAt: "2026-10-27T09:00:00Z", issueId: "issue" }, seenIssueIds: ["a", "a", 4], notifiedIssueIds: null },
      unresolvedBuilds: { digest: "request-uuid", bad: 4 },
      watchedTransitions: {
        current: { modelId: "model", selectionGeneration: 3, jobId: "job", notified: false, args: { secret: "drop" } },
        malformed: { modelId: "model", selectionGeneration: -1 }
      },
      secret: "drop"
    });
    expect(normalized).toEqual({
      digest: { baseline: { generatedAt: "2026-10-27T09:00:00Z", issueId: "issue" }, seenIssueIds: ["a"], notifiedIssueIds: [] },
      unresolvedBuilds: { digest: "request-uuid" },
      watchedTransitions: { current: { modelId: "model", selectionGeneration: 3, jobId: "job", notified: false } }
    });
    expect(normalizeConnectionState(normalized)).toEqual(normalized);
  });

  it("drops credentials and unsupported connection data", () => {
    expect(normalizeDeviceState({ version: 1, serverUrl: "https://user:secret@example.org", connections: [], token: "secret" }))
      .toEqual({ version: 1, serverUrl: DEFAULT_SERVER_URL, connections: {} });
  });
});
