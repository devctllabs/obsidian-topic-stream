import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fireEvent, fn, userEvent, waitFor, within } from "storybook/test";
import type { QueryParameters, Schema } from "../../api/contracts";
import { ConnectedShell } from "../shell/ConnectedShell";
import { ShellNavigation } from "../shell/navigation";
import { interactiveScenario } from "../../test/storybook/interactive-scenario";
import { memoryWorkspaceState } from "../../test/storybook/local-state";
import { memoryFeedPreferences } from "../../test/storybook/feed-preferences";
import { fixtureId, makeEntry } from "../../test/storybook/fixtures";
import { httpError } from "../../api/errors";
import { deferred } from "../../test/storybook/deferred";

function searchArgs() {
  const client = interactiveScenario();
  return { client, navigation: new ShellNavigation({ primary: "search", screen: "search" }), localState: memoryWorkspaceState(), notify: client.notify, feedPreferences: memoryFeedPreferences() };
}
const archiveArgs = searchArgs();
const readArchive = fn(async (_request: QueryParameters<"searchItems">): Promise<Schema<"SearchPage">> => ({ items: Array.from({ length: 20 }, (_, index) => makeEntry(index)), nextCursor: "older-archive-page" }));
Object.assign(archiveArgs.client, { searchItems: readArchive });
const meta = { title: "Features/Search/Views/SearchWorkspace", component: ConnectedShell, args: archiveArgs, parameters: { shell: true } } satisfies Meta<typeof ConnectedShell>;
export default meta;
type Story = StoryObj<typeof meta>;

const correctedArgs = searchArgs();
correctedArgs.client.collections.entries = [makeEntry()];
const correctedSearch = fn(correctedArgs.client.searchItems);
const correctionWrite = fn(correctedArgs.client.setTopicCorrection);
correctedArgs.client.searchItems = correctedSearch;
correctedArgs.client.setTopicCorrection = correctionWrite;
export const CorrectionReconcilesSelectedTopic: Story = { args: correctedArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  await canvas.findByRole("button", { name: /^A closer look/ });
  await userEvent.click(canvas.getByRole("button", { name: "Filters" }));
  const filters = within(await document.findByRole("dialog", { name: "Search filters" }));
  await userEvent.selectOptions(filters.getByRole("combobox", { name: "Topic" }), fixtureId(200));
  await userEvent.click(filters.getByRole("button", { name: "Apply filters" }));
  const title = await canvas.findByRole("button", { name: /^A closer look/ });
  await userEvent.click(within(title.closest("li")!).getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(document.getByRole("menuitem", { name: /^Wrong topic:/ }));
  await expect(await canvas.findByRole("heading", { name: "No matching Items" })).toBeVisible();
  await expect(correctionWrite).toHaveBeenCalledTimes(1);
  await expect(correctedSearch).toHaveBeenLastCalledWith({ limit: 20, topicId: fixtureId(200) });
} };

const committedArgs = searchArgs();
committedArgs.client.collections.entries = [makeEntry(0, { contentMarkdown: "", contentOrigin: "empty", summarySupported: false })];
committedArgs.client.setFeedback = fn(async () => { throw httpError(503, JSON.stringify({ type: "https://topic-stream.dev/problems/projection-pending", status: 503, title: "Projection pending", code: "projection_pending", canonicalCommitted: true, projectionRevision: "saved-feedback-revision" })); });
committedArgs.client.setTopicCorrection = fn(async () => { throw httpError(503, JSON.stringify({ type: "https://topic-stream.dev/problems/projection-pending", status: 503, title: "Projection pending", code: "projection_pending", canonicalCommitted: true, projectionRevision: "saved-topic-revision" })); });
export const CommittedWritesWithEmptyReader: Story = { args: committedArgs, parameters: { frame: "mobile" }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  await userEvent.click(await canvas.findByRole("button", { name: /^A closer look/ }));
  const pane = within(canvas.getByRole("region", { name: "Reading pane" }));
  await expect(pane.getByText("Full content is unavailable for this Item.")).toBeVisible();
  await userEvent.click(pane.getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(document.getByRole("menuitemcheckbox", { name: "Useful" }));
  await userEvent.click(document.getByRole("button", { name: "Close note" }));
  await expect(pane.getByText("Saved. Retrieval update pending.")).toBeVisible();
  await userEvent.click(pane.getByRole("button", { name: /^More actions for/ }));
  await expect(document.getByRole("menuitemcheckbox", { name: "Useful" })).toBeChecked();
  await userEvent.click(document.getByRole("menuitem", { name: "Add to Topic…" }));
  await userEvent.click(within(await document.findByRole("dialog", { name: "Add to Topic" })).getByRole("option", { name: "Retrieval 2" }));
  await expect(await pane.findByText("Topic correction saved. Retrieval update pending.")).toBeVisible();
  await expect(pane.getByText("Full content is unavailable for this Item.")).toBeVisible();
  await expect(committedArgs.client.setFeedback).toHaveBeenCalledTimes(1);
  await expect(committedArgs.client.setTopicCorrection).toHaveBeenCalledTimes(1);
  await expect(committedArgs.client.setFeedback).toHaveBeenCalledWith(fixtureId(300), "like", null);
} };

