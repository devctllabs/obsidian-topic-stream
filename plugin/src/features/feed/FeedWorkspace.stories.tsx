import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent, waitFor, within } from "storybook/test";
import { ConnectedShell } from "../shell/ConnectedShell";
import { ShellNavigation } from "../shell/navigation";
import { interactiveScenario } from "../../test/storybook/interactive-scenario";
import { memoryWorkspaceState } from "../../test/storybook/local-state";
import { FIXTURE_TIME, fixtureId, makeEntry } from "../../test/storybook/fixtures";
import type { FeedPage, RequestBody } from "../../api/contracts";
import { deferred } from "../../test/storybook/deferred";
import type { FeedPageRequest } from "../../api/client";
import { memoryFeedPreferences } from "../../test/storybook/feed-preferences";
import { httpError } from "../../api/errors";

function feedArgs() {
  const client = interactiveScenario();
  return { client, navigation: new ShellNavigation(), localState: memoryWorkspaceState(), notify: client.notify, feedPreferences: memoryFeedPreferences(), events: client.events };
}
const meta = {
  title: "Features/Feed/Views/FeedWorkspace", component: ConnectedShell,
  args: feedArgs(), parameters: { shell: true }
} satisfies Meta<typeof ConnectedShell>;
export default meta;
type Story = StoryObj<typeof meta>;

export const ScrolledHeader: Story = { args: feedArgs(), parameters: { frame: "mobile" }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await canvas.findAllByRole("button", { name: /^A closer look/ });
  const document = canvasElement.ownerDocument;
  const header = canvasElement.querySelector<HTMLElement>(".topic-stream-feed-header")!;
  const list = canvasElement.querySelector<HTMLElement>(".topic-stream-list-pane")!;
  const filter = canvas.getByRole("group", { name: "Show items" });
  for (const button of within(filter).getAllByRole("button")) {
    const bounds = button.getBoundingClientRect();
    await expect(document.elementFromPoint(bounds.x + bounds.width / 2, bounds.top + 2)).toBe(button);
    await expect(document.elementFromPoint(bounds.x + bounds.width / 2, bounds.bottom - 2)).toBe(button);
  }
  await expect(document.defaultView!.getComputedStyle(header, "::before").backdropFilter).toBe("none");
  list.scrollTop = 420; list.dispatchEvent(new Event("scroll"));
  await waitFor(() => expect(document.defaultView!.getComputedStyle(header, "::before").backdropFilter).not.toBe("none"));
  await expect(canvas.getByRole("button", { name: "All Topics" })).toBeVisible();
  await expect(canvas.getByRole("button", { name: "Refresh feed" })).toBeEnabled();
  list.scrollTop = 0; list.dispatchEvent(new Event("scroll"));
  await expect(document.defaultView!.getComputedStyle(header, "::before").backdropFilter).toBe("none");
} };

export const TopicSwitching: Story = { args: feedArgs(), play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  await userEvent.click(await canvas.findByRole("button", { name: "All Topics" }));
  const picker = within(await document.findByRole("dialog", { name: "Choose feed Topic" }));
  await userEvent.type(picker.getByRole("searchbox", { name: "Find a Topic" }), "retrieval");
  await userEvent.click(picker.getByRole("option", { name: "Retrieval 2" }));
  await expect(await canvas.findByRole("button", { name: "Retrieval 2" })).toBeVisible();
  await expect(within(canvas.getByRole("list", { name: "Personalized news feed" })).getAllByRole("listitem")).toHaveLength(3);
  await userEvent.click(canvas.getByRole("button", { name: "Retrieval 2" }));
  await userEvent.click(within(await document.findByRole("dialog", { name: "Choose feed Topic" })).getByRole("option", { name: "All Topics" }));
  await expect(await canvas.findByRole("button", { name: "All Topics" })).toBeVisible();
  await expect(within(canvas.getByRole("list", { name: "Personalized news feed" })).getAllByRole("listitem")).toHaveLength(25);
} };

const publicationClient = interactiveScenario();
const publication = fn((input: RequestBody<"refreshFeed">) => publicationClient.getFeed({ limit: input.limit ?? 25, cursor: null, snapshot: null, ...(input.topicId ? { topicId: input.topicId } : {}) }));
const publicationArgs = { ...feedArgs(), client: Object.assign(publicationClient, { refreshFeed: publication }) };
export const FirstVisitOnce: Story = { args: publicationArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  await canvas.findAllByRole("button", { name: /^A closer look/ });
  await expect(publication).toHaveBeenCalledTimes(1);
  await expect(publication).toHaveBeenLastCalledWith({ limit: 25 });
  await userEvent.click(canvas.getByRole("button", { name: "All Topics" }));
  await userEvent.click(within(await document.findByRole("dialog", { name: "Choose feed Topic" })).getByRole("option", { name: "Retrieval 2" }));
  await canvas.findByRole("button", { name: /^Notas de ingeniería sobre búsquedas · 2 / });
  await expect(publication).toHaveBeenCalledTimes(2);
  await expect(publication).toHaveBeenLastCalledWith({ limit: 25, topicId: fixtureId(201) });
  await userEvent.click(canvas.getByRole("button", { name: "Retrieval 2" }));
  await userEvent.click(within(await document.findByRole("dialog", { name: "Choose feed Topic" })).getByRole("option", { name: "All Topics" }));
  await userEvent.click(canvas.getByRole("button", { name: "Settings" }));
  await userEvent.click(canvas.getByRole("button", { name: "Back to reading" }));
  canvasElement.ownerDocument.defaultView?.dispatchEvent(new Event("focus"));
  await expect(publication).toHaveBeenCalledTimes(2);
} };

const failedPublicationClient = interactiveScenario();
const savedRead = fn(failedPublicationClient.getFeed);
const failedPublication = fn(async () => { throw new Error("Publication response unavailable."); });
failedPublicationClient.getFeed = savedRead;
failedPublicationClient.refreshFeed = failedPublication;
export const FirstRefreshFailure: Story = { args: { ...feedArgs(), client: failedPublicationClient }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await expect((await canvas.findAllByRole("button", { name: /^A closer look/ }))[0]).toBeVisible();
  await expect(canvas.getByText("Refresh failed. Showing saved Feed.")).toBeVisible();
  await expect(failedPublication).toHaveBeenCalledTimes(1);
  await expect(savedRead).toHaveBeenCalledWith({ limit: 25, cursor: null, snapshot: null });
  await userEvent.click(canvas.getByRole("button", { name: "Settings" }));
  await userEvent.click(canvas.getByRole("button", { name: "Back to reading" }));
  canvasElement.ownerDocument.defaultView?.dispatchEvent(new Event("focus"));
  await expect(failedPublication).toHaveBeenCalledTimes(1);
} };

