/** Advisory payloads only. The host transport owns connection, coalescing and teardown. */
export type ViewEvent =
  | { type: "feedInvalidated"; itemId: string }
  | { type: "jobChanged"; jobId: string }
  | { type: "digestReady"; issueId: string }
  | { type: "reconnected" };

export interface ViewEvents {
  subscribe: (listener: (event: ViewEvent) => void) => () => void;
}
