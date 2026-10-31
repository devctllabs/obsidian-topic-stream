import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fireEvent, fn, userEvent, within } from "storybook/test";
import { DigestEditor, type DigestEditorProps } from "./DigestEditor";
import { ShellNavigation } from "../shell/navigation";
import { makeTopic, fixtureId, FIXTURE_TIME } from "../../test/storybook/fixtures";
import type { Schema } from "../../api/contracts";
import { ApiError } from "../../api/errors";

const meta = { title: "Features/Settings/Components/DigestEditor", component: DigestEditor,
  args: { digest: null, topics: [makeTopic(0, { name: "Topic 1" }), makeTopic(1, { name: "Topic 2" })], navigation: new ShellNavigation({ screen: "settings" }), onSave: fn<DigestEditorProps["onSave"]>(async () => undefined), onCancel: fn(), loadLatest: fn<DigestEditorProps["loadLatest"]>(async () => ({ ...saved, version: 5 })) }
} satisfies Meta<typeof DigestEditor>;
export default meta;
type Story = StoryObj<typeof meta>;
export const ManualOnly: Story = { play: async ({ canvasElement, args }) => {
  const canvas = within(canvasElement);
  await expect(canvas.getByRole("combobox", { name: "Delivery" })).toHaveValue("manual");
  await expect(canvas.queryByRole("combobox", { name: "Repeat" })).not.toBeInTheDocument();
  await userEvent.type(canvas.getByRole("textbox", { name: "Name" }), "Weekly reading");
  await userEvent.selectOptions(canvas.getByRole("combobox", { name: "Add topic" }), fixtureId(200));
  await userEvent.click(canvas.getByRole("button", { name: "Create definition" }));
  await expect(args.onSave).toHaveBeenCalledWith({ name: "Weekly reading", topics: [{ topicId: fixtureId(200), itemLimit: 5 }], enabled: false, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone, recurrence: { frequency: "weekly", weekdays: [1], hour: 12, minute: 0 } }, null);
} };
export const RetainedCalendar: Story = { play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.selectOptions(canvas.getByRole("combobox", { name: "Delivery" }), "scheduled");
  await userEvent.selectOptions(canvas.getByRole("combobox", { name: "Repeat" }), "daily");
  const time = canvas.getByLabelText("Time");
  await fireEvent.change(time, { target: { value: "08:30" } });
  await userEvent.selectOptions(canvas.getByRole("combobox", { name: "Delivery" }), "manual");
  await expect(canvas.queryByLabelText("Time")).not.toBeInTheDocument();
  await userEvent.selectOptions(canvas.getByRole("combobox", { name: "Delivery" }), "scheduled");
  await expect(canvas.getByLabelText("Time")).toHaveValue("08:30");
  await expect(canvas.getByRole("combobox", { name: "Repeat" })).toHaveValue("daily");
} };
const saved: Schema<"DigestDefinition"> = { id: fixtureId(900), name: "Saved reading", topics: [{ topicId: fixtureId(200), itemLimit: 7 }], timezone: "Europe/Madrid", recurrence: { frequency: "monthly", rule: { type: "lastDay" }, hour: 18, minute: 15 }, enabled: false, version: 4, nextRunAt: null, lastSuccessfulAt: null, createdAt: FIXTURE_TIME, updatedAt: FIXTURE_TIME };
export const UnchangedManualEdit: Story = { args: { digest: saved }, play: async ({ canvasElement, args }) => {
  const canvas = within(canvasElement);
  await userEvent.click(canvas.getByRole("button", { name: "Save definition" }));
  const { name, topics, timezone, recurrence, enabled } = saved;
  await expect(args.onSave).toHaveBeenCalledWith({ name, topics, timezone, recurrence, enabled }, 4);
} };
export const OrderedTopics: Story = { play: async ({ canvasElement, args }) => {
  const canvas = within(canvasElement);
  await userEvent.type(canvas.getByRole("textbox", { name: "Name" }), "Reading order");
  await userEvent.selectOptions(canvas.getByRole("combobox", { name: "Add topic" }), fixtureId(200));
  await userEvent.selectOptions(canvas.getByRole("combobox", { name: "Add topic" }), fixtureId(201));
  const limit = canvas.getByRole("spinbutton", { name: "Item limit for Topic 2" });
  await fireEvent.change(limit, { target: { value: "9" } });
  await userEvent.click(canvas.getByRole("button", { name: "Move Topic 2 up" }));
  await userEvent.click(canvas.getByRole("button", { name: "Create definition" }));
  await expect(args.onSave).toHaveBeenCalledWith(expect.objectContaining({ topics: [{ topicId: fixtureId(201), itemLimit: 9 }, { topicId: fixtureId(200), itemLimit: 5 }] }), null);
} };
export const Invalid: Story = { play: async ({ canvasElement, args }) => {
  const canvas = within(canvasElement);
  await userEvent.click(canvas.getByRole("button", { name: "Create definition" }));
  await expect(await canvas.findByText("Enter a name.")).toBeVisible();
  await expect(args.onSave).not.toHaveBeenCalled();
} };
export const Failure: Story = { args: { digest: saved, onSave: fn<DigestEditorProps["onSave"]>(async () => { throw new Error("Connection interrupted"); }) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.type(canvas.getByRole("textbox", { name: "Name" }), " revised");
  await userEvent.click(canvas.getByRole("button", { name: "Save definition" }));
  await expect(await canvas.findByText("Could not save digest definition. Your draft is kept.")).toBeVisible();
  await expect(canvas.getByRole("textbox", { name: "Name" })).toHaveValue("Saved reading revised");
} };
export const Conflict: Story = { args: { digest: saved }, play: async ({ canvasElement, args }) => {
  args.onSave.mockRejectedValueOnce(new ApiError(412, null));
  const canvas = within(canvasElement);
  await userEvent.type(canvas.getByRole("textbox", { name: "Name" }), " revised");
  await userEvent.click(canvas.getByRole("button", { name: "Save definition" }));
  await userEvent.click(await canvas.findByRole("button", { name: "Review latest" }));
  await expect(await canvas.findByText("Saved version 5")).toBeVisible();
  await userEvent.click(canvas.getByRole("button", { name: "Keep my draft" }));
  await expect(args.onSave).toHaveBeenCalledTimes(1);
  await expect(canvas.getByRole("textbox", { name: "Name" })).toHaveValue("Saved reading revised");
  await userEvent.click(canvas.getByRole("button", { name: "Save definition" }));
  await expect(args.onSave).toHaveBeenLastCalledWith(expect.objectContaining({ name: "Saved reading revised", enabled: false }), 5);
} };