const oldPage: FeedPage = { items: Array.from({ length: 25 }, (_, index) => makeEntry(index)), nextCursor: null, snapshot: "old-opaque-head", generatedAt: FIXTURE_TIME };
const manualClient = interactiveScenario();
const manualPublication = fn(async (_input: RequestBody<"refreshFeed">): Promise<FeedPage> => oldPage);
manualClient.getFeed = async () => oldPage;
manualClient.refreshFeed = manualPublication;
export const ManualRefreshNewHead: Story = { args: { ...feedArgs(), client: manualClient }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await canvas.findAllByRole("button", { name: /^A closer look/ });
  const fresh = makeEntry(99, { title: "A newly published recommendation" });
  manualPublication.mockResolvedValueOnce({ items: [fresh, ...oldPage.items.slice(0, 24)], nextCursor: "fresh-older-cursor", snapshot: "fresh-opaque-head", generatedAt: FIXTURE_TIME });
  await userEvent.click(canvas.getByRole("button", { name: "Refresh feed" }));
  await expect(manualPublication).toHaveBeenCalledTimes(2);
  await expect(await canvas.findByRole("button", { name: /^A newly published recommendation/ })).toBeVisible();
} };

const pendingClient = interactiveScenario();
const pendingPublication = fn(async (_input: RequestBody<"refreshFeed">): Promise<FeedPage> => oldPage);
pendingClient.refreshFeed = pendingPublication;
pendingClient.getFeed = async () => oldPage;
export const RefreshPendingPosition: Story = { args: { ...feedArgs(), client: pendingClient }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await canvas.findAllByRole("button", { name: /^A closer look/ });
  const list = canvasElement.querySelector<HTMLElement>(".topic-stream-list-pane")!;
  list.scrollTop = 420;
  list.dispatchEvent(new Event("scroll"));
  const position = list.scrollTop;
  await expect(position).toBeGreaterThan(0);
  const response = deferred<FeedPage>();
  pendingPublication.mockReturnValueOnce(response.promise);
  await userEvent.click(canvas.getByRole("button", { name: "Refresh feed" }));
  await expect(canvas.getByRole("button", { name: "Refresh feed" })).toBeDisabled();
  await expect(list.scrollTop).toBe(position);
  await expect(canvasElement.querySelector(`[data-item-id="${fixtureId(300)}"]`)).toBeInTheDocument();
  response.resolve({ items: [makeEntry(99, { title: "Fresh head after a pending refresh" }), ...oldPage.items.slice(0, 24)], nextCursor: null, snapshot: "new-head-after-wait", generatedAt: FIXTURE_TIME });
  await canvas.findByRole("button", { name: /^Fresh head after a pending refresh/ });
  await waitFor(() => expect(list.scrollTop).toBe(0));
} };

const membershipClient = interactiveScenario();
const membershipPublication = fn(async (_input: RequestBody<"refreshFeed">): Promise<FeedPage> => oldPage);
membershipClient.refreshFeed = membershipPublication;
export const MembershipRefreshKeepsAnchor: Story = { args: { ...feedArgs(), client: membershipClient }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await canvas.findAllByRole("button", { name: /^A closer look/ });
  const list = canvasElement.querySelector<HTMLElement>(".topic-stream-list-pane")!;
  list.scrollTop = 420;
  list.dispatchEvent(new Event("scroll"));
  const anchor = Array.from(list.querySelectorAll<HTMLElement>("[data-item-id]")).find((row) => row.getBoundingClientRect().top >= list.getBoundingClientRect().top + 70)!;
  const top = anchor.getBoundingClientRect().top;
  const changed = { ...oldPage.items[1]!, item: { ...oldPage.items[1]!.item, title: "The same saved recommendation" } };
  const masked: FeedPage = { ...oldPage, items: [changed, ...oldPage.items.slice(2)], snapshot: "same-history-new-snapshot" };
  membershipClient.getFeed = async (request) => ({ ...masked, snapshot: request.snapshot ?? masked.snapshot });
  membershipPublication.mockResolvedValueOnce(masked);
  await userEvent.click(canvas.getByRole("button", { name: "Refresh feed" }));
  await canvas.findByRole("button", { name: /^The same saved recommendation/ });
  await expect(Math.abs(anchor.getBoundingClientRect().top - top)).toBeLessThan(1.5);
  await expect(list.scrollTop).toBeGreaterThan(0);
} };

const cachedArgs = feedArgs();
cachedArgs.client.collections.entries = Array.from({ length: 150 }, (_, index) => makeEntry(index));
export const IndependentScopeState: Story = { args: cachedArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  const rows = await canvas.findAllByRole("button", { name: /^A closer look/ });
  await userEvent.click(rows[1]!);
  const list = canvasElement.querySelector<HTMLElement>(".topic-stream-list-pane")!;
  list.scrollTop = 350; list.dispatchEvent(new Event("scroll"));
  await new Promise<void>((resolve) => canvasElement.ownerDocument.defaultView!.requestAnimationFrame(() => resolve()));
  await userEvent.click(canvas.getByRole("button", { name: "All Topics" }));
  await userEvent.click(within(await document.findByRole("dialog", { name: "Choose feed Topic" })).getByRole("option", { name: "Retrieval 2" }));
  await userEvent.click(await canvas.findByRole("button", { name: /^Notas de ingeniería sobre búsquedas · 2 / }));
  list.scrollTop = 160; list.dispatchEvent(new Event("scroll"));
  await new Promise<void>((resolve) => canvasElement.ownerDocument.defaultView!.requestAnimationFrame(() => resolve()));
  await userEvent.click(canvas.getByRole("button", { name: "Retrieval 2" }));
  await userEvent.click(within(await document.findByRole("dialog", { name: "Choose feed Topic" })).getByRole("option", { name: "All Topics" }));
  await expect(await canvas.findByRole("heading", { name: "A closer look at retrieval systems · 4" })).toBeVisible();
  await expect(list.scrollTop).toBe(350);
  await expect(canvas.getByRole("button", { name: "All Topics" })).toHaveFocus();
  await userEvent.click(canvas.getByRole("button", { name: "All Topics" }));
  await userEvent.click(within(await document.findByRole("dialog", { name: "Choose feed Topic" })).getByRole("option", { name: "Retrieval 2" }));
  await expect(await canvas.findByRole("heading", { name: "Notas de ingeniería sobre búsquedas · 2" })).toBeVisible();
  await expect(list.scrollTop).toBe(160);
  await expect(canvas.getByRole("button", { name: "Retrieval 2" })).toHaveFocus();
} };

