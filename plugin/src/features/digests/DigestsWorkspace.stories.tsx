import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent, waitFor, within } from "storybook/test";
import { ConnectedShell } from "../shell/ConnectedShell";
import { ShellNavigation } from "../shell/navigation";
import { interactiveScenario } from "../../test/storybook/interactive-scenario";
import { memoryWorkspaceState } from "../../test/storybook/local-state";
import { memoryFeedPreferences } from "../../test/storybook/feed-preferences";
import { makeIssue, makeEntry, makeSummary, fixtureId, makeDigestDefinition } from "../../test/storybook/fixtures";
import type { QueryParameters, Schema } from "../../api/contracts";
import { deferred } from "../../test/storybook/deferred";

const issue = makeIssue();
const listIssues = fn(async (_input: QueryParameters<"listDigestIssues">): Promise<Schema<"DigestIssueList">> => ({ items: [issue], nextCursor: null }));
const client = Object.assign(interactiveScenario(), { listDigestIssues: listIssues });
client.generateDigest = fn(client.generateDigest);
const args = { client, navigation: new ShellNavigation({ primary: "digests", screen: "digests" }), localState: memoryWorkspaceState(), notify: client.notify, feedPreferences: memoryFeedPreferences(), events: client.events };
const meta = { title: "Features/Digests/Views/DigestsWorkspace", component: ConnectedShell, args, parameters: { shell: true } } satisfies Meta<typeof ConnectedShell>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Loaded: Story = { play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await expect(await canvas.findByRole("button", { name: /Research notes for the week/ })).toBeVisible();
  await expect(listIssues).toHaveBeenCalledWith({ limit: 20 });
  await expect(client.generateDigest).not.toHaveBeenCalled();
  await expect(canvas.queryByText("This section is not available yet.")).not.toBeInTheDocument();
} };

const summaryClient = interactiveScenario();
summaryClient.issue = makeIssue([makeEntry(0, { summarySourceVersion: "saved-version" })]);
summaryClient.issues = [summaryClient.issue];
const captured = summaryClient.issue.sections[0]!.items[0]!;
captured.aiSummary = makeSummary({ sourceVersion: "saved-version" });
summaryClient.setReadingState = fn(async () => undefined);
summaryClient.getItemSummary = fn(summaryClient.getItemSummary);
summaryClient.requestDigestItemSummary = fn(async () => {
  captured.aiSummary = makeSummary({ state: "ready", sourceVersion: "saved-version" });
  return { jobId: fixtureId(401) };
});
export const CapturedSummaryAndExplanation: Story = { args: { ...args, client: summaryClient, events: summaryClient.events, navigation: new ShellNavigation({ primary: "digests", screen: "digests" }) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const body = within(canvasElement.ownerDocument.body);
  await userEvent.click(await canvas.findByRole("button", { name: /Research notes for the week/ }));
  await userEvent.click(await canvas.findByRole("button", { name: `More actions for ${captured.item.title}` }));
  await userEvent.click(await body.findByRole("menuitem", { name: "Why this item" }));
  await expect(await canvas.findByRole("heading", { name: "Why this item" })).toBeVisible();
  await userEvent.click(canvas.getByRole("button", { name: `More actions for ${captured.item.title}` }));
  await userEvent.click(await body.findByRole("menuitem", { name: "AI summary" }));
  await expect(await canvas.findByText("Synthetic AI summary fixture. This text is not provider evidence.")).toBeVisible();
  await expect(canvas.getByRole("heading", { name: "Research notes for the week", level: 1 })).toBeVisible();
  await expect(summaryClient.requestDigestItemSummary).toHaveBeenCalledTimes(1);
  await expect(summaryClient.requestDigestItemSummary).toHaveBeenCalledWith(summaryClient.issue.id, captured.item.id);
  await expect(summaryClient.setReadingState).not.toHaveBeenCalled();
  await expect(summaryClient.getItemSummary).not.toHaveBeenCalled();
} };

