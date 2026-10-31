import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent, waitFor, within } from "storybook/test";
import { ConnectedShell } from "../shell/ConnectedShell";
import { ShellNavigation } from "../shell/navigation";
import { interactiveScenario } from "../../test/storybook/interactive-scenario";
import { memoryWorkspaceState } from "../../test/storybook/local-state";
import { memoryFeedPreferences } from "../../test/storybook/feed-preferences";
import { fixtureId, makeJob } from "../../test/storybook/fixtures";
import type { QueryParameters, Schema } from "../../api/contracts";
import { deferred } from "../../test/storybook/deferred";
import { makeRetrievalStatus } from "../../test/storybook/fixtures";
import { makeIssue } from "../../test/storybook/fixtures";
import { ApiError } from "../../api/errors";

const client = interactiveScenario();
client.jobs.clear();
const queues: Schema<"JobQueue">[] = ["fetch", "generation", "retrieval", "feed-preparation", "maintenance"];
queues.forEach((queue, n) => { const job = makeJob({ id: fixtureId(400 + n), kind: n === 2 ? "reindex-workspace" : `fixture-${queue}`, queue }); client.jobs.set(job.id, job); });
const service = Object.assign(client, { listJobs: fn(async (_input: QueryParameters<"listJobs">): Promise<Schema<"JobList">> => ({ items: [...client.jobs.values()], nextCursor: null })) });
const args = { client: service, navigation: new ShellNavigation({ primary: "feed", screen: "tasks" }), localState: memoryWorkspaceState(), notify: client.notify, feedPreferences: memoryFeedPreferences(), events: client.events };
const meta = { title: "Features/Tasks/Views/TasksWorkspace", component: ConnectedShell, args, parameters: { shell: true } } satisfies Meta<typeof ConnectedShell>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Loaded: Story = { play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  for (const label of ["Fetch", "Generation", "Retrieval", "Feed Preparation", "Maintenance"]) await expect(await canvas.findByText(label)).toBeVisible();
  await expect(service.listJobs).toHaveBeenCalledWith({ limit: 30 });
  await userEvent.click(canvas.getByRole("button", { name: /Prepare retrieval index/ }));
  await expect(await canvas.findByRole("heading", { name: "Prepare retrieval index", level: 1 })).toBeVisible();
  await expect(canvas.getByText("1 / 5")).toBeVisible();
  await expect(canvas.getByText(fixtureId(402))).toBeVisible();
} };

const cancellingClient = interactiveScenario();
const cancellingJob = makeJob({ kind: "generate-digest", queue: "generation" });
cancellingClient.jobs.set(cancellingJob.id, cancellingJob);
const cancellation = deferred<Schema<"Job">>();
cancellingClient.cancelJob = fn(() => cancellation.promise);
export const CooperativeCancellation: Story = { args: { ...args, client: cancellingClient, events: cancellingClient.events, navigation: new ShellNavigation({ primary: "feed", screen: "tasks", selections: { tasks: cancellingJob.id } }) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: "Cancel task" }));
  const dialog = within(await within(canvasElement.ownerDocument.body).findByRole("dialog", { name: "Cancel this task?" }));
  await userEvent.click(dialog.getByRole("button", { name: "Request cancellation" }));
  await expect(await dialog.findByText("Requesting cancellation…")).toBeVisible();
  cancellation.resolve({ ...cancellingJob, cancellationRequested: true });
  await expect(await within(canvas.getByRole("region", { name: "Reading pane" })).findByText("Cancellation requested")).toBeVisible();
  await expect(canvas.getByRole("button", { name: "Cancel task" })).toBeDisabled();
  await expect(cancellingClient.cancelJob).toHaveBeenCalledTimes(1);
  await expect(canvas.queryByRole("progressbar")).not.toBeInTheDocument();
} };

const retryClient = interactiveScenario();
const failed = makeJob({ kind: "generate-digest", queue: "generation", status: "dead", reason: "Synthetic provider failure" });
const successor = { ...failed, id: fixtureId(450), status: "succeeded" as const, reason: null };
retryClient.jobs.set(failed.id, failed); retryClient.jobs.set(successor.id, successor);
retryClient.retryJob = fn(async () => ({ jobId: successor.id }));
retryClient.getJob = fn(retryClient.getJob);
export const RetryReturnsSavedSuccessor: Story = { args: { ...args, client: retryClient, events: retryClient.events, navigation: new ShellNavigation({ primary: "feed", screen: "tasks", selections: { tasks: failed.id } }) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await expect(await canvas.findByText("Synthetic provider failure")).toBeVisible();
  await userEvent.click(canvas.getByRole("button", { name: "Retry task" }));
  await expect(await canvas.findByText(successor.id)).toBeVisible();
  await expect(within(canvas.getByRole("region", { name: "Reading pane" })).getByText("Succeeded")).toBeVisible();
  await expect(retryClient.retryJob).toHaveBeenCalledTimes(1);
  await waitFor(() => expect(retryClient.getJob).toHaveBeenCalledWith(successor.id));
} };

