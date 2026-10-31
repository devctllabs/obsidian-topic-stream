import type { EventStreamTransport } from "./event-stream";
import type { ViewEvent, ViewEvents } from "./events";
import { SSEDecoder } from "./sse-decoder";

/** One connection context shares advisory delivery and owns cancellation for all feature subscribers. */
export class AdvisoryConnection implements ViewEvents {
  private readonly listeners = new Set<(event: ViewEvent) => void>();
  private controller: AbortController | null = null;
  private disposed = false;
  private retry: ReturnType<typeof setTimeout> | undefined;
  private failures = 0;
  public constructor(private readonly serverUrl: string, private readonly transport: EventStreamTransport) {}
  public subscribe = (listener: (event: ViewEvent) => void): (() => void) => {
    if (this.disposed) return () => undefined;
    this.listeners.add(listener);
    if (!this.controller && this.retry === undefined) this.start();
    return () => { this.listeners.delete(listener); if (!this.listeners.size) this.stop(); };
  };
  public dispose(): void { this.disposed = true; this.stop(); this.listeners.clear(); }
  public reconnect(): void { this.stop(); if (!this.disposed && this.listeners.size) this.start(); }
  private stop(): void {
    this.controller?.abort(); this.controller = null;
    clearTimeout(this.retry); this.retry = undefined; this.failures = 0;
  }
  private current(controller: AbortController): boolean { return this.controller === controller && !controller.signal.aborted; }
  private start(): void {
    const controller = new AbortController(); this.controller = controller;
    let connectedAt: number | null = null;
    const enqueue = this.coalescer(controller);
    const decoder = new SSEDecoder(enqueue, () => { if (connectedAt === null) { connectedAt = Date.now(); enqueue({ type: "reconnected" }); } });
    const fail = () => this.failed(controller, connectedAt);
    try {
      void this.transport({ url: new URL("/api/v1/events", this.serverUrl).href, signal: controller.signal, receive: (bytes) => {
        if (!this.current(controller)) return;
        try { decoder.push(bytes); } catch { fail(); }
      } }).then(fail, fail);
    } catch { fail(); }
  }
  private coalescer(controller: AbortController): (event: ViewEvent) => void {
    const pending = new Map<string, ViewEvent>();
    let scheduled = false;
    return (event) => {
      pending.set(JSON.stringify(event), event);
      if (scheduled) return;
      scheduled = true;
      queueMicrotask(() => {
        scheduled = false;
        for (const value of pending.values()) this.emit(value, controller);
        pending.clear();
      });
    };
  }
  private failed(controller: AbortController, connectedAt: number | null): void {
    if (!this.current(controller)) return;
    this.controller = null; controller.abort();
    if (this.disposed || !this.listeners.size) return;
    if (connectedAt !== null && Date.now() - connectedAt >= 30_000) this.failures = 0;
    const base = Math.min(30_000, 1000 * 2 ** this.failures);
    const delay = Math.min(30_000, base * (0.8 + Math.random() * 0.4));
    this.failures = Math.min(5, this.failures + 1);
    this.retry = setTimeout(() => { this.retry = undefined; if (!this.disposed && this.listeners.size) this.start(); }, delay);
  }
  private emit(event: ViewEvent, controller: AbortController): void {
    for (const listener of [...this.listeners]) {
      if (!this.current(controller)) return;
      listener(event);
    }
  }
}
