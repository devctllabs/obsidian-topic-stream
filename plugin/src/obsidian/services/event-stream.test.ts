import { describe, expect, it, vi } from "vitest";
import { browserEventStream } from "./event-stream";

function response() {
  let writer!: ReadableStreamDefaultController<Uint8Array>;
  const cancel = vi.fn();
  const body = new ReadableStream<Uint8Array>({ start: (controller) => { writer = controller; }, cancel });
  return { writer, cancel, body, response: new Response(body, { status: 200, headers: { "Content-Type": "text/event-stream; charset=utf-8" } }) };
}
describe("mobile-safe browser streaming boundary", () => {
  it("delivers chunks before EOF, keeps REST separate and releases the reader", async () => {
    const source = response(); const fetch = vi.fn(async () => source.response); const receive = vi.fn();
    const signal = new AbortController().signal;
    const settled = vi.fn();
    const pending = browserEventStream(() => ({ fetch }))({ url: "https://backend.example/api/v1/events", signal, receive }).then(settled);
    source.writer.enqueue(new TextEncoder().encode(": connected\n\n"));
    await vi.waitFor(() => expect(receive).toHaveBeenCalledTimes(1));
    expect(settled).not.toHaveBeenCalled();
    source.writer.enqueue(new TextEncoder().encode(": heartbeat\n\n")); source.writer.close();
    await pending;
    expect(receive).toHaveBeenCalledTimes(2); expect(source.body.locked).toBe(false);
    expect(fetch).toHaveBeenCalledExactlyOnceWith("https://backend.example/api/v1/events", { method: "GET", headers: { Accept: "text/event-stream" }, credentials: "omit", cache: "no-store", signal });
  });
  it("does not dispatch an already-cancelled request and cancels a pending read on close", async () => {
    const source = response(); const fetch = vi.fn(async () => source.response);
    const controller = new AbortController(); const receive = vi.fn();
    const open = browserEventStream(() => ({ fetch }));
    controller.abort();
    await expect(open({ url: "https://backend.example/api/v1/events", signal: controller.signal, receive })).rejects.toMatchObject({ name: "AbortError" });
    expect(fetch).not.toHaveBeenCalled();
    const active = new AbortController();
    const pending = open({ url: "https://backend.example/api/v1/events", signal: active.signal, receive });
    const outcome = expect(pending).rejects.toMatchObject({ name: "AbortError" });
    await vi.waitFor(() => expect(source.body.locked).toBe(true)); active.abort(); await outcome;
    expect(source.cancel).toHaveBeenCalledTimes(1); expect(source.body.locked).toBe(false);
    expect(receive).not.toHaveBeenCalled();
  });
  it.each([{ status: 503, media: "text/event-stream" }, { status: 200, media: "application/json" }])("rejects invalid responses without retaining a body: %j", async ({ status, media }) => {
    const source = response();
    const fetch = vi.fn(async () => new Response(source.body, { status, headers: { "Content-Type": media } }));
    await expect(browserEventStream(() => ({ fetch }))({ url: "https://backend.example/api/v1/events", signal: new AbortController().signal, receive: vi.fn() })).rejects.toThrow();
    expect(source.cancel).toHaveBeenCalledTimes(1);
  });
  it("uses the current owning realm after migration", async () => {
    const first = { fetch: vi.fn(async () => new Response(": connected\n\n", { headers: { "Content-Type": "text/event-stream" } })) };
    const second = { fetch: vi.fn(async () => new Response(": connected\n\n", { headers: { "Content-Type": "text/event-stream" } })) }; let realm = first;
    const open = browserEventStream(() => realm);
    const request = { url: "https://backend.example/api/v1/events", signal: new AbortController().signal, receive: vi.fn() };
    await open(request); realm = second; await open(request);
    expect(first.fetch).toHaveBeenCalledTimes(1);
    expect(second.fetch).toHaveBeenCalledTimes(1);
  });
});
