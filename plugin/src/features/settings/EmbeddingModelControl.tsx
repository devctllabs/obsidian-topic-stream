import type { TransitionNotifications } from "./transition-notifications";
import type { JobActions } from "../../shared/jobs/job-actions";
import { ModelJobActions, RecoveredModelJobActions } from "./ModelJobActions";
import type { Schema } from "../../api/contracts";
import { useState, useRef, useEffect } from "react";
import { ModelReplacementDialog } from "./ModelReplacementDialog";
import { useQuery } from "@tanstack/react-query";
import type { ShellNavigation } from "../shell/navigation";
import type { WorkspaceSettingsService } from "./services";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import { IconButton } from "../../shared/components/IconButton";
import { JobProgress } from "../../shared/jobs/JobProgress";

export function EmbeddingModelControl({ service, navigation, value, onChange, jobs, selectionDisabled, transitions }: { service: WorkspaceSettingsService; navigation: ShellNavigation; value: string; onChange: (value: string) => void; jobs: JobActions; selectionDisabled: boolean; transitions: TransitionNotifications }) {
  const [retried, setRetried] = useState<RetriedTransition | null>(null);
  const [watchError, setWatchError] = useState<Error | null>(null);
  const options = useQuery({ queryKey: ["embedding-model-options"], queryFn: () => service.listEmbeddingModelOptions() });
  const status = useQuery({ queryKey: ["retrieval-status"], queryFn: () => service.getRetrievalStatus() });
  const jobId = currentJobId(status.data, retried);
  const job = useQuery({ queryKey: ["job", jobId], queryFn: () => service.getJob(jobId!), enabled: Boolean(jobId) });
  useEffect(() => {
    if (!status.data) return;
    void transitions.observe(status.data, job.data).then(() => { setWatchError(null); }, () => { setWatchError(new Error("Could not reconcile the model completion notice.")); });
  }, [status.data, job.data, transitions]);
  const modelName = (id: string | null) => options.data?.find(({ model }) => model.id === id)?.model.name ?? "Unavailable model";
  const refresh = () => { void options.refetch(); void status.refetch(); if (jobId) void job.refetch(); };
  return <section><div className="topic-stream-section-header"><h3>Retrieval model</h3><IconButton icon="refresh" label="Refresh model status" disabled={status.isFetching} onClick={refresh} /></div>
    <ModelSelection value={value} onChange={onChange} options={options.data ?? []} status={status.data} disabled={selectionDisabled || options.isPending || options.isError} />
    <p className="topic-stream-metadata">Embedding prepares retrieval. Summary generation uses a separate model.</p>
    {status.data && <RetrievalModels status={status.data} name={modelName} />}
    <ModelJob status={status.data} job={job.data} jobPending={job.isPending} jobId={jobId} navigation={navigation} actions={jobs} serving={status.data?.servingModelId ? modelName(status.data.servingModelId) : null} onRetry={(accepted) => {
      const transition = job.data?.modelTransition ?? status.data?.latestTransition;
      if (transition && jobId) setRetried({ ...transition, previousJobId: jobId, jobId: accepted.jobId });
    }} />
    {watchError && <ApiErrorMessage title="Could not check completion notice" error={watchError} />}
    <ModelErrors optionsError={options.error} statusError={status.error} jobError={hasRecoveredOutcome(status.data) ? null : job.error} />
  </section>;
}
function ModelErrors({ optionsError, statusError, jobError }: { optionsError: Error | null; statusError: Error | null; jobError: Error | null }) {
  return <>{optionsError && <ApiErrorMessage title="Could not load embedding models" error={optionsError} />}{statusError && <ApiErrorMessage title="Could not read retrieval status" error={statusError} />}{jobError && <ApiErrorMessage title="Could not read transition job" error={jobError} />}</>;
}

