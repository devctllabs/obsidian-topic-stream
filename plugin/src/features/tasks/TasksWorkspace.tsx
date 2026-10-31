import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useState, useSyncExternalStore } from "react";
import type { Schema } from "../../api/contracts";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import { IconButton } from "../../shared/components/IconButton";
import { JobProgress, jobStatusLabel } from "../../shared/jobs/JobProgress";
import { SplitPane } from "../shell/SplitPane";
import type { SectionProps } from "../shell/TopicStreamShell";
import type { TasksService } from "./services";
import { TaskActions } from "./TaskActions";
import { TaskModelContext } from "./TaskModelContext";
import { TaskSavedIssue } from "./TaskSavedIssue";
import type { ShellNavigation } from "../shell/navigation";
import type { JobActions } from "../../shared/jobs/job-actions";
import { MODEL_PREPARATION_KIND } from "../../shared/jobs/kinds";

export function TasksWorkspace({ client, actions, transitionError, ...props }: SectionProps & { client: TasksService; actions: JobActions; transitionError: Error | null }) {
  const state = useSyncExternalStore(props.navigation.subscribe, props.navigation.getSnapshot);
  const [status, setStatus] = useState<Schema<"Job">["status"] | "">("");
  const id = state.selections.tasks;
  const selected = useQuery({ queryKey: ["job", id], queryFn: () => client.getJob(id!), enabled: Boolean(id), retry: false, staleTime: Infinity, refetchOnMount: "always", refetchOnWindowFocus: false, refetchOnReconnect: false });
  const list = <>{transitionError && <ApiErrorMessage title="Could not check model completion notice" error={transitionError} />}<TaskList client={client} status={status} onStatus={setStatus} onSelect={(id) => props.navigation.select("tasks", id)} onRefreshSelected={() => { if (id) void selected.refetch(); }} /></>;
  const detail = <>{selected.data && selected.error && <ApiErrorMessage title="Could not refresh this task. Showing the last saved status." error={selected.error} />}{selected.data ? <TaskDetail key={selected.data.id} client={client} job={selected.data} actions={actions} navigation={props.navigation} refreshing={selected.isFetching} onRefresh={() => { void selected.refetch(); }} /> : <TaskFeedback error={selected.error} onRetry={() => { void selected.refetch(); }} />}</>;
  return <SplitPane {...props} selection={id ?? null} onBack={() => props.navigation.select("tasks", null)} backLabel="Back to tasks" emptyMessage="Choose a task to see its details" positionKey={`tasks:${status}`} list={list} detail={detail} />;
}

