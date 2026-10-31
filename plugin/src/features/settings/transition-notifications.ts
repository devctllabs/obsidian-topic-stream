import type { Schema } from "../../api/contracts";
import type { WorkspaceStatePort } from "./device-state";

export class TransitionNotifications {
  private selection = 0;
  private closed = false;
  public constructor(private readonly service: { getRetrievalStatus: () => Promise<Schema<"RetrievalStatus">> }, private readonly storage: WorkspaceStatePort, private readonly notify: (message: string) => void) {}
  public async watch(modelId: string, afterGeneration = 0): Promise<void> {
    const selection = ++this.selection;
    await this.storage.update((value) => {
      if (!this.closed && selection === this.selection) value.pendingTransition = { modelId, afterGeneration };
    });
    if (this.closed || selection !== this.selection) return;
    const status = await this.service.getRetrievalStatus();
    if (!this.closed && selection === this.selection) await this.admit(status);
  }
  public async observe(status: Schema<"RetrievalStatus">, job: Schema<"Job"> | undefined): Promise<void> {
    const latest = status.latestTransition;
    if (this.closed || !latest?.isCurrent || job?.cancellationRequested) return;
    if (!await this.hasWatch(status)) return;
    const message = outcomeMessage(status, job);
    if (!message) return;
    let emit = false;
    await this.storage.update((value) => {
      const watch = value.watchedTransitions.current;
      if (this.closed || !watch || watch.notified || value.pendingTransition) return;
      if (watch.modelId !== latest.modelId || watch.selectionGeneration !== latest.selectionGeneration) return;
      watch.jobId = latest.jobId; watch.notified = true; emit = true;
    });
    if (emit && !this.closed) this.notify(message);
  }
  private async hasWatch(status: Schema<"RetrievalStatus">): Promise<boolean> {
    let local = await this.storage.read();
    if (local.pendingTransition) { await this.admit(status); local = await this.storage.read(); }
    return !local.pendingTransition && Boolean(local.watchedTransitions.current);
  }
  private async admit(status: Schema<"RetrievalStatus">): Promise<void> {
    const latest = status.latestTransition;
    if (this.closed || !latest?.isCurrent) return;
    await this.storage.update((value) => {
      const pending = value.pendingTransition;
      if (this.closed || !pending || pending.modelId !== latest.modelId || latest.selectionGeneration <= pending.afterGeneration) return;
      const current = value.watchedTransitions.current;
      if (current?.modelId !== latest.modelId || current.selectionGeneration !== latest.selectionGeneration) {
        value.watchedTransitions.current = { modelId: latest.modelId, selectionGeneration: latest.selectionGeneration, jobId: latest.jobId, notified: false };
      }
      delete value.pendingTransition;
    });
  }
  public dispose(): void { this.closed = true; }
}
function outcomeMessage(status: Schema<"RetrievalStatus">, job: Schema<"Job"> | undefined): string | null {
  const transition = status.latestTransition;
  if (transition?.status === "dead") return "Embedding model preparation failed. Open Tasks for details.";
  if (transition?.status !== "succeeded" || !transition.jobId) return null;
  if (status.servingModelId !== transition.modelId) return null;
  if (job && (job.id !== transition.jobId || job.status !== "succeeded")) return null;
  return "Embedding model is ready.";
}