const eventClient = interactiveScenario();
const progressJob = makeJob({ progress: { phase: "indexing", processedItems: 2, totalItems: 5, updatedAt: "2026-10-28T09:00:00Z" } });
eventClient.jobs.set(progressJob.id, progressJob);
eventClient.getJob = fn(eventClient.getJob);
eventClient.listJobs = fn(eventClient.listJobs);
export const AdvisoryAndReconnect: Story = { args: { ...args, client: eventClient, events: eventClient.events, navigation: new ShellNavigation({ primary: "feed", screen: "tasks", selections: { tasks: progressJob.id } }) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const pane = within(canvas.getByRole("region", { name: "Reading pane" }));
  await expect(await pane.findByText("2 / 5 Items")).toBeVisible();
  eventClient.jobs.set(progressJob.id, { ...progressJob, progress: { ...progressJob.progress!, processedItems: 5, phase: "catching_up" } });
  eventClient.events.emit({ type: "jobChanged", jobId: progressJob.id });
  await expect(await pane.findByText("5 / 5 Items")).toBeVisible();
  await expect(pane.getByText("Running")).toBeVisible();
  await expect(pane.getByText("Catching up")).toBeVisible();
  eventClient.jobs.set(progressJob.id, { ...progressJob, status: "succeeded", progress: { ...progressJob.progress!, processedItems: 5, phase: "completed" } });
  eventClient.events.emit({ type: "reconnected" });
  await expect(await pane.findByText("Succeeded")).toBeVisible();
  await expect(eventClient.getJob).toHaveBeenCalledTimes(3);
  await expect(eventClient.listJobs).toHaveBeenCalledTimes(3);
} };

const outcomeClient = interactiveScenario();
outcomeClient.retrievalStatus = makeRetrievalStatus({ servingModelId: fixtureId(601), latestTransition: { modelId: fixtureId(601), selectionGeneration: 1, jobId: fixtureId(400), status: "succeeded", isCurrent: true, canRetry: false } });
outcomeClient.jobs.set(fixtureId(400), makeJob({ status: "succeeded", modelTransition: outcomeClient.retrievalStatus.latestTransition! }));
const outcomeLocal = memoryWorkspaceState();
void outcomeLocal.update((value) => { value.watchedTransitions.current = { modelId: fixtureId(601), selectionGeneration: 1, jobId: fixtureId(400), notified: false }; });
export const RestoredTransitionOutcome: Story = { args: { ...args, client: outcomeClient, notify: outcomeClient.notify, localState: outcomeLocal, events: outcomeClient.events, navigation: new ShellNavigation({ primary: "feed", screen: "tasks" }) }, play: async () => {
  await waitFor(() => expect(outcomeClient.notices).toEqual(["Embedding model is ready."]));
  outcomeClient.events.emit({ type: "reconnected" });
  await waitFor(async () => expect((await outcomeLocal.read()).watchedTransitions.current?.notified).toBe(true));
  await expect(outcomeClient.notices).toHaveLength(1);
} };

const eligibilityClient = interactiveScenario();
const currentTransition = { modelId: fixtureId(601), selectionGeneration: 1, jobId: fixtureId(400), status: "dead" as const, isCurrent: true, canRetry: true };
const eligibleJob = makeJob({ status: "dead", modelTransition: currentTransition });
eligibilityClient.jobs.set(eligibleJob.id, eligibleJob);
eligibilityClient.retryJob = fn(async () => {
  eligibilityClient.jobs.set(eligibleJob.id, { ...eligibleJob, modelTransition: { ...currentTransition, isCurrent: false, canRetry: false } });
  throw new ApiError(409, null);
});
export const StaleModelRetryEligibility: Story = { args: { ...args, client: eligibilityClient, events: eligibilityClient.events, navigation: new ShellNavigation({ primary: "feed", screen: "tasks", selections: { tasks: eligibleJob.id } }) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await expect(await canvas.findByText("Target model: Fixture model 2")).toBeVisible();
  await userEvent.click(canvas.getByRole("button", { name: "Retry task" }));
  await expect(await canvas.findByRole("alert")).toBeVisible();
  await waitFor(() => expect(canvas.queryByRole("button", { name: "Retry task" })).not.toBeInTheDocument());
  await expect(eligibilityClient.retryJob).toHaveBeenCalledTimes(1);
} };

