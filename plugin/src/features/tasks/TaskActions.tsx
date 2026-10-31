import { useEffect, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { Schema } from "../../api/contracts";
import type { JobActions } from "../../shared/jobs/job-actions";
import { storeJob } from "../../shared/jobs/cache";
import { IconButton } from "../../shared/components/IconButton";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import { ApiError } from "../../api/errors";
import { MODEL_PREPARATION_KIND } from "../../shared/jobs/kinds";

export function TaskActions({ job, actions, onRetry, onSavedResult }: { job: Schema<"Job">; actions: JobActions; onRetry: (id: string) => void; onSavedResult: () => void }) {
  const queries = useQueryClient();
  const [confirm, setConfirm] = useState(false);
  const cancel = useMutation({ mutationFn: () => actions.cancel(job), retry: false, onSuccess: (value) => { storeJob(queries, value); setConfirm(false); void queries.invalidateQueries({ queryKey: ["retrieval-status"] }); } });
  const retry = useMutation({ mutationFn: () => actions.retry(job), retry: false, onSuccess: (value) => {
    onRetry(value.jobId); void queries.invalidateQueries({ queryKey: ["jobs"] }); void queries.invalidateQueries({ queryKey: ["retrieval-status"] });
  }, onError: (error) => {
    if (error instanceof ApiError && error.status === 409 && job.kind === "generate-digest") onSavedResult();
    void queries.invalidateQueries({ queryKey: ["job", job.id] });
    void queries.invalidateQueries({ queryKey: ["retrieval-status"] });
    void queries.invalidateQueries({ queryKey: ["digest-archive"] });
  } });
  const active = ["queued", "running", "retry_wait"].includes(job.status);
  return <><div className="topic-stream-row-tools">
    {active && <IconButton icon="close" label="Cancel task" disabled={cancel.isPending || job.cancellationRequested} onClick={() => { cancel.reset(); setConfirm(true); }} />}
    {retryEligible(job) && <IconButton icon="refresh" label="Retry task" disabled={retry.isPending || (retry.error instanceof ApiError && retry.error.status === 409)} onClick={() => retry.mutate()} />}
  </div>{retry.isPending && <p role="status" className="topic-stream-metadata">Requesting retry…</p>}
    {retry.error && <ApiErrorMessage title="Could not retry this task" error={retry.error} />}
    {confirm && <CancelTaskDialog pending={cancel.isPending} error={cancel.error} onConfirm={() => cancel.mutate()} onClose={() => setConfirm(false)} />}
  </>;
}
function retryEligible(job: Schema<"Job">): boolean {
  if (!["dead", "cancelled"].includes(job.status)) return false;
  if (job.kind !== MODEL_PREPARATION_KIND) return true;
  return Boolean(job.modelTransition?.isCurrent && job.modelTransition.canRetry);
}
function CancelTaskDialog({ pending, error, onConfirm, onClose }: { pending: boolean; error: Error | null; onConfirm: () => void; onClose: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => { dialog.current?.showModal(); }, []);
  return <dialog ref={dialog} className="topic-stream-dialog" aria-label="Cancel this task?" onCancel={(event) => { event.preventDefault(); if (!pending) onClose(); }}>
    <h2>Cancel this task?</h2><p>Running work stops after its current operation. Completed work stays saved.</p>
    <div className="topic-stream-form-actions"><button type="button" disabled={pending} onClick={onClose}>Keep running</button><button type="button" disabled={pending} onClick={onConfirm}>Request cancellation</button></div>
    {pending && <p role="status">Requesting cancellation…</p>}{error && <ApiErrorMessage title="Could not request cancellation" error={error} />}
  </dialog>;
}
