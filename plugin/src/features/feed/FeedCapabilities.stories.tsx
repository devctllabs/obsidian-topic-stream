import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent, waitFor, within } from "storybook/test";
import { ConnectedShell } from "../shell/ConnectedShell";
import { ShellNavigation } from "../shell/navigation";
import { interactiveScenario } from "../../test/storybook/interactive-scenario";
import { memoryWorkspaceState } from "../../test/storybook/local-state";
import { memoryFeedPreferences } from "../../test/storybook/feed-preferences";
import { deferred } from "../../test/storybook/deferred";
import type { FeedItemCapabilities, ItemSummaryCapability } from "./capabilities";
import type { Schema } from "../../api/contracts";
import { fixtureId, makeSummary } from "../../test/storybook/fixtures";

function capabilityArgs(capabilities: FeedItemCapabilities) {
  const client = interactiveScenario();
  client.setReadingState = fn(client.setReadingState);
  return { client, navigation: new ShellNavigation(), localState: memoryWorkspaceState(), feedPreferences: memoryFeedPreferences(), notify: client.notify, capabilities };
}
const meta = { title: "Features/Feed/Views/FeedCapabilities", component: ConnectedShell, tags: ["!dev", "!autodocs"], parameters: { shell: true }, args: capabilityArgs({}) } satisfies Meta<typeof ConnectedShell>;
export default meta;
type Story = StoryObj<typeof meta>;

const saved = deferred<{ path: string } | null>();
const save = fn(async () => saved.promise);
const saveArgs = capabilityArgs({ saveToVault: save });
export const SaveDoesNotMarkRead: Story = { args: saveArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  const title = await canvas.findByRole("button", { name: /^A closer look at retrieval systems · 1 / });
  await expect(save).not.toHaveBeenCalled();
  const more = within(title.closest("li")!).getByRole("button", { name: /^More actions for/ });
  await userEvent.click(more);
  await userEvent.click(document.getByRole("menuitem", { name: "Save to vault" }));
  await expect(save).toHaveBeenCalledWith(saveArgs.client.collections.entries[0]!.item, more);
  await expect(more).toBeDisabled();
  await expect(saveArgs.client.setReadingState).not.toHaveBeenCalled();
  saved.resolve({ path: "Topic Stream/An explicit Snapshot.md" });
  await expect(await canvas.findByText("Saved to vault.")).toBeVisible();
  await expect(title).toHaveAttribute("data-reading-state", "unread");
  await expect(save).toHaveBeenCalledTimes(1);
} };

const saveError = fn(async () => { throw new Error("Vault write failed."); });
const saveErrorArgs = capabilityArgs({ saveToVault: saveError });
export const SaveError: Story = { args: saveErrorArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const title = await canvas.findByRole("button", { name: /^A closer look at retrieval systems · 1 / });
  const more = within(title.closest("li")!).getByRole("button", { name: /^More actions for/ });
  await userEvent.click(more);
  await userEvent.click(within(canvasElement.ownerDocument.body).getByRole("menuitem", { name: "Save to vault" }));
  await expect(await canvas.findByRole("alert")).toHaveTextContent("Could not save to vault");
  await expect(more).toBeEnabled();
  await expect(saveErrorArgs.client.setReadingState).not.toHaveBeenCalled();
  await expect(saveError).toHaveBeenCalledTimes(1);
} };

const renderReader = fn<NonNullable<FeedItemCapabilities["renderReader"]>>((item) => item.contentMarkdown);
const readerArgs = capabilityArgs({ renderReader });
export const DirectReaderCapability: Story = { args: readerArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const title = await canvas.findByRole("button", { name: /^A closer look at retrieval systems · 1 / });
  await expect(renderReader).not.toHaveBeenCalled();
  await userEvent.click(title);
  await expect(renderReader).toHaveBeenCalledWith(readerArgs.client.collections.entries[0]!.item);
  await expect(canvas.queryByRole("heading", { name: "Why this item" })).not.toBeInTheDocument();
  await waitFor(() => expect(title).toHaveAttribute("data-reading-state", "read"));
} };