const historicalClient = interactiveScenario();
historicalClient.jobs.set(fixtureId(499), makeJob({ id: fixtureId(499), kind: "reindex-workspace", status: "dead", progress: { phase: "catching_up", processedItems: 5, totalItems: 5, updatedAt: "2026-10-28T09:00:00Z" } }));
historicalClient.retryJob = fn(historicalClient.retryJob);
export const UnlinkedHistoricalPreparation: Story = { args: { ...args, client: historicalClient, events: historicalClient.events, navigation: new ShellNavigation({ primary: "feed", screen: "tasks", selections: { tasks: fixtureId(499) } }) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await expect(await canvas.findByRole("heading", { name: "Prepare retrieval index", level: 1 })).toBeVisible();
  await expect(canvas.queryByText(/^Target model:/)).not.toBeInTheDocument();
  await expect(canvas.queryByRole("button", { name: "Retry task" })).not.toBeInTheDocument();
  await expect(within(canvas.getByRole("region", { name: "Reading pane" })).getByText("Failed")).toBeVisible();
  await expect(historicalClient.retryJob).not.toHaveBeenCalled();
} };

const committedClient = interactiveScenario();
const committedJob = makeJob({ id: fixtureId(480), kind: "generate-digest", queue: "generation", status: "dead" });
committedClient.jobs.set(committedJob.id, committedJob);
committedClient.retryJob = fn(async () => { throw new ApiError(409, null); });
committedClient.generateDigest = fn(committedClient.generateDigest);
committedClient.issues.unshift({ ...makeIssue(), id: fixtureId(580), title: "The already saved edition", generationJobId: committedJob.id });
export const CommittedIssueRetryConflict: Story = { args: { ...args, client: committedClient, events: committedClient.events, navigation: new ShellNavigation({ primary: "digests", screen: "digests", selections: { tasks: committedJob.id } }) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /Research notes for the week/ }));
  await userEvent.click(await canvas.findByRole("button", { name: "A closer look at retrieval systems · 1" }));
  await expect(await canvas.findByRole("heading", { name: "A closer look at retrieval systems · 1", level: 1 })).toBeVisible();
  await userEvent.click(canvas.getByRole("button", { name: "Tasks" }));
  await userEvent.click(await canvas.findByRole("button", { name: "Retry task" }));
  await userEvent.click(await canvas.findByRole("button", { name: "Open saved issue" }));
  await expect(await canvas.findByRole("heading", { name: "The already saved edition", level: 1 })).toBeVisible();
  await expect(committedClient.retryJob).toHaveBeenCalledTimes(1);
  await expect(committedClient.generateDigest).not.toHaveBeenCalled();
} };

const staleClient = interactiveScenario();
export const FailedReadRetainsTask: Story = { args: { ...args, client: staleClient, events: staleClient.events, navigation: new ShellNavigation({ primary: "feed", screen: "tasks", selections: { tasks: fixtureId(400) } }) }, parameters: { frame: "mobile" }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const pane = within(canvas.getByRole("region", { name: "Reading pane" }));
  await expect(await pane.findByText("Running")).toBeVisible();
  staleClient.getJob = fn(async () => { throw new Error("Synthetic status read failure"); });
  await userEvent.click(canvas.getByRole("button", { name: "Refresh selected task" }));
  await expect(await pane.findByRole("alert")).toHaveTextContent("Could not refresh this task");
  await expect(pane.getByText("Running")).toBeVisible();
} };

