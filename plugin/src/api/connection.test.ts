import { describe, expect, it, vi } from "vitest";

vi.mock("obsidian", () => ({ requestUrl: vi.fn() }));

import { TopicStreamClient } from "./client";

describe("connection-owned HTTP consumers", () => {
  it("abandons an old connection without delivering its late HTTP response", async () => {
    let complete!: (value: { status: number; text: string; json: unknown }) => void;
    const transport = vi.fn(() => new Promise<{ status: number; text: string; json: unknown }>((resolve) => { complete = resolve; }));
    const connection = new AbortController();
    const client = new TopicStreamClient("http://localhost:7331", transport, connection.signal);
    const request = client.listSources();
    const result = expect(request).rejects.toMatchObject({ name: "AbortError" });
    await vi.waitFor(() => expect(transport).toHaveBeenCalledTimes(1));
    connection.abort();
    await result;
    complete({ status: 200, text: '{"items":[]}', json: { items: [] } });
    expect(transport).toHaveBeenCalledTimes(1);
    await expect(client.listSources()).rejects.toMatchObject({ name: "AbortError" });
    expect(transport).toHaveBeenCalledTimes(1);
  });

  it("sends the current contract's typed Source config once", async () => {
    const input = { name: "Research", config: { kind: "rss" as const, url: "https://example.org/rss", allowPrivateNetwork: false }, enabled: true, pollIntervalMinutes: 60 };
    const transport = vi.fn().mockResolvedValue({ status: 201, json: { id: "source", ...input } });
    const client = new TopicStreamClient("http://localhost:7331", transport);
    await expect(client.createSource(input)).resolves.toMatchObject(input);
    expect(transport).toHaveBeenCalledWith(expect.objectContaining({ method: "POST", body: JSON.stringify(input) }));
    expect(transport).toHaveBeenCalledTimes(1);
  });
});
