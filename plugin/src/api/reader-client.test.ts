import { describe, expect, it, vi } from "vitest";
import { TopicStreamClient } from "./client";
import type { HttpTransport } from "./transport";
import { fixtureId } from "../test/storybook/fixtures";

describe("Reader image API", () => {
  it("downloads only an Item image ID and preserves bytes without touching JSON or text getters", async () => {
    const bytes = new Uint8Array([137, 80, 78, 71]).buffer;
    const transport = vi.fn<HttpTransport>().mockResolvedValue({ status: 200, headers: { "Content-Type": "image/png; charset=binary" }, arrayBuffer: bytes,
      get json(): unknown { throw new Error("Binary success must not parse JSON."); }, get text(): string { throw new Error("Binary success must not decode text."); } });
    const client = new TopicStreamClient("https://backend.example", transport);
    await expect(client.downloadItemImage(fixtureId(300), fixtureId(800))).resolves.toEqual({ bytes, contentType: "image/png" });
    expect(transport).toHaveBeenCalledWith({ url: `https://backend.example/api/v1/items/${fixtureId(300)}/images/${fixtureId(800)}`, method: "GET" });
  });

  it.each([
    { name: "non-image", contentType: "text/html", bytes: new ArrayBuffer(4) },
    { name: "empty", contentType: "image/png", bytes: new ArrayBuffer(0) },
    { name: "oversized", contentType: "image/png", bytes: new ArrayBuffer(10 * 1024 * 1024 + 1) }
  ])("rejects a $name response without retry", async ({ contentType, bytes }) => {
    const transport = vi.fn<HttpTransport>().mockResolvedValue({ status: 200, headers: { "content-type": contentType }, arrayBuffer: bytes });
    const client = new TopicStreamClient("https://backend.example", transport);
    await expect(client.downloadItemImage(fixtureId(300), fixtureId(800))).rejects.toThrow("invalid image");
    expect(transport).toHaveBeenCalledTimes(1);
  });
});
