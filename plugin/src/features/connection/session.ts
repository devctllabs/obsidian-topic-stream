import { QueryClient } from "@tanstack/react-query";

import { TopicStreamClient } from "../../api/client";
import { isCancellation, type HttpTransport } from "../../api/transport";
import { normalizeServerUrl } from "../../api/url";
import { ShellNavigation } from "../shell/navigation";
import type { EventStreamTransport } from "../../shared/event-stream";
import type { ViewEvents } from "../../shared/events";
import { AdvisoryConnection } from "../../shared/advisory-connection";

export interface ConnectionSnapshot {
  id: number;
  serverUrl: string;
  client: TopicStreamClient;
  queryClient: QueryClient;
  navigation: ShellNavigation;
  events?: ViewEvents | undefined;
}

export class ViewConnection {
  private snapshot: ConnectionSnapshot;
  private controller = new AbortController();
  private readonly listeners = new Set<() => void>();
  private closed = false;
  private events: AdvisoryConnection | undefined;

  public constructor(serverUrl: string, private readonly transport: HttpTransport, private readonly eventStream?: EventStreamTransport) {
    this.snapshot = this.create(normalizeServerUrl(serverUrl), 0);
  }

  public getSnapshot = (): ConnectionSnapshot => this.snapshot;

  public reconnectEvents(): void {
    if (!this.closed) this.events?.reconnect();
  }

  public replaceServerUrl(value: string): void {
    if (this.closed) return;
    const url = normalizeServerUrl(value);
    if (url === this.snapshot.serverUrl) return;
    this.release();
    this.controller = new AbortController();
    this.snapshot = this.create(url, this.snapshot.id + 1);
    for (const listener of this.listeners) listener();
  }

  public subscribe = (listener: () => void): (() => void) => {
    if (this.closed) return () => undefined;
    this.listeners.add(listener);
    return () => { this.listeners.delete(listener); };
  };

  public dispose(): void {
    if (this.closed) return;
    this.closed = true;
    this.release();
    this.listeners.clear();
  }

  private create(serverUrl: string, id: number): ConnectionSnapshot {
    this.events = this.eventStream ? new AdvisoryConnection(serverUrl, this.eventStream) : undefined;
    return {
      id, serverUrl,
      events: this.events,
      navigation: new ShellNavigation(),
      client: new TopicStreamClient(serverUrl, this.transport, this.controller.signal),
      queryClient: new QueryClient({ defaultOptions: {
        queries: { retry: (count, error) => !isCancellation(error) && count < 1, staleTime: 30_000 },
        mutations: { retry: false }
      } })
    };
  }

  private release(): void {
    this.events?.dispose();
    this.controller.abort();
    void this.snapshot.queryClient.cancelQueries();
    this.snapshot.queryClient.clear();
  }
}
