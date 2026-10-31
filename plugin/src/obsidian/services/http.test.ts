import { describe, expect, it, vi } from "vitest";

const { requestUrl } = vi.hoisted(() => ({ requestUrl: vi.fn() }));
vi.mock("obsidian", () => ({ requestUrl }));

import { obsidianHttp } from "./http";

describe("buffered Obsidian HTTP adapter", () => {
  it("passes serialized bytes and headers once and preserves non-2xx host responses", async () => {
    const response = { status: 503, text: "problem", headers: { etag: '"3"' }, json: { code: "projection_pending" } };
    requestUrl.mockResolvedValue(response);
    const request = { url: "https://backend.example/api/v1/workspace-settings", method: "PATCH", contentType: "application/json", body: '{"enabled":true}', headers: { "If-Match": '"2"' } };
    await expect(obsidianHttp(request)).resolves.toBe(response);
    expect(requestUrl).toHaveBeenCalledExactlyOnceWith({ ...request, throw: false });
  });
});
