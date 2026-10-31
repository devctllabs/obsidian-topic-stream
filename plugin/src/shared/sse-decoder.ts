import type { ViewEvent } from "./events";

const limit = 65_536;
const fields = { feedInvalidated: "itemId", digestReady: "issueId", jobChanged: "jobId" } as const;
const uuid = /^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/i;

/** Incremental UTF-8/SSE framing; only advisory IDs cross this boundary, never arbitrary payloads. */
export class SSEDecoder {
  private readonly utf8 = new TextDecoder("utf-8", { fatal: true });
  private buffer = "";
  private skipLF = false;
  private name = "";
  private data: string[] = [];
  private size = 0;
  public constructor(private readonly receive: (event: ViewEvent) => void, private readonly connected: () => void) {}
  public push(bytes: Uint8Array): void {
    this.buffer += this.utf8.decode(bytes, { stream: true });
    while (this.buffer.length) {
      if (this.skipLF) { if (this.buffer.startsWith("\n")) this.buffer = this.buffer.slice(1); this.skipLF = false; }
      const index = this.buffer.search(/[\r\n]/);
      if (index < 0) break;
      const line = this.buffer.slice(0, index);
      this.skipLF = this.buffer[index] === "\r";
      this.buffer = this.buffer.slice(index + 1);
      this.line(line);
    }
    if (this.buffer.length > limit) throw new Error("Event line is too large.");
  }
  private line(line: string): void {
    if (line.length > limit) throw new Error("Event line is too large.");
    if (!line) { this.flush(); return; }
    if (line.startsWith(":")) { if (line.slice(1).trim() === "connected") this.connected(); return; }
    const colon = line.indexOf(":");
    const field = colon < 0 ? line : line.slice(0, colon);
    const raw = colon < 0 ? "" : line.slice(colon + 1);
    const value = raw.startsWith(" ") ? raw.slice(1) : raw;
    this.field(field, value);
  }
  private field(name: string, value: string): void {
    if (name === "event") this.name = value;
    if (name === "data") { this.data.push(value); this.size += value.length + 1; }
    if (this.size > limit) throw new Error("Event data is too large.");
  }
  private flush(): void {
    if (this.data.length) {
      const event = decodeEvent(this.name, this.data.join("\n"));
      if (event) this.receive(event);
    }
    this.name = ""; this.data = []; this.size = 0;
  }
}

function decodeEvent(name: string, data: string): ViewEvent | null {
  if (!Object.hasOwn(fields, name)) return null;
  const value: unknown = JSON.parse(data);
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("Invalid event data.");
  const id = (value as Record<string, unknown>)[fields[name as keyof typeof fields]];
  if (typeof id !== "string" || !uuid.test(id)) throw new Error("Invalid event resource ID.");
  switch (name) {
    case "feedInvalidated": return { type: name, itemId: id };
    case "digestReady": return { type: name, issueId: id };
    case "jobChanged": return { type: name, jobId: id };
    default: return null;
  }
}