const buildClient = interactiveScenario();
buildClient.digests = [makeDigestDefinition()];
buildClient.generateDigest = fn(buildClient.generateDigest);
export const BuildManualDefinition: Story = { args: { ...args, client: buildClient, events: buildClient.events, navigation: new ShellNavigation({ primary: "digests", screen: "digests" }) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: "Build a Digest Issue" }));
  const body = within(canvasElement.ownerDocument.body);
  await userEvent.click(await body.findByRole("menuitem", { name: "Build Manual research · Manual only" }));
  await expect(await canvas.findByText("Build queued")).toBeVisible();
  await expect(buildClient.generateDigest).toHaveBeenCalledTimes(1);
  await expect(buildClient.generateDigest).toHaveBeenCalledWith(fixtureId(900), expect.stringMatching(/^[0-9a-f-]{36}$/));
  await expect(canvas.getByRole("button", { name: /Research notes for the week/ })).toBeVisible();
  await expect(await canvas.findByRole("button", { name: "View build task for Manual research" })).toBeVisible();
  const job = [...buildClient.jobs.values()].find((value) => value.kind === "generate-digest")!;
  buildClient.jobs.set(job.id, { ...job, status: "succeeded" });
  const result = { ...makeIssue([]), id: fixtureId(560), digestId: fixtureId(900), generationJobId: job.id, title: "A freshly built edition", generatedAt: "2026-10-28T10:00:00Z" };
  buildClient.issues.unshift(result);
  buildClient.events.emit({ type: "jobChanged", jobId: job.id });
  buildClient.events.emit({ type: "digestReady", issueId: result.id });
  await expect(await canvas.findByText("Build complete")).toBeVisible();
  await expect(canvas.queryByRole("heading", { name: result.title, level: 1 })).not.toBeInTheDocument();
  await userEvent.click(await canvas.findByRole("button", { name: "Open built issue for Manual research" }));
  await expect(await canvas.findByRole("heading", { name: result.title, level: 1 })).toBeVisible();
} };

const readinessClient = interactiveScenario();
const readinessLocal = memoryWorkspaceState();
export const LiveReadiness: Story = { args: { ...args, client: readinessClient, notify: readinessClient.notify, events: readinessClient.events, localState: readinessLocal, navigation: new ShellNavigation({ primary: "digests", screen: "digests" }) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await waitFor(async () => { await expect((await readinessLocal.read()).digest.baseline).not.toBeNull(); });
  const issue = { ...makeIssue([]), id: fixtureId(550), title: "A new saved edition", generatedAt: "2026-10-28T09:00:00Z" };
  readinessClient.issues.unshift(issue);
  readinessClient.events.emit({ type: "digestReady", issueId: issue.id });
  readinessClient.events.emit({ type: "digestReady", issueId: issue.id });
  await expect(await canvas.findByLabelText("1 unseen issue")).toBeVisible();
  await expect(await canvas.findByRole("button", { name: /A new saved edition.*New/ })).toBeVisible();
  await waitFor(() => expect(readinessClient.notices).toEqual(["Digest ready: A new saved edition"]));
  await userEvent.click(canvas.getByRole("button", { name: /A new saved edition/ }));
  await expect(await canvas.findByRole("heading", { name: "A new saved edition", level: 1 })).toBeVisible();
  await waitFor(() => expect(canvas.queryByLabelText("1 unseen issue")).not.toBeInTheDocument());
  await expect((await readinessLocal.read()).digest.seenIssueIds).toContain(issue.id);
  const offline = { ...issue, id: fixtureId(551), title: "Saved while disconnected", generatedAt: "2026-10-28T10:00:00Z" };
  readinessClient.issues.unshift(offline);
  readinessClient.events.emit({ type: "reconnected" });
  await expect(await canvas.findByLabelText("1 unseen issue")).toBeVisible();
  await expect(await canvas.findByRole("button", { name: /Saved while disconnected.*New/ })).toBeVisible();
  readinessClient.issues.unshift({ ...offline, id: fixtureId(552), title: "Another saved issue", generatedAt: "2026-10-28T11:00:00Z" });
  await userEvent.click(canvas.getByRole("button", { name: "Refresh Digest archive" }));
  await expect(await canvas.findByLabelText("2 unseen issues")).toBeVisible();
  await expect(readinessClient.notices).toEqual(["Digest ready: A new saved edition"]);
} };

