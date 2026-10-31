import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent, waitFor, within } from "storybook/test";
import { ConnectedShell } from "../shell/ConnectedShell";
import { ShellNavigation } from "../shell/navigation";
import { interactiveScenario } from "../../test/storybook/interactive-scenario";
import { memoryWorkspaceState } from "../../test/storybook/local-state";
import { memoryFeedPreferences } from "../../test/storybook/feed-preferences";
import { fixtureId, makeEntry, makeJob, makeSummary } from "../../test/storybook/fixtures";
import { deferred } from "../../test/storybook/deferred";

const accepted = deferred<{ jobId: string }>();
const request = fn(async (_id: string) => accepted.promise);
const state = makeSummary();
const read = fn(async (_id: string, _version: string) => state);
const client = Object.assign(interactiveScenario(), { getItemSummary: read, requestItemSummary: request });
client.collections.entries = [makeEntry(0, { contentMarkdown: "The original article remains readable while a summary is prepared." })];
const args = { client, navigation: new ShellNavigation({ primary: "search", screen: "search" }), localState: memoryWorkspaceState(), notify: client.notify, feedPreferences: memoryFeedPreferences(), events: client.events };
const meta = { title: "Features/Reader/Views/ItemSummary", component: ConnectedShell, args, parameters: { shell: true } } satisfies Meta<typeof ConnectedShell>;
export default meta;
type Story = StoryObj<typeof meta>;

export const ExplicitPendingAndReady: Story = { play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /^A closer look/ }));
  const pane = within(canvas.getByRole("region", { name: "Reading pane" }));
  const button = await pane.findByRole("button", { name: "AI summary" });
  await expect(request).not.toHaveBeenCalled();
  await userEvent.click(button);
  await expect(pane.getByRole("group", { name: "AI summary" })).toHaveAttribute("aria-busy", "true");
  await userEvent.click(button);
  await expect(request).toHaveBeenCalledTimes(1);
  await expect(pane.getByText("The original article remains readable while a summary is prepared.")).toBeVisible();
  read.mockResolvedValue(makeSummary({ state: "pending", jobId: fixtureId(400) }));
  accepted.resolve({ jobId: fixtureId(400) });
  await waitFor(() => expect(read.mock.calls.length).toBeGreaterThan(1));
  read.mockResolvedValue(makeSummary({ state: "ready" }));
  button.focus();
  client.events.emit({ type: "jobChanged", jobId: fixtureId(400) });
  await expect(await pane.findByText("Synthetic AI summary fixture. This text is not provider evidence.")).toBeVisible();
  await expect(button).toHaveFocus();
  const results = within(canvas.getByRole("list", { name: "Search results" }));
  await expect(results.queryByRole("group", { name: "AI summary" })).not.toBeInTheDocument();
  await expect(request).toHaveBeenCalledTimes(1);
} };

const failedRead = fn(async (_id: string, _version: string) => makeSummary({ state: "failed" }));
const retry = fn(async (_id: string) => ({ jobId: fixtureId(402) }));
const failedPost = fn(async (_id: string) => ({ jobId: fixtureId(401) }));
const failedClient = Object.assign(interactiveScenario(), { getItemSummary: failedRead, requestItemSummary: failedPost, getJob: async (id: string) => makeJob({ id, kind: "summarize-item", queue: "generation", status: "dead" }), retryJob: retry });
failedClient.collections.entries = structuredClone(client.collections.entries);
export const FailedSummaryRetry: Story = { args: { ...args, client: failedClient, navigation: new ShellNavigation({ primary: "search", screen: "search" }), events: failedClient.events }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /^A closer look/ }));
  const pane = within(canvas.getByRole("region", { name: "Reading pane" }));
  await userEvent.click(await pane.findByRole("button", { name: "AI summary" }));
  await expect(await pane.findByRole("alert")).toHaveTextContent("Synthetic provider failure");
  await expect(retry).not.toHaveBeenCalled();
  failedRead.mockResolvedValueOnce(makeSummary({ state: "failed" })).mockResolvedValue(makeSummary({ state: "ready" }));
  await userEvent.click(pane.getByRole("button", { name: "Retry AI summary" }));
  await expect(await pane.findByText("Synthetic AI summary fixture. This text is not provider evidence.")).toBeVisible();
  await expect(retry).toHaveBeenCalledTimes(1);
  await expect(retry).toHaveBeenCalledWith(fixtureId(401));
  await expect(failedPost).not.toHaveBeenCalled();
} };
export const GenerationFailed: Story = { ...FailedSummaryRetry, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /^A closer look/ }));
  const pane = within(canvas.getByRole("region", { name: "Reading pane" }));
  await userEvent.click(await pane.findByRole("button", { name: "AI summary" }));
  await expect(await pane.findByRole("alert")).toHaveTextContent("Synthetic provider failure");
  await expect(retry).not.toHaveBeenCalled();
} };

