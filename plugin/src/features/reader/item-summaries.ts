import type { QueryClient } from "@tanstack/react-query";
import type { Item, JobAccepted, Schema } from "../../api/contracts";
import type { ViewEvents } from "../../shared/events";
import type { JobActions } from "../../shared/jobs/job-actions";

export interface ItemSummaryService {
  getItemSummary: (id: string, sourceVersion: string) => Promise<Schema<"ItemSummary">>;
  requestItemSummary: (id: string) => Promise<JobAccepted>;
  getJob: (id: string) => Promise<Schema<"Job">>;
  getDigestIssue: (id: string) => Promise<Schema<"DigestIssue">>;
  requestDigestItemSummary: (issueId: string, itemId: string) => Promise<JobAccepted>;
}
export interface CapturedSummaryInput { issueId: string; captured: Schema<"DigestItem">; }
export type SummaryInput = Item | CapturedSummaryInput;
const itemOf = (input: SummaryInput): Item => "captured" in input ? input.captured.item : input;
const key = (input: SummaryInput) => ["item-summary", "captured" in input ? input.issueId : "canonical", itemOf(input).id, itemOf(input).summarySourceVersion] as const;

function exactSummary(input: SummaryInput, state: Schema<"ItemSummary"> | null): Schema<"ItemSummary"> | null {
  if (state === null) return null;
  const version = itemOf(input).summarySourceVersion;
  if (state.sourceVersion !== version || (state.result && state.result.sourceVersion !== version)) throw new Error("The summary belongs to a different source version.");
  return state;
}

/** One view shares exact-version REST state; disclosure remains local to each caller. */
export class ItemSummaries {
  private inputs = new Map<string, { input: SummaryInput; subscribers: number }>();
  private requests = new Map<string, Promise<void>>();
  private disposed = false;
  public constructor(private readonly queries: QueryClient, private readonly client: ItemSummaryService, private readonly jobs: JobActions) {}
  public getSnapshot = (input: SummaryInput): Schema<"ItemSummary"> | null => this.queries.getQueryData(key(input)) ?? null;
  public getResource = (input: SummaryInput) => this.queries.getQueryState<Schema<"ItemSummary"> | null, Error>(key(input));
  public subscribe(input: SummaryInput, listener: () => void): () => void {
    if (this.disposed) return () => undefined;
    const identity = JSON.stringify(key(input));
    const record = this.inputs.get(identity) ?? { input, subscribers: 0 };
    record.subscribers++; this.inputs.set(identity, record);
    const unsubscribe = this.queries.getQueryCache().subscribe((event) => { if (event.query.queryHash === identity) listener(); });
    if (!this.getResource(input)) this.initialize(input);
    return () => { unsubscribe(); record.subscribers--; };
  }
  private initialize(input: SummaryInput): void {
    if ("captured" in input) {
      try { this.queries.setQueryData(key(input), exactSummary(input, input.captured.aiSummary)); return; }
      catch { /* The REST read supplies a recoverable error for invalid capture identity. */ }
    }
    void this.refresh(input).catch(() => undefined);
  }
  public refresh = async (input: SummaryInput): Promise<void> => {
    if (this.disposed) return;
    await this.queries.query({ queryKey: key(input), retry: false, gcTime: Infinity, staleTime: 0, queryFn: async () => {
      const item = itemOf(input);
      const state = "captured" in input ? await this.readCaptured(input) : await this.client.getItemSummary(item.id, item.summarySourceVersion);
      if (this.disposed) throw new DOMException("View closed", "AbortError");
      return exactSummary(input, state);
    } });
  };
  private async readCaptured(input: CapturedSummaryInput): Promise<Schema<"ItemSummary"> | null> {
    const issue = await this.client.getDigestIssue(input.issueId);
    const saved = issue.sections.flatMap((section) => section.items).find(({ item }) => item.id === input.captured.item.id && item.summarySourceVersion === input.captured.item.summarySourceVersion);
    if (issue.id !== input.issueId || !saved) throw new Error("Summary status is unavailable for this saved Item version.");
    return saved.aiSummary;
  }
  public request = (input: SummaryInput): Promise<void> => {
    const item = itemOf(input);
    const identity = JSON.stringify(key(input));
    const active = this.requests.get(identity);
    if (active) return active;
    const state = this.getSnapshot(input);
    if (this.disposed || !state || state.sourceVersion !== item.summarySourceVersion) return Promise.reject(new Error("Summary status is unavailable."));
    if (state.state === "ready" || state.state === "pending") return Promise.resolve();
    if (!state.supported || !state.canGenerate || !item.summarySupported) return Promise.reject(new Error("Summary generation is unavailable for this Item."));
    const response = Promise.resolve().then(() => this.performRequest(input)).finally(() => { this.requests.delete(identity); });
    this.requests.set(identity, response);
    return response;
  };
  private async performRequest(input: SummaryInput): Promise<void> {
    const item = itemOf(input);
    await this.refresh(input);
    if (this.disposed) return;
    const state = this.getSnapshot(input);
    if (!state) throw new Error("Summary status is unavailable.");
    if (state.state === "ready" || state.state === "pending") return;
    if (!state.supported || !state.canGenerate || !item.summarySupported) throw new Error("Summary generation is unavailable for this source version.");
    const accepted = state.state === "failed" ? await this.retryFailed(state) : await this.start(input);
    if (this.disposed) return;
    this.queries.setQueryData(key(input), { ...state, state: "pending", jobId: accepted.jobId, failure: null, retryable: false });
    // Accepted work remains pending if this read fails; a read error cannot replay the POST.
    await this.refresh(input).catch(() => undefined);
  }
  private start(input: SummaryInput): Promise<JobAccepted> {
    return "captured" in input ? this.client.requestDigestItemSummary(input.issueId, input.captured.item.id) : this.client.requestItemSummary(input.id);
  }
  private async retryFailed(state: Schema<"ItemSummary">): Promise<JobAccepted> {
    if (!state.retryable || !state.jobId) throw new Error("This summary cannot be retried.");
    const job = await this.client.getJob(state.jobId);
    if (job.id !== state.jobId) throw new Error("The summary Job could not be verified.");
    return this.jobs.retry(job);
  }
  public connect(events?: ViewEvents): () => void {
    const unsubscribe = events?.subscribe((event) => {
      for (const { input, subscribers } of this.inputs.values()) {
        if (subscribers > 0 && (event.type === "reconnected" || (event.type === "jobChanged" && this.getSnapshot(input)?.jobId === event.jobId))) void this.refresh(input).catch(() => undefined);
      }
    });
    const workspace = this.queries.getQueryCache().subscribe((event) => {
      if (event.type !== "updated" || event.action.type !== "success" || event.query.queryHash !== JSON.stringify(["workspace-settings"])) return;
      for (const { input, subscribers } of this.inputs.values()) if (subscribers > 0) void this.refresh(input).catch(() => undefined);
    });
    return () => { unsubscribe?.(); workspace(); this.disposed = true; void this.queries.cancelQueries({ queryKey: ["item-summary"] }); this.inputs.clear(); this.requests.clear(); };
  }
}
