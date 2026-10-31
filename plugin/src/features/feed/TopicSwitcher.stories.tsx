import { useState, type ComponentProps } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent, within } from "storybook/test";
import { TopicSwitcher } from "./TopicSwitcher";
import { makeTopic } from "../../test/storybook/fixtures";

const topics = Array.from({ length: 10 }, (_, index) => makeTopic(index));
function ControlledSwitcher(props: ComponentProps<typeof TopicSwitcher>) {
  const [value, setValue] = useState(props.value);
  return <TopicSwitcher {...props} value={value} onChange={(next) => { props.onChange(next); setValue(next); }} />;
}
const meta = {
  title: "Features/Feed/Components/TopicSwitcher", component: TopicSwitcher,
  args: { client: { listTopics: async () => topics }, value: null, onChange: fn() },
  render: (args) => <ControlledSwitcher {...args} />
} satisfies Meta<typeof TopicSwitcher>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Loaded: Story = {};
export const KeyboardSelection: Story = { play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  await userEvent.click(canvas.getByRole("button", { name: "All Topics" }));
  const picker = within(await document.findByRole("dialog", { name: "Choose feed Topic" }));
  await expect(picker.getByRole("searchbox")).toHaveFocus();
  await userEvent.keyboard("{ArrowDown}{End}");
  await expect(picker.getByRole("option", { name: "Reading 10" })).toHaveFocus();
  await userEvent.keyboard("{Home}");
  await expect(picker.getByRole("option", { name: "All Topics" })).toHaveFocus();
  await userEvent.type(picker.getByRole("searchbox"), "retrieval");
  await userEvent.keyboard("{ArrowUp}");
  await expect(picker.getByRole("option", { name: "Retrieval 7" })).toHaveFocus();
  await userEvent.keyboard("{Enter}");
  await expect(canvas.getByRole("button", { name: "Retrieval 7" })).toHaveFocus();
  await expect(document.queryByRole("dialog", { name: "Choose feed Topic" })).not.toBeInTheDocument();
} };

let topicsRecovered = false;
const recoverTopics = fn(async () => { if (!topicsRecovered) throw new Error("Could not reach the server."); return topics; });
export const LoadError: Story = { args: { client: { listTopics: recoverTopics } }, beforeEach: () => { topicsRecovered = false; }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  await userEvent.click(canvas.getByRole("button", { name: "All Topics" }));
  const picker = within(await document.findByRole("dialog", { name: "Choose feed Topic" }));
  await picker.findByRole("alert");
  await userEvent.keyboard("{Tab}");
  await expect(picker.getByRole("button", { name: "Try again" })).toHaveFocus();
  topicsRecovered = true;
  await userEvent.keyboard("{Enter}");
  await expect(await picker.findByRole("option", { name: "Retrieval 2" })).toBeVisible();
} };

export const Loading: Story = { args: { client: { listTopics: () => new Promise(() => undefined) } }, play: async ({ canvasElement }) => {
  await userEvent.click(within(canvasElement).getByRole("button", { name: "All Topics" }));
  await expect(await within(canvasElement.ownerDocument.body).findByText("Loading Topics…")).toBeVisible();
} };
export const NoMatchingTopics: Story = { play: async ({ canvasElement }) => {
  await userEvent.click(within(canvasElement).getByRole("button", { name: "All Topics" }));
  const picker = within(await within(canvasElement.ownerDocument.body).findByRole("dialog", { name: "Choose feed Topic" }));
  await userEvent.type(picker.getByRole("searchbox"), "A Topic that does not exist");
  await expect(picker.getByText("No matching Topics")).toBeVisible();
  await expect(picker.getByRole("option", { name: "All Topics" })).toBeVisible();
} };
export const LongContent: Story = { parameters: { frame: "mobile" }, args: { client: { listTopics: async () => [makeTopic(0, { name: "Notas sobre ingeniería y sistemas de búsqueda ".repeat(8) })] } }, play: async ({ canvasElement }) => {
  await userEvent.click(within(canvasElement).getByRole("button", { name: "All Topics" }));
  const picker = await within(canvasElement.ownerDocument.body).findByRole("dialog", { name: "Choose feed Topic" });
  await expect(picker.scrollWidth).toBeLessThanOrEqual(picker.clientWidth);
} };