export const ManageDefinitions: Story = { args: { ...args, navigation: new ShellNavigation({ primary: "digests", screen: "digests" }) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(canvas.getByRole("button", { name: "Digest archive actions" }));
  await userEvent.click(await within(canvasElement.ownerDocument.body).findByRole("menuitem", { name: "Manage definitions" }));
  await expect(await canvas.findByRole("heading", { name: "Digest definitions" })).toBeVisible();
  await userEvent.click(canvas.getByRole("button", { name: "Back to reading" }));
  await expect(canvas.getByRole("heading", { name: "Digests" })).toBeVisible();
} };

const contentsClient = interactiveScenario();
contentsClient.setReadingState = fn(async () => undefined);
export const ContentsNavigation: Story = { args: { ...args, client: contentsClient, events: contentsClient.events, navigation: new ShellNavigation({ primary: "digests", screen: "digests" }) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /Research notes for the week/ }));
  await userEvent.click(await canvas.findByRole("button", { name: "A closer look at retrieval systems · 1" }));
  const contents = within(await canvas.findByRole("navigation", { name: "Issue contents" }));
  await userEvent.click(contents.getByRole("button", { name: "Notas de ingeniería sobre búsquedas · 2" }));
  await expect(await canvas.findByRole("heading", { name: "Notas de ingeniería sobre búsquedas · 2", level: 1 })).toBeVisible();
  await waitFor(() => expect(contentsClient.setReadingState).toHaveBeenCalledTimes(2));
  await userEvent.click(canvas.getByRole("button", { name: "Back to issue" }));
  await expect(canvas.getByRole("button", { name: "Notas de ingeniería sobre búsquedas · 2" })).toHaveFocus();
} };

const failedArchive = Object.assign(interactiveScenario(), { listDigestIssues: fn(async (): Promise<Schema<"DigestIssueList">> => { throw new Error("Synthetic archive read failure"); }) });
export const ArchiveErrorAndRecovery: Story = { args: { ...args, client: failedArchive, events: failedArchive.events, navigation: new ShellNavigation({ primary: "digests", screen: "digests" }) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await expect(await canvas.findByRole("alert")).toHaveTextContent("Could not load Digest archive");
  failedArchive.listDigestIssues = fn(async () => ({ items: [], nextCursor: null }));
  await userEvent.click(canvas.getByRole("button", { name: "Refresh Digest archive" }));
  await expect(await canvas.findByText("No saved Digest Issues yet.")).toBeVisible();
} };
const emptyArchive = interactiveScenario(); emptyArchive.issues = [];
export const Empty: Story = { args: { ...args, client: emptyArchive, events: emptyArchive.events, navigation: new ShellNavigation({ primary: "digests", screen: "digests" }) } };
const archivePending = deferred<Schema<"DigestIssueList">>();
const pendingArchive = Object.assign(interactiveScenario(), { listDigestIssues: fn(() => archivePending.promise) });
export const Loading: Story = { args: { ...args, client: pendingArchive, events: pendingArchive.events, navigation: new ShellNavigation({ primary: "digests", screen: "digests" }) } };

const badgeStorageClient = interactiveScenario();
const unavailableStorage = Object.assign(memoryWorkspaceState(), { read: async () => { throw new Error("Device storage unavailable"); } });
export const BadgeStorageFailure: Story = { args: { ...args, client: badgeStorageClient, events: badgeStorageClient.events, localState: unavailableStorage, navigation: new ShellNavigation({ primary: "digests", screen: "digests" }) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await expect(await canvas.findByRole("button", { name: /Research notes for the week/ })).toBeVisible();
  await expect(await canvas.findByRole("alert")).toHaveTextContent("Could not update new-issue badges");
} };

const olderClient = Object.assign(interactiveScenario(), { listDigestIssues: fn(async (input: QueryParameters<"listDigestIssues">): Promise<Schema<"DigestIssueList">> => {
  if (input.cursor) throw new Error("Synthetic older page failure");
  return { items: [makeIssue([])], nextCursor: "opaque+older=" };
}) });
export const OlderPageFailure: Story = { args: { ...args, client: olderClient, events: olderClient.events, navigation: new ShellNavigation({ primary: "digests", screen: "digests" }) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: "Load older issues" }));
  await expect(await canvas.findByRole("alert")).toHaveTextContent("Could not load older issues");
  await expect(canvas.getByRole("button", { name: /Research notes for the week/ })).toBeVisible();
  await expect(olderClient.listDigestIssues).toHaveBeenCalledWith({ limit: 20, cursor: "opaque+older=" });
} };

