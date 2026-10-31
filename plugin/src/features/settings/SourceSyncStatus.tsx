import type { Source } from "../../api/contracts";
import type { SourceActions, SourceSyncState } from "./source-actions";
import { IconButton } from "../../shared/components/IconButton";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";

export function SourceSyncButton({ source, state, actions }: { source: Source; state: SourceSyncState | undefined; actions: SourceActions }) {
  const action = sourceSyncAction(state);
  return <IconButton icon="refresh" label={`${action.label} for ${source.name}`} disabled={action.disabled} onClick={() => { void (action.check ? actions.refresh(source.id) : actions.sync(source.id)); }} />;
}
export function SourceSyncMessage({ state, sourceName, onOpenJob, compact = false }: { compact?: boolean; state: SourceSyncState | undefined; sourceName: string; onOpenJob: (id: string) => void }) {
  if (!state || compact && state.status === "succeeded") return null;
  const errorTitle = state.status === "failed" ? "Could not sync source" : state.status === "unavailable" ? "Could not read sync status" : null;
  const jobId = state.jobId;
  return <>
    {errorTitle ? <ApiErrorMessage title={errorTitle} error={state.error} /> : <p role="status" className="topic-stream-metadata">{syncLabels[state.status]}</p>}
    {jobId && !compact && <button type="button" className="topic-stream-text-button" aria-label={`View task for ${sourceName}`} onClick={() => onOpenJob(jobId)}>View task</button>}
  </>;
}
const syncLabels: Record<SourceSyncState["status"], string> = {
  requesting: "Starting sync…", queued: "Sync queued", running: "Sync running", retry_wait: "Retry scheduled",
  succeeded: "Sync complete", dead: "Sync needs attention", cancelled: "Sync cancelled", failed: "Sync failed", unavailable: "Status unavailable"
};

export function sourceSyncAction(state: SourceSyncState | undefined) {
  const check = Boolean(state?.jobId && !["succeeded", "dead", "cancelled"].includes(state.status));
  const retry = state?.status === "failed" || state?.status === "dead";
  return { retry, label: check ? "Check sync status" : retry ? "Retry sync" : "Sync now", check, disabled: state?.status === "requesting" };
}