export const CachedScopeWithoutSelection: Story = { args: feedArgs(), play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  await userEvent.click(await canvas.findByRole("button", { name: /^Notas de ingeniería sobre búsquedas · 2 / }));
  await userEvent.click(canvas.getByRole("button", { name: "All Topics" }));
  await userEvent.click(within(await document.findByRole("dialog", { name: "Choose feed Topic" })).getByRole("option", { name: "Retrieval 2" }));
  await canvas.findByRole("button", { name: /^Notas de ingeniería sobre búsquedas · 2 / });
  await userEvent.click(canvas.getByRole("button", { name: "Retrieval 2" }));
  await userEvent.click(within(await document.findByRole("dialog", { name: "Choose feed Topic" })).getByRole("option", { name: "All Topics" }));
  await canvas.findByRole("heading", { name: "Notas de ingeniería sobre búsquedas · 2" });
  await userEvent.click(canvas.getByRole("button", { name: "All Topics" }));
  await userEvent.click(within(await document.findByRole("dialog", { name: "Choose feed Topic" })).getByRole("option", { name: "Retrieval 2" }));
  await expect(canvas.getByRole("button", { name: "Retrieval 2" })).toHaveFocus();
  await expect(canvas.queryByRole("heading", { name: "Notas de ingeniería sobre búsquedas · 2" })).not.toBeInTheDocument();
} };

const pagedClient = interactiveScenario();
const olderEntries = Array.from({ length: 5 }, (_, index) => makeEntry(25 + index));
const pagedHead = { ...oldPage, nextCursor: "opaque/old+cursor" };
const pagedPublication = fn(async (_input: RequestBody<"refreshFeed">): Promise<FeedPage> => pagedHead);
const pagedRead = fn(async (request: FeedPageRequest): Promise<FeedPage> => request.cursor
  ? { ...oldPage, items: olderEntries, snapshot: request.snapshot!, nextCursor: null }
  : { ...pagedHead, snapshot: request.snapshot! });
pagedClient.refreshFeed = pagedPublication;
pagedClient.getFeed = pagedRead;
export const SnapshotPaginationAndRefresh: Story = { args: { ...feedArgs(), client: pagedClient }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await canvas.findAllByRole("button", { name: /^A closer look/ });
  const list = canvasElement.querySelector<HTMLElement>(".topic-stream-list-pane")!;
  list.scrollTop = list.scrollHeight;
  await canvas.findByRole("button", { name: /^Designing a durable reading workspace · 30 / });
  await expect(pagedRead).toHaveBeenCalledWith({ limit: 25, cursor: "opaque/old+cursor", snapshot: "old-opaque-head" });
  const freshHead = { ...pagedHead, snapshot: "fresh/snapshot+opaque", nextCursor: "fresh/cursor+opaque" };
  pagedPublication.mockResolvedValueOnce(freshHead);
  await userEvent.click(canvas.getByRole("button", { name: "Refresh feed" }));
  await expect(pagedRead).toHaveBeenCalledWith({ limit: 25, cursor: "fresh/cursor+opaque", snapshot: "fresh/snapshot+opaque" });
  await expect(canvas.getByRole("button", { name: /^Designing a durable reading workspace · 30 / })).toBeInTheDocument();
  await expect(list.scrollTop).toBeGreaterThan(0);
  pagedPublication.mockResolvedValueOnce({ ...freshHead, items: [makeEntry(99, { title: "A newer publication from another client" }), ...oldPage.items.slice(0, 24)], nextCursor: null });
  await userEvent.click(canvas.getByRole("button", { name: "Refresh feed" }));
  await canvas.findByRole("button", { name: /^A newer publication from another client/ });
  await expect(canvas.queryByRole("button", { name: /^Designing a durable reading workspace · 30 / })).not.toBeInTheDocument();
  await expect(list.scrollTop).toBe(0);
} };

const delayedClient = interactiveScenario();
const delayedPublication = fn(async (_input: RequestBody<"refreshFeed">): Promise<FeedPage> => oldPage);
delayedClient.refreshFeed = delayedPublication;
delayedClient.getFeed = async () => oldPage;
export const LateRefreshKeepsOtherScope: Story = { args: { ...feedArgs(), client: delayedClient }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  await canvas.findAllByRole("button", { name: /^A closer look/ });
  const response = deferred<FeedPage>();
  delayedPublication.mockReturnValueOnce(response.promise);
  await userEvent.dblClick(canvas.getByRole("button", { name: "Refresh feed" }));
  await expect(delayedPublication).toHaveBeenCalledTimes(2);
  await userEvent.click(canvas.getByRole("button", { name: "All Topics" }));
  await userEvent.click(within(await document.findByRole("dialog", { name: "Choose feed Topic" })).getByRole("option", { name: "Retrieval 2" }));
  const list = canvasElement.querySelector<HTMLElement>(".topic-stream-list-pane")!;
  await canvas.findAllByRole("button", { name: /^A closer look/ });
  list.scrollTop = 400;
  response.resolve({ ...oldPage, items: [makeEntry(99, { title: "A late All Topics publication" })], snapshot: "late-head" });
  await expect(canvas.queryByRole("button", { name: /^A late All Topics publication/ })).not.toBeInTheDocument();
  await expect(list.scrollTop).toBe(400);
  await new Promise<void>((resolve) => canvasElement.ownerDocument.defaultView!.requestAnimationFrame(() => resolve()));
  await userEvent.click(canvas.getByRole("button", { name: "Retrieval 2" }));
  await userEvent.click(within(await document.findByRole("dialog", { name: "Choose feed Topic" })).getByRole("option", { name: "All Topics" }));
  await expect(await canvas.findByRole("button", { name: /^A late All Topics publication/ })).toBeVisible();
  await expect(list.scrollTop).toBe(0);
  await expect(delayedPublication).toHaveBeenCalledTimes(3);
} };