const partialClient = interactiveScenario();
partialClient.issue = { ...makeIssue([makeEntry(0, { title: "Keeping a useful reading practice when the collection grows beyond a single notebook", excerpt: "" }), makeEntry(1)]), title: "Notes for a slower reading week: systems, software craft, and questions to keep open", status: "degraded" };
partialClient.issue.sections[0]!.items[0]!.summary = "";
partialClient.issue.sections[1]!.topicName = "Notas de ingeniería para conservar preguntas y volver a pensar";
partialClient.issue.sections[2]!.failure = "A source was unavailable when this section was built.";
partialClient.issues = [partialClient.issue];
export const PartialIssue: Story = { args: { ...args, client: partialClient, events: partialClient.events, navigation: new ShellNavigation({ primary: "digests", screen: "digests", selections: { digests: partialClient.issue.id } }) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await expect(await canvas.findByRole("heading", { name: partialClient.issue.title, level: 1 })).toBeVisible();
  await expect(canvas.getByText("No excerpt was captured for this Item.")).toBeVisible();
  await expect(canvas.getByText(/This section is incomplete/)).toBeVisible();
} };
export const LightEdition: Story = { ...PartialIssue, globals: { theme: "light" } };
export const NarrowEdition: Story = { ...PartialIssue, parameters: { frame: "narrow" } };
export const MobileEdition: Story = { ...PartialIssue, parameters: { frame: "mobile" } };
export const LargeTextEdition: Story = { ...PartialIssue, parameters: { frame: "mobile", fontScale: 2 }, play: async ({ canvasElement }) => {
  const leaf = canvasElement.querySelector<HTMLElement>(".workspace-leaf")!;
  await expect(leaf.scrollWidth).toBeLessThanOrEqual(leaf.clientWidth);
} };

const filteredClient = interactiveScenario();
filteredClient.digests = [makeDigestDefinition()];
filteredClient.issues.unshift({ ...makeIssue([]), id: fixtureId(599), digestId: fixtureId(900), title: "An edition from the selected definition" });
filteredClient.listDigestIssues = fn(filteredClient.listDigestIssues);
export const DefinitionFilter: Story = { args: { ...args, client: filteredClient, events: filteredClient.events, navigation: new ShellNavigation({ primary: "digests", screen: "digests" }) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.selectOptions(await canvas.findByLabelText("Digest definition"), fixtureId(900));
  await expect(await canvas.findByRole("button", { name: /An edition from the selected definition/ })).toBeVisible();
  await expect(canvas.queryByRole("button", { name: /Research notes for the week/ })).not.toBeInTheDocument();
  await expect(filteredClient.listDigestIssues).toHaveBeenCalledWith({ limit: 20, digestId: fixtureId(900) });
} };

const failedSummaryClient = interactiveScenario();
failedSummaryClient.issue = makeIssue([makeEntry()]); failedSummaryClient.issues = [failedSummaryClient.issue];
const failedCapture = failedSummaryClient.issue.sections[0]!.items[0]!;
failedCapture.aiSummary = makeSummary({ state: "failed" });
failedSummaryClient.jobs.set(fixtureId(401), { ...makeJobForSummary(), id: fixtureId(401), status: "dead" });
failedSummaryClient.retryJob = fn(async () => { failedCapture.aiSummary = makeSummary({ state: "pending", jobId: fixtureId(405) }); return { jobId: fixtureId(405) }; });
failedSummaryClient.requestDigestItemSummary = fn(failedSummaryClient.requestDigestItemSummary);
failedSummaryClient.setReadingState = fn(async () => undefined);
export const FailedCapturedSummaryAndLateFill: Story = { args: { ...args, client: failedSummaryClient, events: failedSummaryClient.events, navigation: new ShellNavigation({ primary: "digests", screen: "digests", selections: { digests: failedSummaryClient.issue.id } }) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: `More actions for ${failedCapture.item.title}` }));
  await userEvent.click(await within(canvasElement.ownerDocument.body).findByRole("menuitem", { name: "AI summary" }));
  await expect(await canvas.findByRole("alert")).toHaveTextContent("Synthetic provider failure");
  await expect(failedSummaryClient.retryJob).not.toHaveBeenCalled();
  await userEvent.click(canvas.getByRole("button", { name: "Retry AI summary" }));
  await expect(await canvas.findByText("Summary is being prepared.")).toBeVisible();
  failedCapture.aiSummary = makeSummary({ state: "ready", jobId: fixtureId(405), canGenerate: false, unavailableReason: "generation_disabled" });
  failedSummaryClient.events.emit({ type: "jobChanged", jobId: fixtureId(405) });
  await expect(await canvas.findByText("Synthetic AI summary fixture. This text is not provider evidence.")).toBeVisible();
  await expect(failedSummaryClient.retryJob).toHaveBeenCalledTimes(1);
  await expect(failedSummaryClient.requestDigestItemSummary).not.toHaveBeenCalled();
  await expect(failedSummaryClient.setReadingState).not.toHaveBeenCalled();
  await expect(canvas.getByRole("heading", { name: failedSummaryClient.issue.title, level: 1 })).toBeVisible();
} };
function makeJobForSummary(): Schema<"Job"> { return { id: fixtureId(401), kind: "summarize-item", queue: "generation", status: "dead", attempt: 1, maxAttempts: 5, cancellationRequested: false, createdAt: "2026-10-27T09:00:00Z", updatedAt: "2026-10-27T09:00:00Z" }; }

