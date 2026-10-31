export interface EventStreamRequest { url: string; signal: AbortSignal; receive: (bytes: Uint8Array) => void; }
/** Resolves on EOF; rejects on network/protocol failure or cancellation. No buffered REST. */
export type EventStreamTransport = (request: EventStreamRequest) => Promise<void>;