export const Loaded: Story = { play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const results = within(await canvas.findByRole("list", { name: "Search results" }));
  await expect(results.getAllByRole("listitem")).toHaveLength(20);
  await expect(readArchive).toHaveBeenCalledWith({ limit: 20 });
  await expect(canvas.getByRole("searchbox", { name: "Search the archive" })).toHaveValue("");
  await expect(canvas.getByRole("button", { name: "Load more results" })).toBeEnabled();
  const submit = canvasElement.querySelector<HTMLButtonElement>(".topic-stream-search-form button")!;
  const label = canvasElement.ownerDocument.createRange(); label.selectNodeContents(submit);
  await expect(label.getClientRects()).toHaveLength(1);
} };

const unavailableArgs = searchArgs();
const searchUnavailable = fn(async (_request: QueryParameters<"searchItems">): Promise<Schema<"SearchPage">> => { throw httpError(503, JSON.stringify({ type: "https://topic-stream.dev/problems/retrieval-unavailable", title: "Retrieval unavailable", status: 503, code: "retrieval_unavailable", detail: "Retrieval preparation is still running." })); });
unavailableArgs.client.searchItems = searchUnavailable;
export const RetrievalUnavailable: Story = { args: unavailableArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await expect(await canvas.findByRole("alert")).toHaveTextContent("Search is temporarily unavailable");
  await expect(canvas.queryByText(/offline/i)).not.toBeInTheDocument();
  await userEvent.click(canvas.getByRole("button", { name: "Open Settings" }));
  await expect(canvas.getByRole("heading", { name: "Settings" })).toBeVisible();
  await userEvent.click(canvas.getByRole("button", { name: "Back to reading" }));
  await userEvent.click(canvas.getByRole("button", { name: "Open Tasks" }));
  await expect(canvas.getByRole("heading", { name: "Tasks" })).toBeVisible();
  await userEvent.click(canvas.getByRole("button", { name: "Back to reading" }));
  searchUnavailable.mockResolvedValueOnce({ items: [makeEntry()], nextCursor: null });
  await userEvent.click(canvas.getByRole("button", { name: "Try again" }));
  await expect(await canvas.findByRole("button", { name: /^A closer look/ })).toBeVisible();
  await expect(searchUnavailable).toHaveBeenCalledTimes(2);
} };
export const Unavailable: Story = { ...RetrievalUnavailable, play: async ({ canvasElement }) => {
  await expect(await within(canvasElement).findByRole("alert")).toHaveTextContent("Search is temporarily unavailable");
} };

const loadingArgs = searchArgs();
const loadingResponse = deferred<Schema<"SearchPage">>();
loadingArgs.client.searchItems = () => loadingResponse.promise;
export const Loading: Story = { args: loadingArgs, play: async ({ canvasElement }) => {
  await expect(await within(canvasElement).findByRole("heading", { name: "Loading the archive" })).toBeVisible();
} };

const olderArgs = searchArgs();
const olderSearch = fn(olderArgs.client.searchItems);
olderArgs.client.searchItems = olderSearch;
export const OlderPageFailure: Story = { args: olderArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await canvas.findAllByRole("button", { name: /^A closer look/ });
  olderSearch.mockRejectedValueOnce(new Error("Older results are unavailable."));
  await userEvent.click(canvas.getByRole("button", { name: "Load more results" }));
  await expect(await canvas.findByRole("alert")).toHaveTextContent("Could not load more results");
  await expect(within(canvas.getByRole("list", { name: "Search results" })).getAllByRole("listitem")).toHaveLength(20);
  await userEvent.click(canvas.getByRole("button", { name: "Try again" }));
  await waitFor(() => expect(within(canvas.getByRole("list", { name: "Search results" })).getAllByRole("listitem")).toHaveLength(30));
  await expect(olderSearch).toHaveBeenLastCalledWith({ limit: 20, cursor: "20" });
} };

