import type { JobAccepted, Schema } from "../../api/contracts";
import type { WorkspaceStatePort } from "./device-state";
export interface DigestBuildService {
  generateDigest: (id: string, requestId: string) => Promise<JobAccepted>;
  getJob: (id: string) => Promise<Schema<"Job">>;
}
export interface DigestBuildState { status: Schema<"Job">["status"] | "requesting" | "uncertain" | "unavailable"; jobId: string | null; error?: unknown }
export class DigestBuilds {
  private snapshot: Record<string, DigestBuildState> = {};
  private readonly active = new Map<string, Promise<void>>();
  private readonly listeners = new Set<() => void>();
  private closed = false;
  public constructor(private readonly service: DigestBuildService, private readonly storage: Pick<WorkspaceStatePort, "read" | "update">, private readonly newId: () => string = () => crypto.randomUUID()) {}
  public getSnapshot = (): Record<string, DigestBuildState> => this.snapshot;
  public subscribe = (listener: () => void): (() => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  public async restore(id: string): Promise<void> {
    try {
      const value = await this.storage.read();
      if (this.closed || this.snapshot[id]?.jobId || this.active.has(id)) return;
      if (value.unresolvedBuilds[id]) this.update(id, { status: "uncertain", jobId: null });
      else this.clear(id);
    } catch (error) { if (!this.snapshot[id]) this.update(id, { status: "unavailable", jobId: null, error }); }
  }
  public build(id: string): Promise<void> {
    return this.run(id, async () => {
      const state = this.snapshot[id];
      if (state?.jobId && !terminal.has(state.status)) return this.readJob(id, state.jobId);
      this.update(id, { status: "requesting", jobId: null });
      try {
        const local = await this.storage.read();
        const requestId = local.unresolvedBuilds[id] ?? this.newId();
        await this.storage.update((value) => { value.unresolvedBuilds[id] = requestId; });
        if (this.closed) return;
        const accepted = await this.service.generateDigest(id, requestId);
        if (this.closed) return;
        this.update(id, { status: "queued", jobId: accepted.jobId });
        await this.readJob(id, accepted.jobId);
      } catch (error) { this.update(id, { status: "uncertain", jobId: null, error }); }
    });
  }
  public refresh(id: string): Promise<void> {
    const jobId = this.snapshot[id]?.jobId;
    return jobId ? this.run(id, () => this.readJob(id, jobId)) : Promise.resolve();
  }
  public async observe(job: Schema<"Job">): Promise<void> {
    for (const [id, state] of Object.entries(this.snapshot)) {
      if (state.jobId !== job.id) continue;
      try {
        if (terminal.has(job.status)) await this.storage.update((value) => { delete value.unresolvedBuilds[id]; });
        this.update(id, { status: job.status, jobId: job.id });
      } catch (error) { this.update(id, { status: "unavailable", jobId: job.id, error }); }
    }
  }
  public dispose(): void { this.closed = true; this.active.clear(); this.listeners.clear(); }
  private run(id: string, action: () => Promise<void>): Promise<void> {
    if (this.closed) return Promise.resolve();
    const pending = this.active.get(id);
    if (pending) return pending;
    const work = Promise.resolve().then(() => { if (this.closed) return; return action(); }).finally(() => { this.active.delete(id); });
    this.active.set(id, work);
    return work;
  }
  private async readJob(id: string, jobId: string): Promise<void> {
    try {
      const job = await this.service.getJob(jobId);
      if (this.closed) return;
      if (terminal.has(job.status)) await this.storage.update((value) => { delete value.unresolvedBuilds[id]; });
      this.update(id, { status: job.status, jobId });
    } catch (error) { this.update(id, { status: "unavailable", jobId, error }); }
  }
  private clear(id: string): void {
    if (this.closed || !this.snapshot[id]) return;
    const next = { ...this.snapshot }; delete next[id]; this.snapshot = next;
    for (const listener of this.listeners) listener();
  }
  private update(id: string, state: DigestBuildState): void {
    if (this.closed) return;
    this.snapshot = { ...this.snapshot, [id]: state };
    for (const listener of this.listeners) listener();
  }
}
const terminal = new Set<string>(["succeeded", "dead", "cancelled"]);
