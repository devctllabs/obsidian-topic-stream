import type { JobAccepted, Schema } from "../../api/contracts";
import { MODEL_PREPARATION_KIND } from "./kinds";
export interface JobActionService {
  cancelJob: (id: string) => Promise<Schema<"Job">>;
  retryJob: (id: string) => Promise<JobAccepted>;
}
export type JobRetryTarget = Pick<Schema<"Job">, "id" | "status" | "modelTransition"> & Partial<Pick<Schema<"Job">, "kind">>;
export class JobActions {
  private readonly active = new Map<string, { kind: string; response: Promise<unknown> }>();
  private closed = false;
  public constructor(private readonly service: JobActionService) {}
  public cancel(job: Schema<"Job">): Promise<Schema<"Job">> {
    if (job.cancellationRequested) return Promise.resolve(job);
    if (!["queued", "running", "retry_wait"].includes(job.status)) return Promise.reject(new Error("This Job cannot be cancelled."));
    return this.run(job.id, "cancel", () => this.service.cancelJob(job.id));
  }
  public retry(job: JobRetryTarget): Promise<JobAccepted> {
    if (!["dead", "cancelled"].includes(job.status)) return Promise.reject(new Error("This Job cannot be retried."));
    const transition = job.modelTransition;
    if (job.kind === MODEL_PREPARATION_KIND && !transition) return Promise.reject(new Error("This model preparation has no verified Retry target."));
    if (transition && (!transition.isCurrent || !transition.canRetry)) return Promise.reject(new Error("This model preparation is no longer eligible for Retry."));
    return this.run(job.id, "retry", () => this.service.retryJob(job.id));
  }
  public dispose(): void { this.closed = true; this.active.clear(); }
  private run<T>(id: string, kind: string, action: () => Promise<T>): Promise<T> {
    if (this.closed) return Promise.reject(new DOMException("Connection closed", "AbortError"));
    const pending = this.active.get(id);
    if (pending) return pending.kind === kind ? pending.response as Promise<T> : Promise.reject(new Error("Another Job action is in progress."));
    const response = Promise.resolve().then(() => {
      if (this.closed) throw new DOMException("Connection closed", "AbortError");
      return action();
    }).finally(() => { this.active.delete(id); });
    this.active.set(id, { kind, response });
    return response;
  }
}