const reapplyArgs = feedArgs();
reapplyArgs.feedPreferences = memoryFeedPreferences("unread");
reapplyArgs.client.collections.entries = [makeEntry()];
const reapplyPublication = fn(reapplyArgs.client.refreshFeed);
reapplyArgs.client.refreshFeed = reapplyPublication;
export const UnreadRefreshReapplies: Story = { args: reapplyArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  const title = await canvas.findByRole("button", { name: /^A closer look/ });
  await userEvent.click(within(title.closest("li")!).getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(document.getByRole("menuitem", { name: "Mark as read" }));
  await waitFor(() => expect(title).toHaveAttribute("data-reading-state", "read"));
  const pending = deferred<FeedPage>();
  reapplyPublication.mockReturnValueOnce(pending.promise);
  await userEvent.click(canvas.getByRole("button", { name: "Refresh feed" }));
  await expect(title).toBeInTheDocument();
  pending.reject(new Error("Refresh response unavailable."));
  await canvas.findByText("Refresh failed. Showing saved Feed.");
  await expect(title).toBeInTheDocument();
  await userEvent.click(canvas.getByRole("button", { name: "Refresh feed" }));
  await expect(await canvas.findByText("No unread items")).toBeVisible();
  await expect(canvas.queryByRole("button", { name: /^A closer look/ })).not.toBeInTheDocument();
  await expect(reapplyPublication).toHaveBeenCalledTimes(3);
} };

const filterArgs = feedArgs();
filterArgs.client.collections.entries[0]!.item.readingState = "read";
const filterPublication = fn(filterArgs.client.refreshFeed);
filterArgs.client.refreshFeed = filterPublication;
export const UnreadFilter: Story = { args: filterArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await canvas.findAllByRole("button", { name: /^A closer look/ });
  const filter = within(canvas.getByRole("group", { name: "Show items" }));
  await expect(filter.getByRole("button", { name: "All" })).toHaveAttribute("aria-pressed", "true");
  await userEvent.click(filter.getByRole("button", { name: "Unread" }));
  await expect(canvas.queryByRole("button", { name: /^A closer look at retrieval systems · 1 / })).not.toBeInTheDocument();
  await expect(canvas.getByRole("button", { name: /^Notas de ingeniería sobre búsquedas · 2 / })).toBeVisible();
  await userEvent.click(filter.getByRole("button", { name: "All" }));
  await expect(canvas.getByRole("button", { name: /^A closer look at retrieval systems · 1 / })).toBeVisible();
  await expect(filterPublication).toHaveBeenCalledTimes(1);
} };

const restoredFilterArgs = feedArgs();
restoredFilterArgs.feedPreferences = memoryFeedPreferences("unread");
restoredFilterArgs.client.collections.entries[0]!.item.readingState = "read";
export const RestoredVaultFilter: Story = { args: restoredFilterArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await canvas.findByRole("button", { name: /^Notas de ingeniería sobre búsquedas · 2 / });
  const filter = within(canvas.getByRole("group", { name: "Show items" }));
  await expect(filter.getByRole("button", { name: "Unread" })).toHaveAttribute("aria-pressed", "true");
  await expect(canvas.queryByRole("button", { name: /^A closer look at retrieval systems · 1 / })).not.toBeInTheDocument();
  await userEvent.click(filter.getByRole("button", { name: "All" }));
  await expect(restoredFilterArgs.feedPreferences.getSnapshot()).toBe("all");
} };

const emptyUnreadArgs = feedArgs();
emptyUnreadArgs.feedPreferences = memoryFeedPreferences("unread");
emptyUnreadArgs.client.refreshFeed = async () => ({ ...oldPage, items: oldPage.items.map((entry) => ({ ...entry, item: { ...entry.item, readingState: "read" } })), nextCursor: "older-unread-cursor" });
const unreadOlderRead = fn(async (_request: FeedPageRequest): Promise<FeedPage> => ({ ...oldPage, items: [makeEntry(99, { title: "An unread Item deeper in the archive" })], nextCursor: null }));
emptyUnreadArgs.client.getFeed = unreadOlderRead;
export const UnreadOlderPages: Story = { args: emptyUnreadArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await expect(await canvas.findByText("No unread items in loaded pages")).toBeVisible();
  await expect(unreadOlderRead).not.toHaveBeenCalled();
  await userEvent.click(canvas.getByRole("button", { name: "Load older items" }));
  await expect(await canvas.findByRole("button", { name: /^An unread Item deeper in the archive/ })).toBeVisible();
  await expect(unreadOlderRead).toHaveBeenCalledWith({ limit: 25, cursor: "older-unread-cursor", snapshot: "old-opaque-head" });
} };

const filterErrorArgs = feedArgs();
filterErrorArgs.feedPreferences.save = fn(async () => { throw new Error("Vault settings could not be written."); });
export const FilterSaveError: Story = { args: filterErrorArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await canvas.findAllByRole("button", { name: /^A closer look/ });
  const filter = within(canvas.getByRole("group", { name: "Show items" }));
  await userEvent.click(filter.getByRole("button", { name: "Unread" }));
  await expect(await canvas.findByRole("alert")).toHaveTextContent("Your previous choice is kept.");
  await expect(filter.getByRole("button", { name: "All" })).toHaveAttribute("aria-pressed", "true");
  await expect(filter.getByRole("button", { name: "Unread" })).toBeEnabled();
  await expect(filterErrorArgs.feedPreferences.save).toHaveBeenCalledTimes(1);
} };

