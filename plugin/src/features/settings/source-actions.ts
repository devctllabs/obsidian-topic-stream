import type { JobAccepted, Schema, Source, SourceInput } from "../../api/contracts";

export interface SourceWorkflowService {
  createSource: (input: SourceInput) => Promise<Source>;
  syncSource: (id: string) => Promise<JobAccepted>;
  getJob: (id: string) => Promise<Schema<"Job">>;
}
export interface SourceSyncState {
  status: Schema<"Job">["status"] | "requesting" | "failed" | "unavailable";
  jobId: string | null;
  error?: unknown;
}
export class SourceActions {
  private snapshot: Record<string, SourceSyncState> = {};
  private readonly active = new Map<string, Promise<void>>();
  private closed = false;
  private readonly listeners = new Set<() => void>();
  public subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => { this.listeners.delete(listener); };
  };
  public constructor(private readonly service: SourceWorkflowService) {}
  public async create(input: SourceInput): Promise<Source> {
    const source = await this.service.createSource(input);
    if (!this.closed) void this.sync(source.id);
    return source;
  }
  public getSnapshot = (): Record<string, SourceSyncState> => this.snapshot;
  public sync(id: string): Promise<void> {
    const pending = this.active.get(id);
    if (pending) return pending;
    const state = this.snapshot[id];
    if (state?.jobId && !terminalStatuses.includes(state.status)) return Promise.resolve();
    return this.run(id, () => this.startSync(id));
  }
  public refresh(id: string): Promise<void> {
    const jobId = this.snapshot[id]?.jobId;
    return jobId ? this.run(id, () => this.readJob(id, jobId)) : Promise.resolve();
  }
  public dispose(): void { this.closed = true; this.active.clear(); this.listeners.clear(); }

  private run(id: string, action: () => Promise<void>): Promise<void> {
    if (this.closed) return Promise.resolve();
    const pending = this.active.get(id);
    if (pending) return pending;
    const work = Promise.resolve().then(() => {
      if (this.closed) return;
      return action();
    }).finally(() => { this.active.delete(id); });
    this.active.set(id, work);
    return work;
  }
  private async startSync(id: string): Promise<void> {
    this.update(id, { status: "requesting", jobId: null });
    try {
      const accepted = await this.service.syncSource(id);
      if (this.closed) return;
      this.update(id, { status: "queued", jobId: accepted.jobId });
      await this.readJob(id, accepted.jobId);
    } catch (error) { this.update(id, { status: "failed", jobId: null, error }); }
  }
  private async readJob(id: string, jobId: string): Promise<void> {
    try {
      const job = await this.service.getJob(jobId);
      this.update(id, { status: job.status, jobId });
    } catch (error) { this.update(id, { status: "unavailable", jobId, error }); }
  }
  private update(id: string, state: SourceSyncState): void {
    if (this.closed) return;
    this.snapshot = { ...this.snapshot, [id]: state };
    for (const listener of this.listeners) listener();
  }
}
const terminalStatuses: SourceSyncState["status"][] = ["succeeded", "dead", "cancelled"];