const noTasks = interactiveScenario(); noTasks.jobs.clear();
export const Empty: Story = { args: { ...args, client: noTasks, events: noTasks.events, navigation: new ShellNavigation({ primary: "feed", screen: "tasks" }) }, play: async ({ canvasElement }) => { await expect(await within(canvasElement).findByText("No tasks in this view.")).toBeVisible(); } };
const loadingTasks = Object.assign(interactiveScenario(), { listJobs: () => deferred<Schema<"JobList">>().promise });
export const Loading: Story = { args: { ...args, client: loadingTasks, events: loadingTasks.events, navigation: new ShellNavigation({ primary: "feed", screen: "tasks" }) } };
const taskErrorClient = Object.assign(interactiveScenario(), { listJobs: fn(async (): Promise<Schema<"JobList">> => { throw new Error("Synthetic task list failure"); }) });
export const ListErrorAndRecovery: Story = { args: { ...args, client: taskErrorClient, events: taskErrorClient.events, navigation: new ShellNavigation({ primary: "feed", screen: "tasks" }) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await expect(await canvas.findByRole("alert")).toHaveTextContent("Could not load tasks");
  taskErrorClient.listJobs = fn(async () => ({ items: [], nextCursor: null }));
  await userEvent.click(canvas.getByRole("button", { name: "Refresh tasks" }));
  await expect(await canvas.findByText("No tasks in this view.")).toBeVisible();
} };
export const Narrow: Story = { args: { ...args, navigation: new ShellNavigation({ primary: "feed", screen: "tasks" }) }, parameters: { frame: "narrow" } };
export const Mobile: Story = { args: { ...args, navigation: new ShellNavigation({ primary: "feed", screen: "tasks", selections: { tasks: fixtureId(402) } }) }, parameters: { frame: "mobile" } };
export const Light: Story = { ...Mobile, globals: { theme: "light" } };
export const LargeText: Story = { ...Mobile, parameters: { frame: "mobile", fontScale: 2 }, play: async ({ canvasElement }) => {
  const leaf = canvasElement.querySelector<HTMLElement>(".workspace-leaf")!;
  await expect(leaf.scrollWidth).toBeLessThanOrEqual(leaf.clientWidth);
} };

const sharedClient = interactiveScenario();
sharedClient.getJob = fn(sharedClient.getJob);
export const SharedSettingsTaskReads: Story = { args: { ...args, client: sharedClient, events: sharedClient.events, navigation: new ShellNavigation({ primary: "feed", screen: "settings", settingsSection: "workspace" }) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const taskLink = await canvas.findByRole("button", { name: "View model transition task" });
  sharedClient.jobs.set(fixtureId(400), makeJob({ status: "retry_wait" }));
  await userEvent.click(taskLink);
  await expect(await canvas.findByRole("heading", { name: "Prepare retrieval index", level: 1 })).toBeVisible();
  const pane = within(canvas.getByRole("region", { name: "Reading pane" }));
  await expect(await pane.findByText("Retry scheduled")).toBeVisible();
  await expect(sharedClient.getJob).toHaveBeenCalledTimes(2);
  sharedClient.jobs.set(fixtureId(400), makeJob({ status: "running" }));
  sharedClient.events.emit({ type: "jobChanged", jobId: fixtureId(400) });
  await expect(await pane.findByText("Running")).toBeVisible();
  await expect(sharedClient.getJob).toHaveBeenCalledTimes(3);
} };

const initialError = interactiveScenario();
initialError.getJob = fn(async (): Promise<Schema<"Job">> => { throw new Error("Synthetic task read failure"); });
export const DetailErrorRecovery: Story = { args: { ...args, client: initialError, events: initialError.events, navigation: new ShellNavigation({ primary: "feed", screen: "tasks", selections: { tasks: fixtureId(400) } }) }, parameters: { frame: "mobile" }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const pane = within(canvas.getByRole("region", { name: "Reading pane" }));
  await expect(await pane.findByRole("alert")).toHaveTextContent("Could not load this task");
  initialError.getJob = fn(async () => makeJob());
  await userEvent.click(pane.getByRole("button", { name: "Check task status" }));
  await expect(await pane.findByRole("heading", { name: "Prepare retrieval index", level: 1 })).toBeVisible();
} };

const cancelRace = interactiveScenario();
cancelRace.jobs.set(fixtureId(400), makeJob({ status: "succeeded", cancellationRequested: true }));
export const CompletionAfterCancelRequest: Story = { args: { ...args, client: cancelRace, events: cancelRace.events, navigation: new ShellNavigation({ primary: "feed", screen: "tasks", selections: { tasks: fixtureId(400) } }) }, play: async ({ canvasElement }) => {
  const pane = within(within(canvasElement).getByRole("region", { name: "Reading pane" }));
  await expect(await pane.findByText("Succeeded")).toBeVisible();
  await expect(pane.queryByText("Cancellation requested")).not.toBeInTheDocument();
  await expect(pane.queryByRole("button", { name: "Cancel task" })).not.toBeInTheDocument();
} };