const readArgs = feedArgs();
readArgs.feedPreferences = memoryFeedPreferences("unread");
const markRead = fn(async (_id: string, _state: "read" | "unread") => undefined);
Object.assign(readArgs.client, { setReadingState: markRead });
export const MarkReadRetainsUnreadRow: Story = { args: readArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  const title = await canvas.findByRole("button", { name: /^A closer look at retrieval systems · 1 / });
  const row = within(title.closest("li")!);
  await userEvent.click(row.getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(document.getByRole("menuitem", { name: "Mark as read" }));
  await expect(markRead).toHaveBeenCalledWith(fixtureId(300), "read");
  await waitFor(() => expect(title.closest(".topic-stream-row")).toHaveAttribute("data-reading-state", "read"));
  await expect(title).toBeInTheDocument();
  await userEvent.click(row.getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(document.getByRole("menuitem", { name: "Mark as unread" }));
  await waitFor(() => expect(title.closest(".topic-stream-row")).toHaveAttribute("data-reading-state", "unread"));
  await expect(markRead).toHaveBeenCalledTimes(2);
} };

const rowFeedbackArgs = feedArgs();
rowFeedbackArgs.client.setFeedback = fn(rowFeedbackArgs.client.setFeedback);
export const RowFeedbackAndDetails: Story = { args: rowFeedbackArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  const title = await canvas.findByRole("button", { name: /^A closer look at retrieval systems · 1 / });
  await userEvent.click(within(title.closest("li")!).getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(document.getByRole("menuitemcheckbox", { name: "Not for me" }));
  const note = within(await document.findByRole("dialog", { name: "Feedback note" }));
  await userEvent.type(note.getByRole("textbox"), "Keep this draft for this Item");
  await userEvent.keyboard("{Escape}");
  await expect(title).toBeInTheDocument();
  await userEvent.click(title);
  const detail = within(canvas.getByRole("region", { name: "Reading pane" }));
  await expect(await detail.findByText("Feedback: Not for me")).toBeVisible();
  await userEvent.click(detail.getByRole("button", { name: /^More actions for/ }));
  await expect(document.getByRole("menuitemcheckbox", { name: "Not for me" })).toBeChecked();
  await userEvent.click(document.getByRole("menuitem", { name: "Add note" }));
  await expect(within(await document.findByRole("dialog", { name: "Feedback note" })).getByRole("textbox")).toHaveValue("Keep this draft for this Item");
  await expect(rowFeedbackArgs.client.setFeedback).toHaveBeenCalledTimes(1);
} };

const detailsArgs = feedArgs();
detailsArgs.client.setReadingState = fn(detailsArgs.client.setReadingState);
export const WhyThisItemDoesNotMarkRead: Story = { args: detailsArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  const title = await canvas.findByRole("button", { name: /^A closer look at retrieval systems · 1 / });
  await userEvent.click(within(title.closest("li")!).getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(document.getByRole("menuitem", { name: "Why this item" }));
  const detail = within(canvas.getByRole("region", { name: "Reading pane" }));
  await expect(detail.getByRole("heading", { name: "Why this item" })).toBeVisible();
  await expect(detail.getByText("synthetic-fixture-v1")).toBeVisible();
  await expect(detail.getByText("semantic")).toBeVisible();
  await expect(detailsArgs.client.setReadingState).not.toHaveBeenCalled();
  await expect(title).toHaveAttribute("data-reading-state", "unread");
  await userEvent.click(detail.getByRole("button", { name: /^More actions for/ }));
  await expect(document.getByRole("menuitem", { name: "Open original" })).toBeVisible();
  await expect(document.getByRole("menuitem", { name: "Add to Topic…" })).toBeVisible();
  await expect(document.getByRole("menuitem", { name: "Hide from all Feeds" })).toBeVisible();
} };

const refreshedFeedbackArgs = feedArgs();
refreshedFeedbackArgs.client.collections.entries = [makeEntry()];
export const RefreshReconcilesFeedback: Story = { args: refreshedFeedbackArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  await canvas.findByRole("button", { name: /^A closer look/ });
  refreshedFeedbackArgs.client.collections.entries = [makeEntry(0, { feedback: { sentiment: "dislike", comment: "A note saved from another view", createdAt: FIXTURE_TIME, updatedAt: FIXTURE_TIME } })];
  await userEvent.click(canvas.getByRole("button", { name: "Refresh feed" }));
  await waitFor(() => expect(canvas.getByRole("button", { name: "Refresh feed" })).toBeEnabled());
  await userEvent.click(canvas.getByRole("button", { name: /^More actions for/ }));
  await expect(document.getByRole("menuitemcheckbox", { name: "Not for me" })).toBeChecked();
  await userEvent.click(document.getByRole("menuitem", { name: "Edit note" }));
  await expect(within(await document.findByRole("dialog", { name: "Feedback note" })).getByRole("textbox")).toHaveValue("A note saved from another view");
} };

const hideArgs = feedArgs();
const hideItem = fn(async (_id: string, _visibility: "hidden" | "visible") => undefined);
Object.assign(hideArgs.client, { setVisibility: hideItem });
export const HideGlobally: Story = { args: hideArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  await userEvent.click(await canvas.findByRole("button", { name: "All Topics" }));
  await userEvent.click(within(await document.findByRole("dialog", { name: "Choose feed Topic" })).getByRole("option", { name: "Retrieval 2" }));
  await canvas.findByRole("button", { name: /^Notas de ingeniería sobre búsquedas · 2 / });
  await userEvent.click(canvas.getByRole("button", { name: "Retrieval 2" }));
  await userEvent.click(within(await document.findByRole("dialog", { name: "Choose feed Topic" })).getByRole("option", { name: "All Topics" }));
  const title = canvas.getByRole("button", { name: /^Notas de ingeniería sobre búsquedas · 2 / });
  await userEvent.click(within(title.closest("li")!).getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(document.getByRole("menuitem", { name: "Hide from all Feeds" }));
  await waitFor(() => expect(canvas.queryByRole("button", { name: /^Notas de ingeniería sobre búsquedas · 2 / })).not.toBeInTheDocument());
  await expect(hideItem).toHaveBeenCalledWith(fixtureId(301), "hidden");
  await userEvent.click(canvas.getByRole("button", { name: "All Topics" }));
  await userEvent.click(within(await document.findByRole("dialog", { name: "Choose feed Topic" })).getByRole("option", { name: "Retrieval 2" }));
  await expect(canvas.queryByRole("button", { name: /^Notas de ingeniería sobre búsquedas · 2 / })).not.toBeInTheDocument();
} };

const correctionArgs = feedArgs();
export const WrongTopicKeepsAllHistory: Story = { args: correctionArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  await userEvent.click(await canvas.findByRole("button", { name: "All Topics" }));
  await userEvent.click(within(await document.findByRole("dialog", { name: "Choose feed Topic" })).getByRole("option", { name: "Retrieval 2" }));
  const title = await canvas.findByRole("button", { name: /^Notas de ingeniería sobre búsquedas · 2 / });
  await userEvent.click(within(title.closest("li")!).getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(document.getByRole("menuitem", { name: "Wrong topic: Retrieval 2" }));
  await waitFor(() => expect(canvas.queryByRole("button", { name: /^Notas de ingeniería sobre búsquedas · 2 / })).not.toBeInTheDocument());
  await userEvent.click(canvas.getByRole("button", { name: "Retrieval 2" }));
  await userEvent.click(within(await document.findByRole("dialog", { name: "Choose feed Topic" })).getByRole("option", { name: "All Topics" }));
  await expect(await canvas.findByRole("button", { name: /^Notas de ingeniería sobre búsquedas · 2 / })).toBeVisible();
} };

