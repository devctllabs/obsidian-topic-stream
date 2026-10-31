import { describe, expect, it, vi } from "vitest";

import { ViewConnection } from "./session";
import type { EventStreamRequest, EventStreamTransport } from "../../shared/event-stream";
import type { ViewEvent } from "../../shared/events";
import { fixtureId } from "../../test/storybook/fixtures";

function heldStream() {
  const requests: EventStreamRequest[] = [];
  const open: EventStreamTransport = (request) => new Promise((_resolve, reject) => {
    requests.push(request);
    request.signal.addEventListener("abort", () => reject(new DOMException("Closed", "AbortError")), { once: true });
  });
  return { requests, open };
}

describe("view connection ownership", () => {
  it("reopens streaming in the new window without replacing navigation or authoritative queries", async () => {
    const stream = heldStream();
    const view = new ViewConnection("http://desktop:7331", vi.fn(), stream.open);
    const context = view.getSnapshot();
    context.navigation.open("digests");
    context.navigation.rememberPosition("digests:list", 230);
    context.queryClient.setQueryData(["saved-reading"], "retained");
    const receive = vi.fn(); context.events!.subscribe(receive);
    view.reconnectEvents();
    expect(stream.requests).toHaveLength(2);
    expect(stream.requests[0]!.signal.aborted).toBe(true);
    expect(view.getSnapshot()).toBe(context);
    expect(context.navigation.position("digests:list")).toBe(230);
    expect(context.queryClient.getQueryData(["saved-reading"])).toBe("retained");
    stream.requests[0]!.receive(new TextEncoder().encode(": connected\n\n"));
    stream.requests[1]!.receive(new TextEncoder().encode(": connected\n\n"));
    await vi.waitFor(() => expect(receive).toHaveBeenCalledExactlyOnceWith({ type: "reconnected" }));
    view.dispose(); view.reconnectEvents();
    expect(stream.requests).toHaveLength(2);
    expect(stream.requests[1]!.signal.aborted).toBe(true);
  });
  it("shares one advisory stream per context and suppresses old endpoint callbacks", async () => {
    const stream = heldStream();
    const view = new ViewConnection("http://desktop:7331", vi.fn(), stream.open);
    const old = view.getSnapshot();
    expect(old.events).toBeDefined();
    const first = vi.fn<(event: ViewEvent) => void>(); const second = vi.fn<(event: ViewEvent) => void>();
    const stopFirst = old.events!.subscribe(first); const stopSecond = old.events!.subscribe(second);
    expect(stream.requests).toHaveLength(1);
    const request = stream.requests[0]!;
    expect(request.url).toBe("http://desktop:7331/api/v1/events");
    const event = new TextEncoder().encode(`: connected\n\nevent: jobChanged\ndata: {"jobId":"${fixtureId(400)}"}\n\n`);
    request.receive(event);
    await vi.waitFor(() => expect(first.mock.calls.map(([value]) => value)).toEqual([{ type: "reconnected" }, { type: "jobChanged", jobId: fixtureId(400) }]));
    expect(second.mock.calls).toEqual(first.mock.calls);
    stopFirst(); expect(request.signal.aborted).toBe(false);
    view.replaceServerUrl("https://phone.example");
    expect(request.signal.aborted).toBe(true);
    first.mockClear(); second.mockClear(); request.receive(event);
    expect(first).not.toHaveBeenCalled(); expect(second).not.toHaveBeenCalled();
    const current = view.getSnapshot(); const latest = vi.fn(); current.events!.subscribe(latest);
    expect(stream.requests).toHaveLength(2);
    expect(stream.requests[1]!.url).toBe("https://phone.example/api/v1/events");
    stopSecond(); view.dispose();
    expect(stream.requests[1]!.signal.aborted).toBe(true);
    stream.requests[1]!.receive(event);
    expect(latest).not.toHaveBeenCalled();
  });
  it("replaces the endpoint and cache without delivering old in-flight work", async () => {
    let finish!: (value: { status: number; json: unknown }) => void;
    const transport = vi.fn(() => new Promise<{ status: number; json: unknown }>((resolve) => { finish = resolve; }));
    const view = new ViewConnection("http://desktop:7331", transport);
    const old = view.getSnapshot();
    old.queryClient.setQueryData(["selected-item"], "old-private-item");
    old.navigation.open("digests");
    old.navigation.select("digests", "old-private-issue");
    const pending = old.client.listSources();
    const outcome = expect(pending).rejects.toMatchObject({ name: "AbortError" });
    await vi.waitFor(() => expect(transport).toHaveBeenCalledTimes(1));
    const changed = vi.fn();
    view.subscribe(changed);
    view.replaceServerUrl("https://phone.example/");
    await outcome;
    finish({ status: 200, json: { items: [{ id: "old-source" }] } });
    expect(changed).toHaveBeenCalledTimes(1);
    expect(view.getSnapshot().serverUrl).toBe("https://phone.example");
    expect(view.getSnapshot().queryClient.getQueryData(["selected-item"])).toBeUndefined();
    expect(view.getSnapshot().navigation.getSnapshot()).toEqual({ primary: "feed", screen: "feed", selections: {} });
    expect(old.queryClient.getQueryCache().getAll()).toEqual([]);
    expect(view.getSnapshot().id).not.toBe(old.id);
    view.dispose();
  });

  it("keeps equivalent URLs stable and disposes once with no later callbacks", () => {
    const view = new ViewConnection("http://EXAMPLE.ORG:80/", vi.fn());
    const changed = vi.fn();
    const unsubscribe = view.subscribe(changed);
    const current = view.getSnapshot();
    view.replaceServerUrl("http://example.org");
    expect(view.getSnapshot()).toBe(current);
    expect(changed).not.toHaveBeenCalled();
    unsubscribe();
    view.replaceServerUrl("http://other:7331");
    view.dispose();
    view.dispose();
    expect(changed).not.toHaveBeenCalled();
    view.replaceServerUrl("http://late:7331");
    expect(view.getSnapshot().serverUrl).toBe("http://other:7331");
  });
});
