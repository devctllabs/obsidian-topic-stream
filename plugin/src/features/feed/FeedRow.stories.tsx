import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent, within } from "storybook/test";
import { FeedRow } from "./FeedRow";
import { makeEntry } from "../../test/storybook/fixtures";
import { interactiveScenario } from "../../test/storybook/interactive-scenario";

const meta = {
  title: "Features/Feed/Components/FeedRow", component: FeedRow,
  args: { client: interactiveScenario(), entry: makeEntry(), selected: false, topicId: null, onOpen: fn(), onDetails: fn(), onReadingState: fn(), onResult: fn() },
  decorators: [(Story) => <ul className="topic-stream-reading-list"><Story /></ul>]
} satisfies Meta<typeof FeedRow>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Loaded: Story = { play: async ({ canvasElement }) => {
  await expect(within(canvasElement).getByText("Article")).toBeVisible();
  await expect(within(canvasElement).getByRole("button", { name: /^A closer look/ })).toHaveAttribute("data-reading-state", "unread");
} };
export const Read: Story = { args: { entry: makeEntry(0, { readingState: "read" }) } };
export const Selected: Story = { args: { selected: true } };
export const LongContent: Story = { parameters: { frame: "mobile" }, args: { entry: makeEntry(0, { title: "Ingeniería y sistemas de búsqueda ".repeat(8), excerpt: "A long excerpt with an unbroken identifier " + "LongIdentifier".repeat(25) }) }, play: async ({ canvasElement }) => {
  const row = canvasElement.querySelector<HTMLElement>(".topic-stream-feed-row")!;
  await expect(row.scrollWidth).toBeLessThanOrEqual(row.clientWidth);
} };
const readPending = interactiveScenario();
readPending.setReadingState = () => new Promise(() => undefined);
export const ReadingPending: Story = { args: { client: readPending }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(canvas.getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(within(canvasElement.ownerDocument.body).getByRole("menuitem", { name: "Mark as read" }));
  await expect(canvas.getByRole("button", { name: /^More actions for/ })).toBeDisabled();
} };
const hideError = interactiveScenario();
hideError.setVisibility = async () => { throw new Error("Server unavailable."); };
export const HideError: Story = { args: { client: hideError }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(canvas.getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(within(canvasElement.ownerDocument.body).getByRole("menuitem", { name: "Hide from all Feeds" }));
  await expect(await canvas.findByRole("alert")).toHaveTextContent("Could not hide Item");
  await expect(canvas.getByRole("button", { name: /^More actions for/ })).toBeEnabled();
} };