const addArgs = feedArgs();
const addPublications = fn(addArgs.client.refreshFeed);
addArgs.client.refreshFeed = addPublications;
export const AddToCachedTopic: Story = { args: addArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  await userEvent.click(await canvas.findByRole("button", { name: "All Topics" }));
  await userEvent.click(within(await document.findByRole("dialog", { name: "Choose feed Topic" })).getByRole("option", { name: "Retrieval 2" }));
  await canvas.findByRole("button", { name: /^Notas de ingeniería sobre búsquedas · 2 / });
  await userEvent.click(canvas.getByRole("button", { name: "Retrieval 2" }));
  await userEvent.click(within(await document.findByRole("dialog", { name: "Choose feed Topic" })).getByRole("option", { name: "All Topics" }));
  const title = canvas.getByRole("button", { name: /^A closer look at retrieval systems · 1 / });
  await userEvent.click(within(title.closest("li")!).getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(document.getByRole("menuitem", { name: "Add to Topic…" }));
  const picker = within(await document.findByRole("dialog", { name: "Add to Topic" }));
  await userEvent.type(picker.getByRole("searchbox", { name: "Find a Topic" }), "retrieval");
  await userEvent.click(picker.getByRole("option", { name: "Retrieval 2" }));
  await waitFor(() => expect(document.queryByRole("dialog", { name: "Add to Topic" })).not.toBeInTheDocument());
  await userEvent.click(canvas.getByRole("button", { name: "All Topics" }));
  await userEvent.click(within(await document.findByRole("dialog", { name: "Choose feed Topic" })).getByRole("option", { name: "Retrieval 2" }));
  await expect(await canvas.findByRole("button", { name: /^A closer look at retrieval systems · 1 / })).toBeVisible();
  await expect(canvas.getByRole("list", { name: "Personalized news feed" }).querySelector("[data-item-id]")).toHaveAttribute("data-item-id", fixtureId(300));
  await userEvent.click(within(canvas.getByRole("button", { name: /^A closer look at retrieval systems · 1 / }).closest("li")!).getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(document.getByRole("menuitem", { name: "Use automatic matching: Retrieval 2" }));
  await waitFor(() => expect(canvas.queryByRole("button", { name: /^A closer look at retrieval systems · 1 / })).not.toBeInTheDocument());
  await expect(addPublications).toHaveBeenCalledTimes(2);
} };

const committedCorrectionArgs = feedArgs();
const commitCorrection = committedCorrectionArgs.client.setTopicCorrection;
committedCorrectionArgs.client.setTopicCorrection = fn<typeof commitCorrection>(async (id, input) => {
  await commitCorrection(id, input);
  throw httpError(503, JSON.stringify({ type: "https://topic-stream.dev/problems/projection-pending", title: "Retrieval update pending", status: 503, code: "projection_pending", canonicalCommitted: true, projectionRevision: "correction-revision-19" }));
});
export const CorrectionSavedProjectionPending: Story = { args: committedCorrectionArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  await userEvent.click(await canvas.findByRole("button", { name: "All Topics" }));
  await userEvent.click(within(await document.findByRole("dialog", { name: "Choose feed Topic" })).getByRole("option", { name: "Retrieval 2" }));
  const title = await canvas.findByRole("button", { name: /^Notas de ingeniería sobre búsquedas · 2 / });
  await userEvent.click(within(title.closest("li")!).getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(document.getByRole("menuitem", { name: "Wrong topic: Retrieval 2" }));
  await expect(await canvas.findByText("Topic correction saved. Retrieval update pending.")).toBeVisible();
  await waitFor(() => expect(canvas.queryByRole("button", { name: /^Notas de ingeniería sobre búsquedas · 2 / })).not.toBeInTheDocument());
  await expect(canvas.queryByRole("alert")).not.toBeInTheDocument();
  await expect(committedCorrectionArgs.client.setTopicCorrection).toHaveBeenCalledTimes(1);
} };

const failedCorrectionArgs = feedArgs();
failedCorrectionArgs.client.setTopicCorrection = fn(async () => { throw new Error("Server unavailable."); });
export const CorrectionSaveError: Story = { args: failedCorrectionArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  const title = await canvas.findByRole("button", { name: /^A closer look at retrieval systems · 1 / });
  await userEvent.click(within(title.closest("li")!).getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(document.getByRole("menuitem", { name: "Add to Topic…" }));
  const picker = within(await document.findByRole("dialog", { name: "Add to Topic" }));
  await userEvent.click(picker.getByRole("option", { name: "Retrieval 2" }));
  await expect(await picker.findByRole("alert")).toHaveTextContent("Could not change Topic membership");
  await expect(picker.getByRole("option", { name: "Retrieval 2" })).toBeEnabled();
  await expect(title).toBeInTheDocument();
  await expect(failedCorrectionArgs.client.setTopicCorrection).toHaveBeenCalledTimes(1);
} };

const titleArgs = feedArgs();
titleArgs.feedPreferences = memoryFeedPreferences("unread");
titleArgs.client.setReadingState = fn(titleArgs.client.setReadingState);
export const TitleOpensReadingAndMarksRead: Story = { args: titleArgs, parameters: { frame: "mobile" }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const title = await canvas.findByRole("button", { name: /^A closer look at retrieval systems · 1 / });
  await userEvent.click(title);
  await expect(canvas.getByRole("heading", { name: "A closer look at retrieval systems · 1" })).toBeVisible();
  await expect(canvas.queryByRole("heading", { name: "Why this item" })).not.toBeInTheDocument();
  await expect(titleArgs.client.setReadingState).toHaveBeenCalledWith(fixtureId(300), "read");
  await userEvent.click(canvas.getByRole("button", { name: "Back to list" }));
  await expect(title).toHaveFocus();
  await waitFor(() => expect(title).toHaveAttribute("data-reading-state", "read"));
  await expect(title).toBeVisible();
  await expect(titleArgs.client.setReadingState).toHaveBeenCalledTimes(1);
} };

const originalArgs = feedArgs();
originalArgs.client.setReadingState = fn(originalArgs.client.setReadingState);
export const OpenOriginalMarksRead: Story = { args: originalArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  const browserWindow = canvasElement.ownerDocument.defaultView!;
  const previousOpen = browserWindow.open.bind(browserWindow);
  const open = fn<Window["open"]>(() => null);
  browserWindow.open = open;
  try {
    const title = await canvas.findByRole("button", { name: /^A closer look at retrieval systems · 1 / });
    await userEvent.click(within(title.closest("li")!).getByRole("button", { name: /^More actions for/ }));
    await userEvent.click(document.getByRole("menuitem", { name: "Open original" }));
    await expect(open).toHaveBeenCalledWith("https://example.org/items/1", "_blank", "noopener,noreferrer");
    await waitFor(() => expect(title).toHaveAttribute("data-reading-state", "read"));
    await expect(originalArgs.client.setReadingState).toHaveBeenCalledTimes(1);
    await expect(canvas.queryByRole("heading", { name: "A closer look at retrieval systems · 1" })).not.toBeInTheDocument();
    const second = canvas.getByRole("button", { name: /^Notas de ingeniería sobre búsquedas · 2 / });
    await userEvent.click(within(second.closest("li")!).getByRole("button", { name: /^More actions for/ }));
    await userEvent.click(document.getByRole("menuitem", { name: "Why this item" }));
    await expect(originalArgs.client.setReadingState).toHaveBeenCalledTimes(1);
    await userEvent.click(canvas.getByRole("link", { name: "Notas de ingeniería sobre búsquedas · 2" }));
    await expect(open).toHaveBeenLastCalledWith("https://example.org/items/2", "_blank", "noopener,noreferrer");
    await waitFor(() => expect(second).toHaveAttribute("data-reading-state", "read"));
    await expect(originalArgs.client.setReadingState).toHaveBeenCalledTimes(2);
  } finally { browserWindow.open = previousOpen; }
} };