const definitionErrorClient = Object.assign(interactiveScenario(), { listDigests: fn(async (): Promise<Schema<"DigestDefinition">[]> => { throw new Error("Synthetic definition read failure"); }) });
export const DefinitionReadFailure: Story = { args: { ...args, client: definitionErrorClient, events: definitionErrorClient.events, navigation: new ShellNavigation({ primary: "digests", screen: "digests" }) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await expect(await canvas.findByRole("alert")).toHaveTextContent("Could not load Digest definitions");
  await expect(canvas.getByRole("button", { name: /Research notes for the week/ })).toBeVisible();
  definitionErrorClient.listDigests = fn(async () => [makeDigestDefinition()]);
  await userEvent.click(canvas.getByRole("button", { name: "Refresh Digest archive" }));
  await waitFor(() => expect(canvas.getByRole("button", { name: "Build a Digest Issue" })).toBeEnabled());
} };

const editionClient = Object.assign(interactiveScenario(), { setReadingState: fn(async () => undefined), requestItemSummary: fn(async () => ({ jobId: "never-automatic" })) });
editionClient.getDigestIssue = fn(editionClient.getDigestIssue);
export const WholeIssueToReaderAndBack: Story = { args: { ...args, client: editionClient, navigation: new ShellNavigation({ primary: "digests", screen: "digests" }), events: editionClient.events }, parameters: { frame: "mobile" }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /Research notes for the week/ }));
  await expect(await canvas.findByRole("heading", { name: "Research notes for the week", level: 1 })).toBeVisible();
  await expect(editionClient.setReadingState).not.toHaveBeenCalled();
  const title = canvas.getByRole("button", { name: "A closer look at retrieval systems · 1" });
  await userEvent.click(title);
  await expect(await canvas.findByRole("heading", { name: "A closer look at retrieval systems · 1", level: 1 })).toBeVisible();
  await expect(await canvas.findByText("Synthetic article content for a deterministic UI fixture.")).toBeVisible();
  await expect(editionClient.setReadingState).toHaveBeenCalledTimes(1);
  await expect(editionClient.getDigestIssue).toHaveBeenCalledTimes(1);
  await expect(canvas.queryByRole("alert")).not.toBeInTheDocument();
  await userEvent.click(canvas.getByRole("button", { name: "Back to issue" }));
  await expect(canvas.getByRole("heading", { name: "Research notes for the week", level: 1 })).toBeVisible();
  await expect(canvas.getByRole("button", { name: "A closer look at retrieval systems · 1" })).toHaveFocus();
  await userEvent.click(canvas.getByRole("button", { name: "A closer look at retrieval systems · 1" }));
  await userEvent.click(canvas.getByRole("button", { name: "More actions for A closer look at retrieval systems · 1" }));
  await expect(await within(canvasElement.ownerDocument.body).findByRole("menuitem", { name: "Mark as unread" })).toBeVisible();
  await expect(editionClient.setReadingState).toHaveBeenCalledTimes(1);
  await userEvent.keyboard("{Escape}");
  await expect(editionClient.requestItemSummary).not.toHaveBeenCalled();
} };
