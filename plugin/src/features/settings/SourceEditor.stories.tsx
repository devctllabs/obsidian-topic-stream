import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent, within } from "storybook/test";
import { SourceEditor, type SourceEditorProps } from "./SourceEditor";
import { ShellNavigation } from "../shell/navigation";
import { makeSource } from "../../test/storybook/fixtures";
import { deferred } from "../../test/storybook/deferred";
import { ApiError } from "../../api/errors";

const meta = {
  title: "Features/Settings/Components/SourceEditor", component: SourceEditor,
  args: { source: null, navigation: new ShellNavigation({ screen: "settings" }), onSave: fn<SourceEditorProps["onSave"]>(async () => undefined), onCancel: fn() }
} satisfies Meta<typeof SourceEditor>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Loaded: Story = { play: async ({ canvasElement, args }) => {
  const canvas = within(canvasElement);
  await userEvent.type(canvas.getByRole("textbox", { name: "Name" }), "Engineering notes");
  await userEvent.type(canvas.getByRole("textbox", { name: "Feed URL" }), "https://example.org/feed");
  await userEvent.click(canvas.getByRole("button", { name: "Create source" }));
  await expect(args.onSave).toHaveBeenCalledWith({
    name: "Engineering notes", enabled: true, pollIntervalMinutes: 60,
    config: { kind: "rss", url: "https://example.org/feed", allowPrivateNetwork: false }
  });
} };
export const Arxiv: Story = { play: async ({ canvasElement, args }) => {
  const canvas = within(canvasElement);
  await userEvent.type(canvas.getByRole("textbox", { name: "Name" }), "Research");
  await userEvent.selectOptions(canvas.getByRole("combobox", { name: "Adapter" }), "arxiv");
  await userEvent.type(canvas.getByRole("textbox", { name: "arXiv query" }), "cat:cs.IR");
  await userEvent.click(canvas.getByRole("button", { name: "Create source" }));
  await expect(args.onSave).toHaveBeenCalledWith({ name: "Research", enabled: true, pollIntervalMinutes: 60, config: { kind: "arxiv", query: "cat:cs.IR" } });
} };
export const Youtube: Story = { play: async ({ canvasElement, args }) => {
  const canvas = within(canvasElement);
  await userEvent.type(canvas.getByRole("textbox", { name: "Name" }), "Channel notes");
  await userEvent.selectOptions(canvas.getByRole("combobox", { name: "Adapter" }), "youtube");
  await userEvent.type(canvas.getByRole("textbox", { name: "Channel ID" }), "UC" + "a".repeat(22));
  await userEvent.click(canvas.getByRole("button", { name: "Create source" }));
  await expect(args.onSave).toHaveBeenCalledWith({ name: "Channel notes", enabled: true, pollIntervalMinutes: 60, config: { kind: "youtube", channelId: "UC" + "a".repeat(22) } });
} };
export const GithubTrending: Story = { play: async ({ canvasElement, args }) => {
  const canvas = within(canvasElement);
  await userEvent.type(canvas.getByRole("textbox", { name: "Name" }), "Rust tools");
  await userEvent.selectOptions(canvas.getByRole("combobox", { name: "Adapter" }), "github");
  await userEvent.selectOptions(canvas.getByRole("combobox", { name: "Period" }), "weekly");
  await userEvent.selectOptions(canvas.getByRole("combobox", { name: "Language" }), "rust");
  await userEvent.click(canvas.getByRole("button", { name: "Create source" }));
  await expect(args.onSave).toHaveBeenCalledWith({ name: "Rust tools", enabled: true, pollIntervalMinutes: 60, config: { kind: "github", period: "weekly", language: "rust" } });
} };
export const Editing: Story = { args: { source: makeSource() }, play: async ({ canvasElement, args }) => {
  const canvas = within(canvasElement);
  await expect(canvas.getByRole("textbox", { name: "Name" })).toHaveValue("Source 1");
  await userEvent.clear(canvas.getByRole("textbox", { name: "Name" }));
  await userEvent.type(canvas.getByRole("textbox", { name: "Name" }), "Updated source");
  await userEvent.click(canvas.getByRole("button", { name: "Save source" }));
  await expect(args.onSave).toHaveBeenCalledWith({ name: "Updated source", enabled: true, pollIntervalMinutes: 60, config: { kind: "rss", url: "https://example.org/source-1.xml", allowPrivateNetwork: false } });
} };
export const ValidationErrors: Story = { play: async ({ canvasElement, args }) => {
  const canvas = within(canvasElement);
  await userEvent.click(canvas.getByRole("button", { name: "Create source" }));
  await expect(await canvas.findByText("Enter a name.")).toBeVisible();
  await expect(args.onSave).not.toHaveBeenCalled();
} };
export const Atom: Story = { play: async ({ canvasElement, args }) => {
  const canvas = within(canvasElement);
  await userEvent.type(canvas.getByRole("textbox", { name: "Name" }), "Atom notes");
  await userEvent.selectOptions(canvas.getByRole("combobox", { name: "Adapter" }), "atom");
  await userEvent.type(canvas.getByRole("textbox", { name: "Feed URL" }), "https://example.org/atom");
  await userEvent.click(canvas.getByRole("button", { name: "Create source" }));
  await expect(args.onSave).toHaveBeenCalledWith({ name: "Atom notes", enabled: true, pollIntervalMinutes: 60, config: { kind: "atom", url: "https://example.org/atom", allowPrivateNetwork: false } });
} };
export const InvalidChannel: Story = { play: async ({ canvasElement, args }) => {
  const canvas = within(canvasElement);
  await userEvent.type(canvas.getByRole("textbox", { name: "Name" }), "Channel");
  await userEvent.selectOptions(canvas.getByRole("combobox", { name: "Adapter" }), "youtube");
  await userEvent.type(canvas.getByRole("textbox", { name: "Channel ID" }), "https://youtube.com/@channel");
  await userEvent.click(canvas.getByRole("button", { name: "Create source" }));
  await expect(await canvas.findByText("Enter a YouTube Channel ID beginning with UC.")).toBeVisible();
  await expect(args.onSave).not.toHaveBeenCalled();
} };
export const SaveError: Story = { args: { source: makeSource(), onSave: fn<SourceEditorProps["onSave"]>(async () => { throw new ApiError(400, null); }) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const name = canvas.getByRole("textbox", { name: "Name" });
  await userEvent.clear(name);
  await userEvent.type(name, "My draft");
  await userEvent.click(canvas.getByRole("button", { name: "Save source" }));
  await expect(await canvas.findByText("Could not save source")).toBeVisible();
  await expect(name).toHaveValue("My draft");
} };
export const SavePending: Story = { args: { source: makeSource(), onSave: fn<SourceEditorProps["onSave"]>(() => deferred<void>().promise) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(canvas.getByRole("button", { name: "Save source" }));
  await expect(canvas.getByRole("button", { name: "Save source" })).toBeDisabled();
  await expect(canvas.getByRole("textbox", { name: "Name" })).toBeDisabled();
} };
export const Advanced: Story = { args: { source: makeSource(0, { enabled: false, pollIntervalMinutes: 30, config: { kind: "rss", url: "http://localhost/feed", allowPrivateNetwork: true } }) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(canvas.getByText("Advanced"));
  await expect(canvas.getByRole("checkbox", { name: "Enabled" })).not.toBeChecked();
  await expect(canvas.getByRole("checkbox", { name: "Allow private network fetches" })).toBeChecked();
  await expect(canvas.getByRole("spinbutton", { name: "Poll interval (minutes)" })).toHaveValue(30);
} };
export const LongContent: Story = { args: { source: makeSource(0, { name: "Notas de ingeniería y sistemas de búsqueda ".repeat(3).slice(0, 120) }) }, parameters: { frame: "mobile" } };