const failedReadingArgs = feedArgs();
failedReadingArgs.client.setReadingState = fn(async () => { throw new Error("Reading state could not be saved."); });
export const ReadingSaveErrorInPane: Story = { args: failedReadingArgs, parameters: { frame: "mobile" }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /^A closer look at retrieval systems · 1 / }));
  const pane = within(canvas.getByRole("region", { name: "Reading pane" }));
  await expect(await pane.findByRole("alert")).toHaveTextContent("Could not change reading state");
  await userEvent.click(canvas.getByRole("button", { name: "Back to list" }));
  const title = canvas.getByRole("button", { name: /^A closer look at retrieval systems · 1 / });
  await expect(title).toHaveAttribute("data-reading-state", "unread");
  await expect(within(title.closest("li")!).getByRole("button", { name: /^More actions for/ })).toBeEnabled();
  await expect(failedReadingArgs.client.setReadingState).toHaveBeenCalledTimes(1);
} };

const eventArgs = feedArgs();
const eventPublication = fn(async (_input: RequestBody<"refreshFeed">): Promise<FeedPage> => oldPage);
const eventRead = fn(async (request: FeedPageRequest): Promise<FeedPage> => ({ ...oldPage, snapshot: request.snapshot! }));
eventArgs.client.refreshFeed = eventPublication;
eventArgs.client.getFeed = eventRead;
export const AdvisoryKeepsSavedHead: Story = { args: eventArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await canvas.findAllByRole("button", { name: /^A closer look/ });
  const list = canvasElement.querySelector<HTMLElement>(".topic-stream-list-pane")!;
  list.scrollTop = 480; list.dispatchEvent(new Event("scroll"));
  eventArgs.events.emit({ type: "feedInvalidated", itemId: fixtureId(399) });
  const refresh = canvas.getByRole("button", { name: "Refresh feed" });
  await waitFor(() => expect(refresh).toHaveAttribute("title", "Updates available. Refresh feed"));
  await waitFor(() => expect(eventRead).toHaveBeenCalledWith({ limit: 25, cursor: null, snapshot: "old-opaque-head" }));
  await expect(eventPublication).toHaveBeenCalledTimes(1);
  await expect(list.scrollTop).toBe(480);
  const fresh = makeEntry(99, { title: "A publication announced by an advisory" });
  eventPublication.mockResolvedValueOnce({ ...oldPage, items: [fresh, ...oldPage.items.slice(0, 24)], snapshot: "announced-new-head" });
  await expect(canvas.queryByRole("button", { name: /^A publication announced/ })).not.toBeInTheDocument();
  await userEvent.click(refresh);
  await canvas.findByRole("button", { name: /^A publication announced/ });
  await waitFor(() => expect(list.scrollTop).toBe(0));
  await expect(refresh).toHaveAttribute("title", "Refresh feed");
  await expect(eventPublication).toHaveBeenCalledTimes(2);
} };

const backgroundArgs = feedArgs();
backgroundArgs.feedPreferences = memoryFeedPreferences("unread");
backgroundArgs.client.collections.entries = [makeEntry()];
const backgroundPublication = fn(backgroundArgs.client.refreshFeed);
backgroundArgs.client.refreshFeed = backgroundPublication;
const backgroundRead = fn(backgroundArgs.client.getFeed);
backgroundArgs.client.getFeed = backgroundRead;
export const UnreadBackgroundReconciliation: Story = { args: backgroundArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  const title = await canvas.findByRole("button", { name: /^A closer look/ });
  backgroundArgs.client.collections.entries = [makeEntry(0, { readingState: "read", feedback: { sentiment: "like", comment: null, createdAt: FIXTURE_TIME, updatedAt: FIXTURE_TIME } })];
  backgroundArgs.events.emit({ type: "feedInvalidated", itemId: fixtureId(300) });
  await waitFor(() => expect(title).toHaveAttribute("data-reading-state", "read"));
  await expect(title).toBeVisible();
  await userEvent.click(within(title.closest("li")!).getByRole("button", { name: /^More actions for/ }));
  await expect(document.getByRole("menuitemcheckbox", { name: "Useful" })).toBeChecked();
  await userEvent.keyboard("{Escape}");
  backgroundArgs.events.emit({ type: "reconnected" });
  await waitFor(() => expect(backgroundRead).toHaveBeenCalledWith({ limit: 25, cursor: null, snapshot: "fixture-snapshot" }));
  await expect(title).toBeVisible();
  await expect(backgroundPublication).toHaveBeenCalledTimes(1);
  await userEvent.click(title);
  await userEvent.click(canvas.getByRole("button", { name: "Back to list" }));
  await expect(title).toBeVisible();
  await userEvent.click(canvas.getByRole("button", { name: "Refresh feed" }));
  await expect(await canvas.findByText("No unread items")).toBeVisible();
} };

const dislikeArgs = feedArgs();
dislikeArgs.client.setFeedback = fn(dislikeArgs.client.setFeedback);
export const DislikeOffersWrongTopic: Story = { args: dislikeArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  await userEvent.click(await canvas.findByRole("button", { name: "All Topics" }));
  await userEvent.click(within(await document.findByRole("dialog", { name: "Choose feed Topic" })).getByRole("option", { name: "Retrieval 2" }));
  const title = await canvas.findByRole("button", { name: /^Notas de ingeniería sobre búsquedas · 2 / });
  await userEvent.click(within(title.closest("li")!).getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(document.getByRole("menuitemcheckbox", { name: "Not for me" }));
  const note = within(await document.findByRole("dialog", { name: "Feedback note" }));
  await expect(title).toBeInTheDocument();
  await waitFor(() => expect(note.getByRole("button", { name: "Wrong topic: Retrieval 2" })).toBeEnabled());
  await userEvent.click(note.getByRole("button", { name: "Wrong topic: Retrieval 2" }));
  await waitFor(() => expect(canvas.queryByRole("button", { name: /^Notas de ingeniería sobre búsquedas · 2 / })).not.toBeInTheDocument());
  await expect(dislikeArgs.client.setFeedback).toHaveBeenCalledTimes(1);
} };