const capArgs = searchArgs();
capArgs.client.collections.entries = Array.from({ length: 140 }, (_, index) => makeEntry(index));
const pagedSearch = fn(capArgs.client.searchItems);
capArgs.client.searchItems = pagedSearch;
export const ResultCap: Story = { args: capArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const results = within(await canvas.findByRole("list", { name: "Search results" }));
  for (let page = 1; page < 5; page++) {
    await waitFor(() => expect(results.getAllByRole("listitem")).toHaveLength(page * 20));
    await userEvent.click(canvas.getByRole("button", { name: "Load more results" }));
  }
  await waitFor(() => expect(results.getAllByRole("listitem")).toHaveLength(100));
  await expect(pagedSearch).toHaveBeenCalledTimes(5);
  await expect(canvas.queryByRole("button", { name: "Load more results" })).not.toBeInTheDocument();
  await expect(canvas.getByText("Showing the first 100 results. Refine your search for more.")).toBeVisible();
  await userEvent.type(canvas.getByRole("searchbox", { name: "Search the archive" }), "Notas{Enter}");
  await waitFor(() => expect(results.getAllByRole("listitem")).toHaveLength(20));
  await expect(pagedSearch).toHaveBeenLastCalledWith({ q: "Notas", mode: "hybrid", limit: 20 });
  await expect(canvas.getByRole("button", { name: "Load more results" })).toBeEnabled();
  await userEvent.clear(canvas.getByRole("searchbox", { name: "Search the archive" }));
  await userEvent.keyboard("{Enter}");
  await waitFor(() => expect(results.getAllByRole("listitem")).toHaveLength(20));
  await expect(pagedSearch).toHaveBeenCalledTimes(7);
  await expect(pagedSearch).toHaveBeenLastCalledWith({ limit: 20 });
} };
export const ResultLimit: Story = { ...ResultCap, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const results = within(await canvas.findByRole("list", { name: "Search results" }));
  for (let page = 1; page < 5; page++) {
    await waitFor(() => expect(results.getAllByRole("listitem")).toHaveLength(page * 20));
    await userEvent.click(canvas.getByRole("button", { name: "Load more results" }));
  }
  await expect(await canvas.findByText("Showing the first 100 results. Refine your search for more.")).toBeVisible();
} };

const hiddenArgs = searchArgs();
hiddenArgs.client.collections.entries = [makeEntry(0, { visibility: "hidden", topicIds: [] }), makeEntry(1, { topicIds: [] })];
export const HiddenArchiveItems: Story = { args: hiddenArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const results = within(await canvas.findByRole("list", { name: "Search results" }));
  await waitFor(() => expect(results.getAllByRole("listitem")).toHaveLength(2));
  await expect(results.getByText("Hidden")).toBeVisible();
  await expect(canvas.queryByText(/rejected/i)).not.toBeInTheDocument();
  await userEvent.click(results.getAllByRole("button", { name: /^More actions for/ })[1]!);
  await expect(within(canvasElement.ownerDocument.body).getByRole("menuitem", { name: "Add to Topic…" })).toBeVisible();
} };

const emptyArgs = searchArgs();
emptyArgs.client.collections.entries = [];
const emptySearch = fn(emptyArgs.client.searchItems);
emptyArgs.client.searchItems = emptySearch;
export const EmptyArchiveAndNoResults: Story = { args: emptyArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await expect(await canvas.findByRole("heading", { name: "No saved Items yet" })).toBeVisible();
  await userEvent.type(canvas.getByRole("searchbox", { name: "Search the archive" }), "unmatched phrase{Enter}");
  await expect(await canvas.findByRole("heading", { name: "No matching Items" })).toBeVisible();
  await expect(emptySearch).toHaveBeenLastCalledWith({ q: "unmatched phrase", mode: "hybrid", limit: 20 });
} };

