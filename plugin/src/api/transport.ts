export interface HttpRequest {
  url: string;
  method: string;
  headers?: Record<string, string>;
  body?: string;
  contentType?: string;
}

export interface HttpResponse {
  status: number;
  headers?: Record<string, string>;
  text?: string;
  readonly json?: unknown;
  readonly arrayBuffer?: ArrayBuffer;
}

export type HttpTransport = (request: HttpRequest) => Promise<HttpResponse>;

/** Cancels only this consumer; the host's buffered HTTP request may still finish. */
export function waitForResponse(transport: HttpTransport, request: HttpRequest, signal?: AbortSignal): Promise<HttpResponse> {
  if (signal?.aborted) return Promise.reject(new DOMException("Connection closed", "AbortError"));
  return new Promise((resolve, reject) => {
    const abort = () => {
      signal?.removeEventListener("abort", abort);
      reject(new DOMException("Connection closed", "AbortError"));
    };
    signal?.addEventListener("abort", abort, { once: true });
    Promise.resolve().then(() => {
      if (signal?.aborted) throw new DOMException("Connection closed", "AbortError");
      return transport(request);
    }).then(resolve, reject).finally(() => signal?.removeEventListener("abort", abort));
  });
}

export function isCancellation(error: unknown): boolean {
  return typeof error === "object" && error !== null && "name" in error && error.name === "AbortError";
}