function summaryPort(initial: Schema<"ItemSummary">, request: ItemSummaryCapability["request"]) {
  let state = initial;
  const listeners = new Set<() => void>();
  return {
    getSnapshot: (item: { id: string }) => item.id === fixtureId(300) ? state : null,
    subscribe: (_item: unknown, listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener); }; },
    request,
    render: (item: { id: string }, expanded: boolean) => item.id === fixtureId(300) && expanded ? state.result?.text : null,
    update: (value: Schema<"ItemSummary">) => { state = value; for (const listener of listeners) listener(); }
  } satisfies ItemSummaryCapability & { update: (value: Schema<"ItemSummary">) => void };
}
const summaryAccepted = deferred<void>();
const requestSummary = fn<ItemSummaryCapability["request"]>(async () => summaryAccepted.promise);
const liveSummary = summaryPort(makeSummary(), requestSummary);
const summaryArgs = capabilityArgs({ summary: liveSummary });
export const ExplicitSummaryOnly: Story = { args: summaryArgs, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  const title = await canvas.findByRole("button", { name: /^A closer look at retrieval systems · 1 / });
  const row = within(title.closest("li")!);
  const excerpt = title.textContent;
  await expect(requestSummary).not.toHaveBeenCalled();
  await userEvent.click(row.getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(document.getByRole("menuitem", { name: "AI summary" }));
  await expect(row.getByRole("group", { name: "AI summary" })).toHaveAttribute("aria-busy", "true");
  await expect(requestSummary).toHaveBeenCalledTimes(1);
  liveSummary.update(makeSummary({ state: "pending" })); summaryAccepted.resolve();
  await userEvent.click(row.getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(document.getByRole("menuitem", { name: "AI summary" }));
  await expect(requestSummary).toHaveBeenCalledTimes(1);
  liveSummary.update(makeSummary({ state: "ready" }));
  await expect(await row.findByText("Synthetic AI summary fixture. This text is not provider evidence.")).toBeVisible();
  await expect(title.textContent).toBe(excerpt);
  await expect(summaryArgs.client.setReadingState).not.toHaveBeenCalled();
  await expect(canvas.getAllByRole("group", { name: "AI summary" })).toHaveLength(1);
} };

const optedOut = summaryPort(makeSummary({ canGenerate: false, unavailableReason: "generation_disabled" }), fn(async () => undefined));
export const SummaryRequiresOptIn: Story = { args: capabilityArgs({ summary: optedOut }), play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  const title = await canvas.findByRole("button", { name: /^A closer look at retrieval systems · 1 / });
  await userEvent.click(within(title.closest("li")!).getByRole("button", { name: /^More actions for/ }));
  await expect(document.queryByRole("menuitem", { name: "AI summary" })).not.toBeInTheDocument();
  await userEvent.keyboard("{Escape}");
  optedOut.update(makeSummary({ state: "ready", canGenerate: false, unavailableReason: "generation_disabled" }));
  await userEvent.click(within(title.closest("li")!).getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(document.getByRole("menuitem", { name: "AI summary" }));
  await expect(await canvas.findByText("Synthetic AI summary fixture. This text is not provider evidence.")).toBeVisible();
  await expect(optedOut.request).not.toHaveBeenCalled();
} };

const unsupported = summaryPort(makeSummary({ supported: false, canGenerate: false, unavailableReason: "unsupported_input" }), fn(async () => undefined));
export const UnsupportedSummaryHidden: Story = { args: capabilityArgs({ summary: unsupported }), play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const title = await canvas.findByRole("button", { name: /^A closer look at retrieval systems · 1 / });
  await userEvent.click(within(title.closest("li")!).getByRole("button", { name: /^More actions for/ }));
  await expect(within(canvasElement.ownerDocument.body).queryByRole("menuitem", { name: "AI summary" })).not.toBeInTheDocument();
  await expect(unsupported.request).not.toHaveBeenCalled();
} };

const failingSummaryRequest = fn<ItemSummaryCapability["request"]>(async () => { throw new Error("Generation service unavailable."); });
const failingSummary = summaryPort(makeSummary(), failingSummaryRequest);
export const SummaryRequestError: Story = { args: capabilityArgs({ summary: failingSummary }), play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const title = await canvas.findByRole("button", { name: /^A closer look at retrieval systems · 1 / });
  const row = within(title.closest("li")!);
  await userEvent.click(row.getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(within(canvasElement.ownerDocument.body).getByRole("menuitem", { name: "AI summary" }));
  await expect(await row.findByRole("alert")).toHaveTextContent("Could not request AI summary");
  await userEvent.click(row.getByRole("button", { name: "Retry AI summary" }));
  await expect(failingSummaryRequest).toHaveBeenCalledTimes(2);
  await expect(title).toHaveAttribute("data-reading-state", "unread");
} };

const paneSummary = summaryPort(makeSummary({ state: "ready" }), fn(async () => undefined));
export const ReadySummaryInReadingPane: Story = { args: capabilityArgs({ summary: paneSummary }), play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /^A closer look at retrieval systems · 1 / }));
  const pane = within(canvas.getByRole("region", { name: "Reading pane" }));
  await userEvent.click(pane.getByRole("button", { name: "AI summary" }));
  await expect(pane.getByRole("heading", { name: "AI summary", level: 2 })).toBeVisible();
  await expect(paneSummary.request).not.toHaveBeenCalled();
} };
