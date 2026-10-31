import { memoryWorkspaceState } from "../../test/storybook/local-state";
import { memoryFeedPreferences } from "../../test/storybook/feed-preferences";
import { useEffect, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, userEvent, waitFor, within } from "storybook/test";
import { ShellNavigation } from "./navigation";
import { ConnectedShell } from "./ConnectedShell";
import { showcaseScenario } from "../../test/storybook/showcase-scenario";
import { memorySnapshotVault } from "../../test/storybook/snapshot-vault";
import { fixtureId } from "../../test/storybook/fixtures";

function ShellPreview({ selected = true }: { selected?: boolean }) {
  const [navigation] = useState(() => new ShellNavigation({ selections: selected ? { feed: fixtureId(300) } : {} }));
  const [client] = useState(showcaseScenario);
  useEffect(() => () => client.dispose(), [client]);
  const [localState] = useState(memoryWorkspaceState);
  const [feedPreferences] = useState(memoryFeedPreferences);
  const [snapshots] = useState(memorySnapshotVault);
  return <ConnectedShell snapshots={snapshots} notify={client.notify} localState={localState} navigation={navigation} client={client} feedPreferences={feedPreferences} events={client.events} />;
}
const meta = { title: "Features/Shell/TopicStreamShell", component: ShellPreview, parameters: { layout: "fullscreen", shell: true, docs: { description: { component: "The production connected shell and feature screens with in-memory services. No live backend connection. Each feature appears here as its production implementation lands." } } } } satisfies Meta<typeof ShellPreview>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Loaded: Story = { play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await expect(canvas.getByRole("button", { name: "Refresh feed" })).toBeVisible();
  const refresh = canvas.getByRole("button", { name: "Refresh feed" });
  await expect((await canvas.findAllByRole("button", { name: /^A closer look/ }))[0]).toBeVisible();
  await expect(refresh).toBeEnabled();
  await expect(await within(canvas.getByRole("region", { name: "Reading pane" })).findByRole("button", { name: "AI summary" })).toBeVisible();
} };
export const ReaderActionsRegression: Story = { tags: ["!dev", "!autodocs"], play: async ({ canvasElement }) => {
  const pane = within(within(canvasElement).getByRole("region", { name: "Reading pane" }));
  await userEvent.click(await pane.findByRole("button", { name: "AI summary" }));
  await expect(await pane.findByText("Synthetic AI summary fixture. This text is not provider evidence.")).toBeVisible();
  await userEvent.click(pane.getByRole("button", { name: /Load all images in this Item$/ }));
  await expect(await pane.findByRole("img", { name: "From source to useful results" })).toBeVisible();
} };
export const SnapshotWorkflowRegression: Story = { tags: ["!dev", "!autodocs"], play: async ({ canvasElement }) => {
  const canvas = within(canvasElement); const document = within(canvasElement.ownerDocument.body);
  const pane = within(canvas.getByRole("region", { name: "Reading pane" }));
  const trigger = await pane.findByRole("button", { name: /^More actions for/ });
  await userEvent.click(trigger); await userEvent.click(await document.findByRole("menuitem", { name: "Save to vault" }));
  await expect(await canvas.findByRole("dialog", { name: "Save to vault" })).toBeVisible();
  await userEvent.keyboard("{Escape}"); await waitFor(() => expect(trigger).toHaveFocus());
  await userEvent.click(trigger); await userEvent.click(document.getByRole("menuitem", { name: "Save to vault" }));
  await userEvent.click(canvas.getByRole("button", { name: "Save" }));
  await expect(await pane.findByText("Saved to vault.")).toBeVisible();
  await userEvent.click(canvas.getByRole("tab", { name: "Digests" }));
  await userEvent.click(await canvas.findByRole("button", { name: /^The weekly reading list/ }));
  await userEvent.click(await canvas.findByRole("button", { name: "Issue actions" }));
  await userEvent.click(document.getByRole("menuitem", { name: "Save to vault" }));
  await userEvent.click(canvas.getByRole("radio", { name: "Full" }));
  await userEvent.click(canvas.getByRole("radio", { name: "Local" }));
  await userEvent.click(canvas.getByRole("button", { name: "Save" }));
  await expect(await canvas.findByText(/Saved to vault with 12 local images/)).toBeVisible();
} };
export const DigestAndTaskScreens: Story = { tags: ["!dev", "!autodocs"], play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(canvas.getByRole("tab", { name: "Digests" }));
  await userEvent.click(await canvas.findByRole("button", { name: /^The weekly reading list/ }));
  await expect(await canvas.findByRole("heading", { name: "The weekly reading list", level: 1 })).toBeVisible();
  await expect(canvas.getByRole("heading", { name: "Retrieval systems", level: 2 })).toBeVisible();
  await userEvent.click(canvas.getByRole("button", { name: "Build a Digest Issue" }));
  await userEvent.click(await within(canvasElement.ownerDocument.body).findByRole("menuitem", { name: "Build The weekly reading list · Manual only" }));
  await waitFor(() => expect(canvas.getByText("Build complete")).toBeVisible(), { timeout: 5000 });
  await userEvent.click(canvas.getByRole("button", { name: "View build task for The weekly reading list" }));
  await expect(await canvas.findByRole("heading", { name: "Build Digest Issue", level: 1 })).toBeVisible();
  await userEvent.click(await canvas.findByRole("button", { name: "Open saved issue" }));
  await expect(await canvas.findByRole("heading", { name: /^The weekly reading list ·/, level: 1 })).toBeVisible();
} };
export const Light: Story = { globals: { theme: "light" } };
export const Narrow: Story = { args: { selected: false }, parameters: { frame: "narrow" } };
export const Mobile: Story = { ...Narrow, globals: { theme: "dark" }, parameters: { frame: "mobile" } };
export const NarrowNavigationRegression: Story = { ...Narrow, tags: ["!dev", "!autodocs"], play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const list = canvasElement.querySelector<HTMLElement>(".topic-stream-list-pane");
  if (!list) throw new Error("Missing list pane");
  const trigger = (await canvas.findAllByRole("button", { name: /^A closer look/ }))[1];
  if (!trigger) throw new Error("Missing reading item");
  trigger.scrollIntoView({ block: "center" });
  await new Promise<void>((resolve) => canvasElement.ownerDocument.defaultView!.requestAnimationFrame(() => resolve()));
  const position = list.scrollTop;
  await userEvent.click(trigger);
  await expect(canvas.getByRole("region", { name: "Reading pane" })).toBeVisible();
  await expect(list).not.toBeVisible();
  await userEvent.click(canvas.getByRole("button", { name: "Settings" }));
  await userEvent.click(canvas.getByRole("button", { name: "Back to reading" }));
  await expect(canvas.getByRole("region", { name: "Reading pane" })).toBeVisible();
  await userEvent.click(canvas.getByRole("button", { name: "Back to list" }));
  await expect(trigger).toHaveFocus();
  await expect(list.scrollTop).toBe(position);
  const leaf = canvasElement.querySelector<HTMLElement>(".workspace-leaf");
  await expect(leaf?.scrollWidth).toBeLessThanOrEqual(460);
} };
export const LargeText: Story = { ...Narrow, parameters: { frame: "mobile", fontScale: 2 }, play: async ({ canvasElement }) => {
  const leaf = canvasElement.querySelector<HTMLElement>(".workspace-leaf");
  if (!leaf) throw new Error("Missing host frame");
  await expect(leaf.scrollWidth).toBeLessThanOrEqual(leaf.clientWidth);
} };
export const ResponsiveLabelsRegression: Story = { ...LargeText, tags: ["!dev", "!autodocs"], play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const singleLine = async (element: HTMLElement) => {
    const range = canvasElement.ownerDocument.createRange(); range.selectNodeContents(element);
    await expect(range.getClientRects()).toHaveLength(1);
  };
  for (const tab of canvas.getAllByRole("tab")) await singleLine(tab);
  await userEvent.click(canvas.getByRole("button", { name: "Settings" }));
  await singleLine(await canvas.findByRole("heading", { name: "Settings" }));
  await singleLine(await canvas.findByRole("heading", { name: "Sources" }));
  await expect(canvas.getByRole("button", { name: "Add source" })).toBeVisible();
  const leaf = canvasElement.querySelector<HTMLElement>(".workspace-leaf")!;
  await expect(leaf.scrollWidth).toBeLessThanOrEqual(leaf.clientWidth);
} };
export const NarrowSettingsWidthRegression: Story = { ...Narrow, tags: ["!dev", "!autodocs"], play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(canvas.getByRole("button", { name: "Settings" }));
  await canvas.findByRole("heading", { name: "Sources" });
  const layout = canvasElement.querySelector<HTMLElement>(".topic-stream-settings-layout")!;
  const panel = layout.querySelector<HTMLElement>(":scope > section")!;
  await expect(panel.getBoundingClientRect().width).toBeCloseTo(layout.getBoundingClientRect().width, 0);
} };
export const NavigationRegression: Story = { tags: ["!dev", "!autodocs"], play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await expect(canvas.getByRole("heading", { name: "Feed" })).toBeVisible();
  await userEvent.click(canvas.getByRole("tab", { name: "Digests" }));
  await expect(canvas.getByRole("heading", { name: "Digests" })).toBeVisible();
  await userEvent.click(canvas.getByRole("button", { name: "Settings" }));
  await expect(canvas.getByRole("heading", { name: "Settings" })).toBeVisible();
  await userEvent.click(canvas.getByRole("button", { name: "Back to reading" }));
  await expect(canvas.getByRole("heading", { name: "Digests" })).toBeVisible();
  await expect(canvas.getByRole("tab", { name: "Digests" })).toHaveFocus();
  await userEvent.keyboard("{ArrowRight}");
  await expect(canvas.getByRole("heading", { name: "Search" })).toBeVisible();
  await userEvent.click(canvas.getByRole("button", { name: "Tasks" }));
  await expect(canvas.getByRole("heading", { name: "Tasks" })).toBeVisible();
} };
