import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AdvisoryConnection } from "./advisory-connection";
import type { EventStreamRequest, EventStreamTransport } from "./event-stream";
import { deferred } from "../test/storybook/deferred";
import { fixtureId } from "../test/storybook/fixtures";
import type { ViewEvent } from "./events";

const bytes = (value: string) => new TextEncoder().encode(value);
const job = `event: jobChanged\ndata: {"jobId":"${fixtureId(400)}","args":{"secret":"must not escape"}}\n\n`;
const owned: AdvisoryConnection[] = [];
function backend() {
  const requests: (EventStreamRequest & { finish: () => void; fail: (error: Error) => void })[] = [];
  const open: EventStreamTransport = (request) => {
    const wait = deferred<void>();
    requests.push({ ...request, finish: () => wait.resolve(), fail: (error) => wait.reject(error) });
    const abort = () => wait.reject(new DOMException("Closed", "AbortError"));
    request.signal.addEventListener("abort", abort, { once: true });
    return wait.promise.finally(() => request.signal.removeEventListener("abort", abort));
  };
  const connection = new AdvisoryConnection("http://backend.example", open); owned.push(connection);
  const receive = vi.fn<(event: ViewEvent) => void>(); const unsubscribe = connection.subscribe(receive);
  return { requests, connection, receive, unsubscribe };
}

describe("view-owned advisory delivery", () => {
  beforeEach(() => { vi.useFakeTimers(); vi.spyOn(Math, "random").mockReturnValue(0.5); });
  afterEach(() => { owned.splice(0).forEach((connection) => connection.dispose()); vi.useRealTimers(); vi.restoreAllMocks(); });

  it("parses split UTF-8, CR/LF, multiline data and typed IDs while ignoring comments and unknown events", async () => {
    const source = backend();
    const text = `\uFEFF: connected\r\n\r\n: heartbeat\r\n\r\nevent: future\ndata: Español, not JSON\n\n${job}event: feedInvalidated\rdata: {"itemId":\rdata: "${fixtureId(300)}"}\r\revent: digestReady\r\ndata: {"issueId":"${fixtureId(500)}"}\r\n\r\n`;
    const encoded = bytes(text);
    for (let index = 0; index < encoded.length; index += 3) source.requests[0]!.receive(encoded.slice(index, index + 3));
    await vi.advanceTimersByTimeAsync(0);
    expect(source.receive.mock.calls.map(([event]) => event)).toEqual([{ type: "reconnected" }, { type: "jobChanged", jobId: fixtureId(400) }, { type: "feedInvalidated", itemId: fixtureId(300) }, { type: "digestReady", issueId: fixtureId(500) }]);
  });
  it("coalesces same-turn invalidations without inventing readiness on reconnect", async () => {
    const source = backend();
    source.requests[0]!.receive(bytes(": connected\n\n: connected\n\n" + job.repeat(20)));
    await vi.advanceTimersByTimeAsync(0);
    expect(source.receive.mock.calls.map(([event]) => event)).toEqual([{ type: "reconnected" }, { type: "jobChanged", jobId: fixtureId(400) }]);
    source.requests[0]!.finish(); await vi.advanceTimersByTimeAsync(1000);
    expect(source.requests).toHaveLength(2);
    source.requests[1]!.receive(bytes(": connected\n\n")); await vi.advanceTimersByTimeAsync(0);
    expect(source.receive).toHaveBeenLastCalledWith({ type: "reconnected" });
    expect(source.receive).toHaveBeenCalledTimes(3);
  });
  it.each(["event: jobChanged\ndata: not-json\n\n", 'event: jobChanged\ndata: {"jobId":"invalid"}\n\n', "data: " + "x".repeat(65_537)])("recovers from malformed/bounded input with an owned retry", async (text) => {
    const source = backend();
    expect(() => source.requests[0]!.receive(bytes(text))).not.toThrow();
    await vi.advanceTimersByTimeAsync(0);
    expect(source.requests[0]!.signal.aborted).toBe(true);
    expect(source.receive).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(999); expect(source.requests).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(1); expect(source.requests).toHaveLength(2);
  });
  it("backs off failed connections to 30 seconds and cancels all retries on disposal", async () => {
    const source = backend();
    for (const [index, delay] of [1000, 2000, 4000, 8000, 16000, 30000, 30000].entries()) {
      source.requests[index]!.fail(new Error("Disconnected")); await vi.advanceTimersByTimeAsync(0);
      await vi.advanceTimersByTimeAsync(delay - 1); expect(source.requests).toHaveLength(index + 1);
      await vi.advanceTimersByTimeAsync(1); expect(source.requests).toHaveLength(index + 2);
    }
    source.requests.at(-1)!.fail(new Error("Disconnected")); await vi.advanceTimersByTimeAsync(0);
    source.connection.dispose(); await vi.advanceTimersByTimeAsync(120_000);
    expect(source.requests).toHaveLength(8);
  });
  it("keeps quiet and heartbeat-only streams open, resets backoff after stability and discards queued late delivery", async () => {
    const source = backend();
    source.requests[0]!.receive(bytes(": connected\n\n")); await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(60_000);
    source.requests[0]!.receive(bytes(": heartbeat\n\n")); await vi.advanceTimersByTimeAsync(0);
    expect(source.requests).toHaveLength(1); expect(source.receive).toHaveBeenCalledTimes(1);
    source.requests[0]!.fail(new Error("Disconnected")); await vi.advanceTimersByTimeAsync(1000);
    expect(source.requests).toHaveLength(2);
    source.requests[1]!.receive(bytes(job)); source.unsubscribe();
    await vi.advanceTimersByTimeAsync(120_000);
    expect(source.receive).toHaveBeenCalledTimes(1);
    expect(source.requests[1]!.signal.aborted).toBe(true);
    source.connection.subscribe(source.receive); expect(source.requests).toHaveLength(3);
  });
});