const readyClient = Object.assign(interactiveScenario(), { getItemSummary: fn(async (_id: string, _version: string) => makeSummary({ state: "ready", canGenerate: false, unavailableReason: "historical_input" })), requestItemSummary: fn(async (_id: string) => ({ jobId: fixtureId(401) })) });
readyClient.collections.entries = [makeEntry(0, { contentMarkdown: "", summarySupported: false, contentOrigin: "empty" })];
export const ReadyWithoutGenerationOrContent: Story = { args: { ...args, client: readyClient, navigation: new ShellNavigation({ primary: "search", screen: "search" }), events: readyClient.events }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /^A closer look/ }));
  const pane = within(canvas.getByRole("region", { name: "Reading pane" }));
  await userEvent.click(await pane.findByRole("button", { name: "AI summary" }));
  await expect(await pane.findByText("Synthetic AI summary fixture. This text is not provider evidence.")).toBeVisible();
  await expect(pane.getByText("Full content is unavailable for this Item.")).toBeVisible();
  await expect(pane.getByRole("heading", { name: "AI summary" }).getBoundingClientRect().top).toBeGreaterThanOrEqual(pane.getByRole("button", { name: "AI summary" }).getBoundingClientRect().bottom);
  await expect(pane.queryByRole("button", { name: "Retry AI summary" })).not.toBeInTheDocument();
  await expect(readyClient.requestItemSummary).not.toHaveBeenCalled();
} };

const readError = fn(async (_id: string, _version: string) => makeSummary());
const readErrorClient = Object.assign(interactiveScenario(), { getItemSummary: readError, requestItemSummary: fn(async (_id: string) => ({ jobId: fixtureId(401) })) });
readErrorClient.collections.entries = structuredClone(client.collections.entries);
export const StatusReadError: Story = { args: { ...args, client: readErrorClient, navigation: new ShellNavigation({ primary: "search", screen: "search" }), events: readErrorClient.events }, beforeEach: () => { readError.mockRejectedValueOnce(new Error("Summary status is temporarily unavailable.")); }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /^A closer look/ }));
  const pane = within(canvas.getByRole("region", { name: "Reading pane" }));
  await expect(await pane.findByRole("alert")).toHaveTextContent("Could not read summary status");
  await expect(pane.getByText("The original article remains readable while a summary is prepared.")).toBeVisible();
  await userEvent.click(pane.getByRole("button", { name: "Check summary status" }));
  await expect(await pane.findByRole("button", { name: "AI summary" })).toBeVisible();
  await expect(readErrorClient.requestItemSummary).not.toHaveBeenCalled();
} };
export const StatusUnavailable: Story = { ...StatusReadError, beforeEach: () => { readError.mockRejectedValue(new Error("Summary status is temporarily unavailable.")); }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /^A closer look/ }));
  const pane = within(canvas.getByRole("region", { name: "Reading pane" }));
  await expect(await pane.findByRole("alert")).toHaveTextContent("Could not read summary status");
  await expect(pane.getByText("The original article remains readable while a summary is prepared.")).toBeVisible();
} };

