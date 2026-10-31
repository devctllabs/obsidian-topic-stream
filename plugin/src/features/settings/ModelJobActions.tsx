import { useEffect, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { JobAccepted, Schema } from "../../api/contracts";
import type { JobActions, JobRetryTarget } from "../../shared/jobs/job-actions";
import { IconButton } from "../../shared/components/IconButton";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import type { ShellNavigation } from "../shell/navigation";
import { storeJob } from "../../shared/jobs/cache";
export function ModelJobActions({ job, actions, navigation, serving, onRetry }: { job: Schema<"Job">; actions: JobActions; navigation: ShellNavigation; serving: string | null; onRetry: (value: JobAccepted) => void }) {
  const queries = useQueryClient();
  const [confirm, setConfirm] = useState(false);
  const cancel = useMutation({ mutationFn: () => actions.cancel(job), onSuccess: (value) => {
    storeJob(queries, value); setConfirm(false);
    void queries.invalidateQueries({ queryKey: ["retrieval-status"] });
  } });
  const canCancel = ["queued", "running", "retry_wait"].includes(job.status);
  const canRetry = job.modelTransition?.isCurrent && job.modelTransition.canRetry;
  return <><div className="topic-stream-row-tools">
    {canCancel && <IconButton icon="close" label="Cancel model preparation" disabled={cancel.isPending || job.cancellationRequested} onClick={() => { cancel.reset(); setConfirm(true); }} />}
    {canRetry && <ModelRetry target={job} actions={actions} onRetry={onRetry} />}
    <IconButton icon="tasks" label="View model transition task" onClick={() => navigation.openTask(job.id)} />
  </div>
    {confirm && <CancelModelDialog serving={serving} pending={cancel.isPending} error={cancel.error} onConfirm={() => cancel.mutate()} onCancel={() => setConfirm(false)} />}
  </>;
}
function CancelModelDialog({ serving, pending, error, onConfirm, onCancel }: { serving: string | null; pending: boolean; error: unknown; onConfirm: () => void; onCancel: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => { dialog.current?.showModal(); }, []);
  return <dialog ref={dialog} className="topic-stream-dialog" aria-label="Cancel model preparation?" onCancel={(event) => { event.preventDefault(); if (!pending) onCancel(); }}>
    <h2>Cancel model preparation?</h2><p>Desired selection stays unchanged. Preparation will stop after the current operation.</p>
    <p>{serving ? `${serving} keeps serving retrieval.` : "No model is serving. Retrieval stays unavailable until you deliberately retry or select another model. Saved Feed history, Settings and Jobs remain usable."}</p>
    <div className="topic-stream-form-actions"><button type="button" disabled={pending} onClick={onCancel}>Keep preparing</button><button type="button" disabled={pending} onClick={onConfirm}>Cancel preparation</button></div>
    {pending && <p role="status">Requesting cancellation…</p>}{error != null && <ApiErrorMessage title="Could not cancel model preparation" error={error} />}
  </dialog>;
}

function ModelRetry({ target, actions, onRetry }: { target: JobRetryTarget; actions: JobActions; onRetry: (value: JobAccepted) => void }) {
  const queries = useQueryClient();
  const retry = useMutation({ mutationFn: () => actions.retry(target), onSuccess: (value) => { onRetry(value); void queries.invalidateQueries({ queryKey: ["retrieval-status"] }); } });
  return <div><IconButton icon="refresh" label="Retry model preparation" disabled={retry.isPending} onClick={() => retry.mutate()} />
    {retry.isPending && <p role="status" className="topic-stream-metadata">Requesting retry…</p>}
    {retry.isError && <ApiErrorMessage title="Could not retry model preparation" error={retry.error} />}
  </div>;
}
export function RecoveredModelJobActions({ transition, actions, navigation, onRetry }: { transition: Schema<"ModelTransition">; actions: JobActions; navigation: ShellNavigation; onRetry: (value: JobAccepted) => void }) {
  if (!transition.isCurrent || !transition.jobId) return null;
  if (transition.status !== "dead" && transition.status !== "cancelled") return null;
  const target: JobRetryTarget = { id: transition.jobId, status: transition.status, modelTransition: transition };
  return <div><p role="status" className="topic-stream-metadata">{transition.status === "dead" ? "Preparation failed" : "Preparation cancelled"}</p><div className="topic-stream-row-tools">
    {transition.canRetry && <ModelRetry target={target} actions={actions} onRetry={onRetry} />}
    <IconButton icon="tasks" label="View model transition task" onClick={() => navigation.openTask(target.id)} />
  </div></div>;
}
