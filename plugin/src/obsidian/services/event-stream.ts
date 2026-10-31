import type { EventStreamTransport } from "../../shared/event-stream";

/** Fetch belongs to the ItemView's current window, including pop-out realms. */
export function browserEventStream(realm: () => Pick<Window, "fetch">): EventStreamTransport {
  return async ({ url, signal, receive }) => {
    signal.throwIfAborted();
    const response = await realm().fetch(url, { method: "GET", headers: { Accept: "text/event-stream" }, credentials: "omit", cache: "no-store", signal });
    const media = response.headers.get("Content-Type")?.split(";")[0]?.trim().toLowerCase();
    if (response.status !== 200 || media !== "text/event-stream" || !response.body) {
      await response.body?.cancel().catch(() => undefined);
      throw new Error("Advisory streaming is unavailable.");
    }
    const reader = response.body.getReader();
    const abort = () => { void reader.cancel().catch(() => undefined); };
    signal.addEventListener("abort", abort, { once: true });
    try {
      signal.throwIfAborted();
      while (!signal.aborted) {
        const next = await reader.read();
        signal.throwIfAborted();
        if (next.done) return;
        receive(next.value);
      }
      signal.throwIfAborted();
    } finally {
      signal.removeEventListener("abort", abort);
      await reader.cancel().catch(() => undefined);
      reader.releaseLock();
    }
  };
}