const arrivalRead = fn(async (_id: string, _version: string) => makeSummary({ state: "pending" }));
const arrivalClient = Object.assign(interactiveScenario(), { getItemSummary: arrivalRead, requestItemSummary: fn(async (_id: string) => ({ jobId: fixtureId(401) })), setReadingState: fn(async () => undefined) });
arrivalClient.collections.entries = [makeEntry(0, { contentMarkdown: Array.from({ length: 40 }, (_, index) => `Paragraph ${index + 1}. Read the source in context and preserve the question you want to revisit.`).join("\n\n") })];
export const Pending: Story = { args: { ...args, client: arrivalClient, navigation: new ShellNavigation({ primary: "search", screen: "search" }), events: arrivalClient.events }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /^A closer look/ }));
  const pane = within(canvas.getByRole("region", { name: "Reading pane" }));
  await userEvent.click(await pane.findByRole("button", { name: "AI summary" }));
  await expect(pane.getByText("Summary is being prepared.")).toBeVisible();
  await expect(arrivalClient.requestItemSummary).not.toHaveBeenCalled();
} };
export const ArrivalPreservesReadingPosition: Story = { args: { ...args, client: arrivalClient, navigation: new ShellNavigation({ primary: "search", screen: "search" }), events: arrivalClient.events }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /^A closer look/ }));
  const pane = within(canvas.getByRole("region", { name: "Reading pane" }));
  const button = await pane.findByRole("button", { name: "AI summary" });
  await userEvent.click(button);
  const paragraph = await pane.findByText(/^Paragraph 20\./);
  paragraph.scrollIntoView({ block: "center" });
  button.focus({ preventScroll: true });
  const position = paragraph.getBoundingClientRect().top;
  arrivalRead.mockResolvedValue(makeSummary({ state: "ready" }));
  arrivalClient.events.emit({ type: "jobChanged", jobId: fixtureId(401) });
  await waitFor(() => expect(pane.getByText("Synthetic AI summary fixture. This text is not provider evidence.")).toBeInTheDocument());
  await expect(button).toHaveFocus();
  await expect(Math.abs(paragraph.getBoundingClientRect().top - position)).toBeLessThanOrEqual(1);
  await expect(arrivalClient.setReadingState).toHaveBeenCalledTimes(1);
  await expect(arrivalClient.requestItemSummary).not.toHaveBeenCalled();
} };

const versionClient = Object.assign(interactiveScenario(), { getItemSummary: fn(async (_id: string, version: string) => makeSummary({ state: "ready", sourceVersion: version, result: { ...makeSummary({ state: "ready", sourceVersion: version }).result!, text: `Annotation for ${version}.` } })), requestItemSummary: fn(async () => ({ jobId: fixtureId(401) })) });
versionClient.collections.entries = [makeEntry(0, { summarySourceVersion: "first-version" })];
export const SourceVersionChangeCollapses: Story = { args: { ...args, client: versionClient, navigation: new ShellNavigation(), events: versionClient.events }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /^A closer look/ }));
  const pane = within(canvas.getByRole("region", { name: "Reading pane" }));
  await userEvent.click(await pane.findByRole("button", { name: "AI summary" }));
  await expect(await pane.findByText("Annotation for first-version.")).toBeVisible();
  versionClient.collections.entries = [makeEntry(0, { summarySourceVersion: "second-version", contentMarkdown: "The updated canonical source text." })];
  await userEvent.click(canvas.getByRole("button", { name: "Refresh feed" }));
  await expect(await pane.findByText("The updated canonical source text.")).toBeVisible();
  await expect(await pane.findByRole("button", { name: "AI summary" })).toHaveAttribute("aria-expanded", "false");
  await expect(pane.queryByText("Annotation for second-version.")).not.toBeInTheDocument();
  await expect(versionClient.requestItemSummary).not.toHaveBeenCalled();
} };