function TaskList({ client, status, onStatus, onSelect, onRefreshSelected }: { client: TasksService; status: Schema<"Job">["status"] | ""; onStatus: (status: Schema<"Job">["status"] | "") => void; onSelect: (id: string) => void; onRefreshSelected: () => void }) {
  const jobs = useInfiniteQuery({ queryKey: ["jobs", status], retry: false, staleTime: Infinity, refetchOnWindowFocus: false, refetchOnReconnect: false,
    initialPageParam: null as string | null, queryFn: ({ pageParam }) => client.listJobs({ limit: 30, ...(status ? { status } : {}), ...(pageParam ? { cursor: pageParam } : {}) }), getNextPageParam: (page) => page.nextCursor });
  const entries = jobs.data?.pages.flatMap((page) => page.items) ?? [];
  return <><div className="topic-stream-section-header"><h2>Tasks</h2><IconButton icon="refresh" label="Refresh tasks" disabled={jobs.isFetching} onClick={() => { void jobs.refetch(); onRefreshSelected(); }} /></div>
    <label className="topic-stream-archive-filter">Task status<select value={status} onChange={(event) => onStatus(event.target.value as typeof status)}><option value="">All statuses</option>{Object.entries(taskStatuses).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
    {jobs.isPending && <p role="status" className="topic-stream-metadata">Loading tasks…</p>}
    {jobs.error && <ApiErrorMessage title="Could not load tasks. Refresh to try again." error={jobs.error} />}
    {jobs.isSuccess && entries.length === 0 && <p className="topic-stream-metadata">No tasks in this view.</p>}
    <ul className="topic-stream-reading-list" aria-label="Tasks">{entries.map((job) => <li key={job.id}><button type="button" className="topic-stream-row" data-item-id={job.id} onClick={() => onSelect(job.id)}><span className="topic-stream-row-title">{jobTitle(job)}</span><span className="topic-stream-row-meta"><span>{queueLabels[job.queue]}</span><span>{jobStatusLabel(job)}</span></span></button></li>)}</ul>
    {jobs.hasNextPage && <button type="button" className="topic-stream-refresh" disabled={jobs.isFetchingNextPage} onClick={() => { void jobs.fetchNextPage(); }}>Load older tasks</button>}
  </>;
}
function TaskFeedback({ error, onRetry }: { error: Error | null; onRetry: () => void }) { return error ? <><ApiErrorMessage title="Could not load this task" error={error} /><button type="button" className="topic-stream-refresh" onClick={onRetry}>Check task status</button></> : <p role="status" className="topic-stream-metadata">Loading task…</p>; }

function TaskDetail({ job, actions, navigation, client, refreshing, onRefresh }: { job: Schema<"Job">; actions: JobActions; navigation: ShellNavigation; client: TasksService; refreshing: boolean; onRefresh: () => void }) {
  const [committed, setCommitted] = useState(false);
  return <article className="topic-stream-task-detail"><div className="topic-stream-section-header"><h1>{jobTitle(job)}</h1><IconButton icon="refresh" label="Refresh selected task" disabled={refreshing} onClick={onRefresh} /></div><div className="topic-stream-transition-status"><JobProgress job={job} /><TaskActions job={job} actions={actions} onRetry={(id) => navigation.openTask(id)} onSavedResult={() => setCommitted(true)} /></div>
    {job.kind === MODEL_PREPARATION_KIND && <TaskModelContext job={job} client={client} />}
    {job.kind === "generate-digest" && (committed || job.status === "succeeded") && <TaskSavedIssue client={client} jobId={job.id} navigation={navigation} />}
    <dl className="topic-stream-task-facts"><dt>Queue</dt><dd>{queueLabels[job.queue]}</dd><dt>Attempts</dt><dd>{job.attempt} / {job.maxAttempts}</dd><dt>Job ID</dt><dd>{job.id}</dd>
      <dt>Created</dt><dd><Timestamp value={job.createdAt} /></dd><dt>Updated</dt><dd><Timestamp value={job.updatedAt} /></dd>
      <dt>Started</dt><dd><Timestamp value={job.startedAt} /></dd><dt>Finished</dt><dd><Timestamp value={job.finishedAt} /></dd>
      <dt>Duration</dt><dd>{job.durationMs == null ? "Not available" : `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 }).format(job.durationMs / 1000)} s`}</dd>
    </dl>{job.reason && <section aria-label="Task reason"><h2>Reason</h2><p>{job.reason}</p></section>}
  </article>;
}
function Timestamp({ value }: { value: string | null | undefined }) { return value ? <time dateTime={value}>{new Date(value).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "medium" })}</time> : <>Not recorded</>; }
const queueLabels: Record<Schema<"JobQueue">, string> = { fetch: "Fetch", generation: "Generation", retrieval: "Retrieval", "feed-preparation": "Feed Preparation", maintenance: "Maintenance" };
const taskStatuses = { queued: "Queued", running: "Running", retry_wait: "Retry scheduled", succeeded: "Succeeded", dead: "Failed", cancelled: "Cancelled" };
function jobTitle(job: Schema<"Job">): string {
  const labels: Record<string, string> = { [MODEL_PREPARATION_KIND]: "Prepare retrieval index", "sync-source": "Fetch source", "sync-all-sources": "Fetch all sources", "generate-digest": "Build Digest Issue", "summarize-item": "Summarize Item", "prepare-feed": "Prepare feed" };
  return labels[job.kind] ?? job.kind;
}