const filteredArgs = searchArgs();
filteredArgs.client.collections.entries = Array.from({ length: 140 }, (_, index) => makeEntry(index));
const filteredSearch = fn(filteredArgs.client.searchItems);
filteredArgs.client.searchItems = filteredSearch;
export const FiltersStartNewSession: Story = { args: filteredArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  await canvas.findAllByRole("button", { name: /^A closer look/ });
  await userEvent.type(canvas.getByRole("searchbox", { name: "Search the archive" }), "Notas{Enter}");
  await waitFor(() => expect(filteredSearch).toHaveBeenLastCalledWith({ q: "Notas", mode: "hybrid", limit: 20 }));
  await userEvent.click(canvas.getByRole("button", { name: "Load more results" }));
  await waitFor(() => expect(within(canvas.getByRole("list", { name: "Search results" })).getAllByRole("listitem")).toHaveLength(40));
  await userEvent.click(canvas.getByRole("button", { name: "Filters" }));
  const panel = within(await document.findByRole("dialog", { name: "Search filters" }));
  canvasElement.querySelector(".topic-stream-list-pane")!.dispatchEvent(new Event("scroll"));
  await expect(document.getByRole("dialog", { name: "Search filters" })).toBeVisible();
  await panel.findByRole("option", { name: "Retrieval 2" });
  await panel.findByRole("option", { name: "Source 2" });
  await userEvent.selectOptions(panel.getByRole("combobox", { name: "Topic" }), fixtureId(201));
  await userEvent.selectOptions(panel.getByRole("combobox", { name: "Source" }), fixtureId(101));
  await fireEvent.change(panel.getByLabelText("Published from"), { target: { value: "2026-10-01" } });
  await fireEvent.change(panel.getByLabelText("Published through"), { target: { value: "2026-10-28" } });
  await expect(filteredSearch).toHaveBeenCalledTimes(3);
  await userEvent.click(panel.getByRole("button", { name: "Apply filters" }));
  await waitFor(() => expect(filteredSearch).toHaveBeenLastCalledWith({ q: "Notas", mode: "hybrid", limit: 20, topicId: fixtureId(201), sourceId: fixtureId(101), publishedAfter: "2026-10-01T00:00:00Z", publishedBefore: "2026-10-28T23:59:59.999999999Z" }));
  await expect(document.queryByRole("dialog", { name: "Search filters" })).not.toBeInTheDocument();
  await expect(canvas.getByRole("button", { name: "Filters" })).toHaveFocus();
  await userEvent.click(canvas.getByRole("button", { name: "Filters" }));
  const reset = within(await document.findByRole("dialog", { name: "Search filters" }));
  await userEvent.click(reset.getByRole("button", { name: "Clear filters" }));
  await userEvent.click(reset.getByRole("button", { name: "Apply filters" }));
  await waitFor(() => expect(filteredSearch).toHaveBeenLastCalledWith({ q: "Notas", mode: "hybrid", limit: 20 }));
  await waitFor(() => expect(within(canvas.getByRole("list", { name: "Search results" })).getAllByRole("listitem")).toHaveLength(20));
} };

const validationArgs = searchArgs();
const validationSearch = fn(validationArgs.client.searchItems);
validationArgs.client.searchItems = validationSearch;
export const FilterValidationAndCancel: Story = { args: validationArgs, parameters: { frame: "mobile" }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  await canvas.findAllByRole("button", { name: /^A closer look/ });
  await userEvent.click(canvas.getByRole("button", { name: "Filters" }));
  const dialog = await document.findByRole("dialog", { name: "Search filters" });
  const panel = within(dialog);
  await panel.findByRole("option", { name: "Source 2" });
  await fireEvent.change(panel.getByLabelText("Published from"), { target: { value: "2026-10-28" } });
  await fireEvent.change(panel.getByLabelText("Published through"), { target: { value: "2026-10-01" } });
  await userEvent.click(panel.getByRole("button", { name: "Apply filters" }));
  await expect(await panel.findByRole("alert")).toHaveTextContent("Choose an end date on or after the start date.");
  await expect(panel.getByLabelText("Published from")).toHaveValue("2026-10-28");
  await expect(validationSearch).toHaveBeenCalledTimes(1);
  await userEvent.keyboard("{Escape}");
  await expect(dialog).not.toBeInTheDocument();
  await expect(canvas.getByRole("button", { name: "Filters" })).toHaveFocus();
  await userEvent.click(canvas.getByRole("button", { name: "Filters" }));
  const reopened = within(await document.findByRole("dialog", { name: "Search filters" }));
  await expect(reopened.getByLabelText("Published from")).toHaveValue("");
  await fireEvent.change(reopened.getByLabelText("Published from"), { target: { value: "2026-10-28" } });
  await fireEvent.change(reopened.getByLabelText("Published through"), { target: { value: "2026-10-01" } });
  await userEvent.click(reopened.getByRole("button", { name: "Apply filters" }));
  await expect(await reopened.findByRole("alert")).toHaveTextContent("Choose an end date on or after the start date.");
} };

