import type { Schema } from "../../api/contracts";
export function JobProgress({ job }: { job: Schema<"Job"> }) {
  const progress = job.progress;
  return <div className="topic-stream-job-progress" data-status={job.status}>
    <p role="status" className="topic-stream-metadata">{jobStatusLabel(job)}</p>
    {progress && <>
      <div className="topic-stream-progress-caption"><span>{phaseLabels[progress.phase]}</span><span>{progress.totalItems === null ? `${progress.processedItems} Items · total not known yet` : `${progress.processedItems} / ${progress.totalItems} Items`}</span></div>
      <progress aria-label="Initial Item cohort" max={Math.max(progress.totalItems ?? 1, 1)} value={progress.totalItems === null ? undefined : progress.processedItems} />
      <p className="topic-stream-metadata">Updated {new Date(progress.updatedAt).toLocaleString("en")}</p>
    </>}
  </div>;
}
const phaseLabels = { preparing: "Preparing", indexing: "Indexing initial Items", catching_up: "Catching up", activating: "Activating", completed: "Completed" };
const statusLabels = { queued: "Queued", running: "Running", retry_wait: "Retry scheduled", succeeded: "Succeeded", dead: "Failed", cancelled: "Cancelled" };
export const jobStatusLabel = (job: Schema<"Job">): string => job.cancellationRequested && ["queued", "running", "retry_wait"].includes(job.status) ? "Cancellation requested" : statusLabels[job.status];
