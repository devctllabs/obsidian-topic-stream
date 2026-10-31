import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent, within } from "storybook/test";

import { FIXTURE_TIME, makeEntry } from "../../test/storybook/fixtures";
import { FeedCard } from "./FeedCard";
import type { FeedService } from "./services";
import { httpError } from "../../api/errors";

const save = fn<FeedService["setFeedback"]>(async (_itemId, sentiment, comment) => ({ sentiment, comment, createdAt: FIXTURE_TIME, updatedAt: FIXTURE_TIME }));
const meta = {
  title: "Features/Feed/Components/FeedCard", component: FeedCard,
  args: { entry: makeEntry(), client: { setFeedback: save } }, tags: ["autodocs"]
} satisfies Meta<typeof FeedCard>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Loaded: Story = {};
export const FeedbackMenu: Story = { play: async ({ canvasElement, args }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  await expect(canvas.queryByRole("button", { name: "Useful" })).not.toBeInTheDocument();
  await userEvent.click(canvas.getByRole("button", { name: `More actions for ${args.entry.item.title}` }));
  await userEvent.click(document.getByRole("menuitemcheckbox", { name: "Useful" }));
  const note = await document.findByRole("dialog", { name: "Feedback note" });
  await expect(args.client.setFeedback).toHaveBeenCalledWith(args.entry.item.id, "like", null);
  await expect(await canvas.findByText("Feedback: Useful")).toBeVisible();
  await userEvent.type(within(note).getByRole("textbox", { name: "Optional note for future recommendations" }), "Clear evaluation examples");
  await expect(args.client.setFeedback).toHaveBeenCalledTimes(1);
  await userEvent.click(canvas.getByRole("button", { name: `More actions for ${args.entry.item.title}` }));
  await expect(document.queryByRole("dialog", { name: "Feedback note" })).not.toBeInTheDocument();
  await expect(document.getByRole("menuitemcheckbox", { name: "Useful" })).toBeChecked();
  await expect(document.getByRole("menuitemcheckbox", { name: "Not for me" })).not.toBeChecked();
  await userEvent.click(document.getByRole("menuitem", { name: "Add note" }));
  const reopened = await document.findByRole("dialog", { name: "Feedback note" });
  await expect(within(reopened).getByRole("textbox")).toHaveValue("Clear evaluation examples");
  await expect(args.client.setFeedback).toHaveBeenCalledTimes(1);
  await userEvent.click(within(reopened).getByRole("button", { name: "Send note" }));
  await expect(args.client.setFeedback).toHaveBeenLastCalledWith(args.entry.item.id, "like", "Clear evaluation examples");
  await expect(await canvas.findByText("Note sent")).toBeVisible();
  await expect(document.queryByRole("dialog")).not.toBeInTheDocument();
  await expect(canvas.getByRole("button", { name: `More actions for ${args.entry.item.title}` })).toHaveFocus();
} };
export const LongContent: Story = { args: { entry: makeEntry(0, { title: "A long multilingual Item title: ingeniería y sistemas de búsqueda ".repeat(5) }) } };
export const FeedbackPending: Story = {
  args: { client: { setFeedback: fn<FeedService["setFeedback"]>(() => new Promise(() => undefined)) } },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: /^More actions for/ }));
    await userEvent.click(within(canvasElement.ownerDocument.body).getByRole("menuitemcheckbox", { name: "Useful" }));
    await expect(canvas.getByRole("button", { name: /^More actions for/ })).toBeDisabled();
    await expect(within(canvasElement.ownerDocument.body).getByText("Saving feedback…")).toBeVisible();
    await userEvent.keyboard("{Escape}");
    await expect(within(canvasElement.ownerDocument.body).queryByRole("dialog", { name: "Feedback note" })).not.toBeInTheDocument();
  }
};
export const FeedbackError: Story = {
  args: { client: { setFeedback: fn<FeedService["setFeedback"]>(() => Promise.reject(new Error("Could not connect to Topic Stream."))) } },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: /^More actions for/ }));
    await userEvent.click(within(canvasElement.ownerDocument.body).getByRole("menuitemcheckbox", { name: "Useful" }));
    await expect(await within(canvasElement.ownerDocument.body).findByRole("alert")).toHaveTextContent("Could not save feedback");
  }
};
export const NotePending: Story = { args: { client: { setFeedback: fn<FeedService["setFeedback"]>(async (_id, sentiment, comment) => {
  if (comment) return new Promise(() => undefined);
  return { sentiment, comment, createdAt: FIXTURE_TIME, updatedAt: FIXTURE_TIME };
}) } }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  await userEvent.click(canvas.getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(document.getByRole("menuitemcheckbox", { name: "Useful" }));
  const note = within(await document.findByRole("dialog", { name: "Feedback note" }));
  await canvas.findByText("Feedback: Useful");
  await userEvent.type(note.getByRole("textbox"), "My explanation");
  await userEvent.click(note.getByRole("button", { name: "Send note" }));
  await expect(note.getByRole("button", { name: "Send note" })).toBeDisabled();
  await expect(note.getByText("Sending note…")).toBeVisible();
} };
export const NoteError: Story = { args: { client: { setFeedback: fn<FeedService["setFeedback"]>(async (_id, sentiment, comment) => {
  if (comment) throw new Error("The backend is unavailable.");
  return { sentiment, comment, createdAt: FIXTURE_TIME, updatedAt: FIXTURE_TIME };
}) } }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  await userEvent.click(canvas.getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(document.getByRole("menuitemcheckbox", { name: "Not for me" }));
  const note = within(await document.findByRole("dialog", { name: "Feedback note" }));
  await canvas.findByText("Feedback: Not for me");
  await userEvent.type(note.getByRole("textbox"), "Keep this draft");
  await userEvent.click(note.getByRole("button", { name: "Send note" }));
  await expect(await note.findByRole("alert")).toHaveTextContent("Could not send note");
  await expect(canvas.getByText("Feedback: Not for me")).toBeVisible();
  await expect(note.getByRole("textbox")).toHaveValue("Keep this draft");
  await expect(note.getByRole("button", { name: "Send note" })).toBeEnabled();
} };

export const FeedbackSavedProjectionPending: Story = { args: { client: { setFeedback: fn<FeedService["setFeedback"]>(async () => {
  throw httpError(503, JSON.stringify({ type: "https://topic-stream.dev/problems/projection-pending", title: "Retrieval update pending", status: 503, code: "projection_pending", canonicalCommitted: true, projectionRevision: "opaque-revision-17" }));
}) } }, play: async ({ canvasElement, args }) => {
  const canvas = within(canvasElement);
  const document = within(canvasElement.ownerDocument.body);
  await userEvent.click(canvas.getByRole("button", { name: /^More actions for/ }));
  await userEvent.click(document.getByRole("menuitemcheckbox", { name: "Useful" }));
  await expect(await canvas.findByText("Feedback: Useful")).toBeVisible();
  await expect(await document.findByText("Saved. Retrieval update pending.")).toBeVisible();
  await expect(document.queryByRole("alert")).not.toBeInTheDocument();
  await userEvent.keyboard("{Escape}");
  await userEvent.click(canvas.getByRole("button", { name: /^More actions for/ }));
  await expect(document.getByRole("menuitemcheckbox", { name: "Useful" })).toBeChecked();
  await expect(args.client.setFeedback).toHaveBeenCalledTimes(1);
} };