const olderErrorArgs = feedArgs();
olderErrorArgs.client.refreshFeed = async () => pagedHead;
let olderCanRecover = false;
const failedOlderRead = fn(async (request: FeedPageRequest): Promise<FeedPage> => {
  if (!olderCanRecover) throw new Error("The older page is unavailable.");
  if (request.snapshot !== "old-opaque-head") return { ...oldPage, items: [makeEntry(99, { title: "A newer head that must not enter this traversal" })] };
  return { ...oldPage, items: olderEntries };
});
olderErrorArgs.client.getFeed = failedOlderRead;
export const OlderPageErrorKeepsSnapshot: Story = { args: olderErrorArgs, beforeEach: () => { olderCanRecover = false; }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await canvas.findAllByRole("button", { name: /^A closer look/ });
  const list = canvasElement.querySelector<HTMLElement>(".topic-stream-list-pane")!;
  list.scrollTop = list.scrollHeight;
  const error = await canvas.findByRole("alert");
  await expect(error).toHaveTextContent("Could not load older items");
  await expect(error).toBeVisible();
  await expect(error.getBoundingClientRect().top).toBeGreaterThanOrEqual(list.getBoundingClientRect().top);
  await expect(canvas.getByRole("button", { name: /^A closer look at retrieval systems · 1 / })).toBeInTheDocument();
  await expect(failedOlderRead).toHaveBeenCalledTimes(1);
  olderCanRecover = true;
  await userEvent.click(canvas.getByRole("button", { name: "Try again" }));
  await canvas.findByRole("button", { name: /^Designing a durable reading workspace · 30 / });
  await expect(canvas.queryByRole("button", { name: /^A newer head that/ })).not.toBeInTheDocument();
  await expect(failedOlderRead).toHaveBeenLastCalledWith({ limit: 25, cursor: "opaque/old+cursor", snapshot: "old-opaque-head" });
  await expect(canvas.getByRole("button", { name: /^A closer look at retrieval systems · 1 / })).toBeInTheDocument();
} };

const positionedFailureArgs = feedArgs();
const positionedPublication = fn(positionedFailureArgs.client.refreshFeed);
positionedFailureArgs.client.refreshFeed = positionedPublication;
export const RefreshErrorStaysBesideRefresh: Story = { args: positionedFailureArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await canvas.findAllByRole("button", { name: /^A closer look/ });
  const list = canvasElement.querySelector<HTMLElement>(".topic-stream-list-pane")!;
  list.scrollTop = 600; list.dispatchEvent(new Event("scroll"));
  await new Promise<void>((resolve) => canvasElement.ownerDocument.defaultView!.requestAnimationFrame(() => resolve()));
  positionedPublication.mockRejectedValueOnce(new Error("Publication response unavailable."));
  await userEvent.click(canvas.getByRole("button", { name: "Refresh feed" }));
  const error = await canvas.findByText("Refresh failed. Showing saved Feed.");
  await expect(error).toBeVisible();
  const refresh = canvas.getByRole("button", { name: "Refresh feed" });
  await expect(Math.abs(error.getBoundingClientRect().top - refresh.getBoundingClientRect().bottom)).toBeLessThan(30);
  const text = canvasElement.ownerDocument.createRange();
  text.selectNodeContents(error);
  await expect(error.parentElement!.getBoundingClientRect().bottom - text.getBoundingClientRect().bottom).toBeGreaterThanOrEqual(8);
  await expect(list.scrollTop).toBe(600);
} };

const emptyHiddenArgs = feedArgs();
emptyHiddenArgs.client.collections.entries = [makeEntry()];
export const EmptyAfterHide: Story = { args: emptyHiddenArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const title = await canvas.findByRole("button", { name: /^A closer look/ });
  await userEvent.click(within(title.closest("li")!).getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(within(canvasElement.ownerDocument.body).getByRole("menuitem", { name: "Hide from all Feeds" }));
  await expect(await canvas.findByText("Nothing here yet")).toBeVisible();
} };

const emptyWithCursorArgs = feedArgs();
emptyWithCursorArgs.feedPreferences = memoryFeedPreferences("unread");
emptyWithCursorArgs.client.refreshFeed = async () => ({ ...oldPage, items: [], nextCursor: "an-older-page" });
export const EmptyLoadedPageHasOlderHistory: Story = { args: emptyWithCursorArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await expect(await canvas.findByText("No unread items in loaded pages")).toBeVisible();
  await expect(canvas.queryByText("Nothing here yet")).not.toBeInTheDocument();
  await expect(canvas.getByRole("button", { name: "Load older items" })).toBeEnabled();
} };

const lateReadArgs = feedArgs();
const delayedSavedRead = deferred<FeedPage>();
const lateReadPublication = fn(async (_input: RequestBody<"refreshFeed">): Promise<FeedPage> => oldPage);
const savedHeadRead = fn(async (request: FeedPageRequest): Promise<FeedPage> => ({ ...oldPage, snapshot: request.snapshot! }));
savedHeadRead.mockReturnValueOnce(delayedSavedRead.promise);
lateReadArgs.client.refreshFeed = lateReadPublication;
lateReadArgs.client.getFeed = savedHeadRead;
export const LateReconciliationCannotReplaceNewHead: Story = { args: lateReadArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await canvas.findAllByRole("button", { name: /^A closer look/ });
  lateReadArgs.events.emit({ type: "feedInvalidated", itemId: fixtureId(300) });
  await waitFor(() => expect(savedHeadRead).toHaveBeenCalledTimes(1));
  lateReadPublication.mockResolvedValueOnce({ ...oldPage, items: [makeEntry(99, { title: "A fresh head retained through late reconciliation" }), ...oldPage.items.slice(0, 24)], snapshot: "fresh-head-before-late-read" });
  await userEvent.click(canvas.getByRole("button", { name: "Refresh feed" }));
  await canvas.findByRole("button", { name: /^A fresh head retained/ });
  delayedSavedRead.resolve({ ...oldPage, items: [makeEntry(0, { title: "A late obsolete saved page" })] });
  await new Promise<void>((resolve) => canvasElement.ownerDocument.defaultView!.requestAnimationFrame(() => resolve()));
  await expect(canvas.getByRole("button", { name: /^A fresh head retained/ })).toBeVisible();
  await expect(canvas.queryByRole("button", { name: /^A late obsolete saved page/ })).not.toBeInTheDocument();
} };