function RetrievalModels({ status, name }: { status: Schema<"RetrievalStatus">; name: (id: string | null) => string }) {
  return <div className="topic-stream-retrieval-models"><p className="topic-stream-metadata">Desired: {name(status.desiredModelId)}</p>
    <p className="topic-stream-metadata">Serving: {status.servingModelId ? name(status.servingModelId) : "None"}</p>
    {!status.servingModelId && <p className="topic-stream-metadata">Retrieval is preparing. Saved Feed history, Settings and Jobs remain available.</p>}
  </div>;
}

function isPreparing(status: Schema<"RetrievalStatus"> | undefined): boolean {
  if (!status) return false;
  if (status.latestTransition) return ["waiting", "queued", "running", "retry_wait"].includes(status.latestTransition.status);
  return status.transitionJobId !== null;
}
function ModelJob({ status, job, jobPending, jobId, navigation, actions, serving, onRetry }: { jobPending: boolean; actions: JobActions; serving: string | null; onRetry: (value: import("../../api/contracts").JobAccepted) => void; status: Schema<"RetrievalStatus"> | undefined; job: Schema<"Job"> | undefined; jobId: string | null | undefined; navigation: ShellNavigation }) {
  if (!status) return null;
  const activated = modelActivated(status, jobId);
  if (activated) return <p role="status" className="topic-stream-metadata">Model active</p>;
  if (!jobId) return <p role="status" className="topic-stream-metadata">Waiting for preparation</p>;
  if (jobPending) return <p role="status" className="topic-stream-metadata">Reading preparation status…</p>;
  if (!job && status?.latestTransition) return <RecoveredModelJobActions transition={status.latestTransition} actions={actions} navigation={navigation} onRetry={onRetry} />;
  return <>{job && <div className="topic-stream-transition-status"><JobProgress job={job} /><ModelJobActions job={job} actions={actions} navigation={navigation} serving={serving} onRetry={onRetry} /></div>}</>;
}

function ModelSelection({ value, onChange, options, status, disabled }: { value: string; onChange: (value: string) => void; options: Schema<"EmbeddingModelOption">[]; status: Schema<"RetrievalStatus"> | undefined; disabled: boolean }) {
  const [pending, setPending] = useState<string | null>(null);
  const select = useRef<HTMLSelectElement>(null);
  const name = (id: string) => options.find(({ model }) => model.id === id)?.model.name ?? "Unavailable model";
  const choose = (id: string) => {
    if (id === value) return;
    if (isPreparing(status)) setPending(id);
    else onChange(id);
  };
  const finish = () => { setPending(null); select.current?.focus(); };
  return <><label>Embedding model<select ref={select} value={value} disabled={disabled || !status} onChange={(event) => choose(event.target.value)}>
    {options.map(({ model, selectable }) => <option key={model.id} value={model.id} disabled={!selectable}>{model.name}{selectable ? "" : " (unavailable)"}</option>)}
  </select></label>
    {pending && <ModelReplacementDialog selected={name(pending)} serving={status?.servingModelId ? name(status.servingModelId) : null} onAccept={() => { onChange(pending); finish(); }} onCancel={finish} />}
  </>;
}

interface RetriedTransition { modelId: string; selectionGeneration: number; jobId: string; previousJobId: string }
function currentJobId(status: Schema<"RetrievalStatus"> | undefined, retried: RetriedTransition | null): string | null {
  if (!status) return null;
  const latest = status.latestTransition;
  if (!latest) return status.transitionJobId;
  if (!retried) return latest.jobId;
  if (latest.modelId === retried.modelId && latest.selectionGeneration === retried.selectionGeneration && latest.jobId === retried.previousJobId) return retried.jobId;
  return latest.jobId;
}

function hasRecoveredOutcome(status: Schema<"RetrievalStatus"> | undefined): boolean {
  const latest = status?.latestTransition;
  return Boolean(latest?.isCurrent && ["succeeded", "dead", "cancelled"].includes(latest.status));
}

function modelActivated(status: Schema<"RetrievalStatus"> | undefined, jobId: string | null | undefined): boolean {
  if (!status || status.servingModelId !== status.desiredModelId) return false;
  if (!jobId) return true;
  return status.latestTransition?.status === "succeeded" && status.latestTransition.isCurrent;
}