const filterErrorArgs = searchArgs();
const filterSources = fn(filterErrorArgs.client.listSources);
filterErrorArgs.client.listSources = filterSources;
export const FilterOptionsError: Story = { args: filterErrorArgs, beforeEach: () => { filterSources.mockRejectedValueOnce(new Error("Filter Sources are unavailable.")); }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await canvas.findAllByRole("button", { name: /^A closer look/ });
  await userEvent.click(canvas.getByRole("button", { name: "Filters" }));
  const panel = within(await within(canvasElement.ownerDocument.body).findByRole("dialog", { name: "Search filters" }));
  await expect(await panel.findByRole("alert")).toHaveTextContent("Could not load filter options");
  await userEvent.click(panel.getByRole("button", { name: "Try again" }));
  await expect(await panel.findByRole("option", { name: "Source 2" })).toBeInTheDocument();
  await expect(panel.queryByRole("alert")).not.toBeInTheDocument();
} };

const sharedReadingArgs = searchArgs();
const removedSourceArgs = searchArgs();
const removedSources = fn(removedSourceArgs.client.listSources);
removedSourceArgs.client.listSources = removedSources;
export const RetainsUnavailableFilter: Story = { args: removedSourceArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  await canvas.findAllByRole("button", { name: /^A closer look/ });
  await userEvent.click(canvas.getByRole("button", { name: "Filters" }));
  const first = within(await document.findByRole("dialog", { name: "Search filters" }));
  await userEvent.selectOptions(first.getByRole("combobox", { name: "Source" }), fixtureId(100));
  await userEvent.click(first.getByRole("button", { name: "Apply filters" }));
  removedSources.mockResolvedValue([]);
  await userEvent.click(canvas.getByRole("button", { name: "Filters" }));
  const second = within(await document.findByRole("dialog", { name: "Search filters" }));
  await expect(await second.findByRole("option", { name: "Selected Source is unavailable" })).toBeInTheDocument();
  await expect(second.getByRole("combobox", { name: "Source" })).toHaveValue(fixtureId(100));
} };

sharedReadingArgs.navigation = new ShellNavigation();
sharedReadingArgs.feedPreferences = memoryFeedPreferences("unread");
sharedReadingArgs.client.setReadingState = fn(sharedReadingArgs.client.setReadingState);
export const ReadingAcrossFeedAndSearch: Story = { args: sharedReadingArgs, parameters: { frame: "mobile" }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await canvas.findAllByRole("button", { name: /^A closer look/ });
  await userEvent.click(canvas.getByRole("tab", { name: "Search" }));
  const title = await canvas.findByRole("button", { name: /^A closer look at retrieval systems · 4 / });
  const list = canvasElement.querySelectorAll<HTMLElement>(".topic-stream-list-pane")[1]!;
  list.scrollTop = 400; list.dispatchEvent(new Event("scroll"));
  await userEvent.click(title);
  await expect(canvas.getByRole("heading", { name: "A closer look at retrieval systems · 4" })).toBeVisible();
  await userEvent.click(canvas.getByRole("button", { name: "Back to list" }));
  await expect(title).toHaveFocus();
  await expect(list.scrollTop).toBe(400);
  await expect(title).toHaveAttribute("data-reading-state", "read");
  await userEvent.click(canvas.getByRole("tab", { name: "Feed" }));
  const feedTitle = canvas.getByRole("button", { name: /^A closer look at retrieval systems · 4 / });
  await expect(feedTitle).toHaveAttribute("data-reading-state", "read");
  await expect(feedTitle).toBeInTheDocument();
  await expect(sharedReadingArgs.client.setReadingState).toHaveBeenCalledTimes(1);
  await userEvent.click(canvas.getByRole("button", { name: "Refresh feed" }));
  await waitFor(() => expect(canvas.queryByRole("button", { name: /^A closer look at retrieval systems · 4 / })).not.toBeInTheDocument());
} };

const detailsArgs = searchArgs();
detailsArgs.client.setReadingState = fn(detailsArgs.client.setReadingState);
export const DetailsHaveSharedItemActions: Story = { args: detailsArgs, parameters: { frame: "mobile" }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  const title = await canvas.findByRole("button", { name: /^A closer look at retrieval systems · 1 / });
  await userEvent.click(within(title.closest("li")!).getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(document.getByRole("menuitem", { name: "Why this item" }));
  await expect(canvas.getByRole("heading", { name: "Why this item" })).toBeVisible();
  await expect(detailsArgs.client.setReadingState).not.toHaveBeenCalled();
  const pane = within(canvas.getByRole("region", { name: "Reading pane" }));
  await userEvent.click(pane.getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(document.getByRole("menuitem", { name: "Mark as read" }));
  await userEvent.click(canvas.getByRole("button", { name: "Back to list" }));
  await expect(title).toHaveAttribute("data-reading-state", "read");
  await expect(title).toHaveFocus();
  await expect(detailsArgs.client.setReadingState).toHaveBeenCalledTimes(1);
} };
