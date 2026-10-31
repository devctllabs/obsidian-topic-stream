import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent, within } from "storybook/test";
import { PreferenceProfileEditor, type PreferenceProfileService } from "./PreferenceProfileEditor";
import { ApiError } from "../../api/errors";
import { FixtureScenario } from "../../test/storybook/scenario";
import { deferred } from "../../test/storybook/deferred";

const profile = { prefer: "Practical engineering", avoid: "Promotional content", version: 3, revision: 2, updatedAt: "2026-10-27T09:00:00Z" };
const service: PreferenceProfileService = {
  getPreferenceProfile: fn(async () => profile),
  updatePreferenceProfile: fn<PreferenceProfileService["updatePreferenceProfile"]>(async (input, version) => ({ ...profile, ...input, version: version + 1, revision: profile.revision + 1 }))
};
const meta = { title: "Features/Settings/Components/PreferenceProfileEditor", component: PreferenceProfileEditor, args: { service } } satisfies Meta<typeof PreferenceProfileEditor>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Loaded: Story = { play: async ({ canvasElement, args }) => {
  const canvas = within(canvasElement);
  const prefer = await canvas.findByRole("textbox", { name: "Prefer" });
  await expect(prefer).toHaveValue(profile.prefer);
  await userEvent.clear(prefer);
  await userEvent.type(prefer, "Sistemas fiables");
  await userEvent.click(canvas.getByRole("button", { name: "Save changes" }));
  await expect(args.service.updatePreferenceProfile).toHaveBeenCalledWith({ prefer: "Sistemas fiables", avoid: profile.avoid }, 3);
  await expect(canvas.getByRole("status")).toHaveTextContent("Saved");
} };
const conflictService = {
  getPreferenceProfile: fn<PreferenceProfileService["getPreferenceProfile"]>(async () => profile),
  updatePreferenceProfile: fn<PreferenceProfileService["updatePreferenceProfile"]>(async () => { throw new ApiError(412, null); })
};
export const Conflict: Story = { args: { service: conflictService }, play: async ({ canvasElement, args }) => {
  const canvas = within(canvasElement);
  const prefer = await canvas.findByRole("textbox", { name: "Prefer" });
  await userEvent.clear(prefer);
  await userEvent.type(prefer, "My draft");
  await userEvent.click(canvas.getByRole("button", { name: "Save changes" }));
  await expect(await canvas.findByText("Profile changed on the server. Your draft is kept.")).toBeVisible();
  await expect(prefer).toHaveValue("My draft");
  conflictService.getPreferenceProfile.mockResolvedValue({ ...profile, prefer: "Server changes", version: 4 });
  conflictService.updatePreferenceProfile.mockResolvedValue({ ...profile, prefer: "My draft", version: 5 });
  await userEvent.click(canvas.getByRole("button", { name: "Review latest" }));
  await expect(await canvas.findByText("Server changes")).toBeVisible();
  await userEvent.click(canvas.getByRole("button", { name: "Keep my draft" }));
  await expect(prefer).toHaveValue("My draft");
  await userEvent.click(canvas.getByRole("button", { name: "Save changes" }));
  await expect(args.service.updatePreferenceProfile).toHaveBeenLastCalledWith({ prefer: "My draft", avoid: profile.avoid }, 4);
} };
export const Empty: Story = { args: { service: { ...service, getPreferenceProfile: fn(async () => ({ ...profile, prefer: "", avoid: "" })) } }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await expect(await canvas.findByRole("textbox", { name: "Prefer" })).toHaveValue("");
  await expect(canvas.getByRole("textbox", { name: "Avoid" })).toHaveValue("");
  await expect(canvas.getByRole("button", { name: "Save changes" })).toBeDisabled();
} };
const pendingLoad = deferred<typeof profile>();
export const Loading: Story = { args: { service: { ...service, getPreferenceProfile: fn(() => pendingLoad.promise) } }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await expect(canvas.getByRole("status")).toHaveTextContent("Loading preference profile…");
  pendingLoad.resolve(profile);
  await expect(await canvas.findByRole("textbox", { name: "Prefer" })).toHaveValue(profile.prefer);
} };
const pendingSaveService = new FixtureScenario();
export const PendingFailure: Story = { args: { service: pendingSaveService }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const prefer = await canvas.findByRole("textbox", { name: "Prefer" });
  await userEvent.clear(prefer);
  await userEvent.type(prefer, "Keep this guidance");
  await userEvent.click(canvas.getByRole("button", { name: "Save changes" }));
  await expect(canvas.getByRole("button", { name: "Save changes" })).toBeDisabled();
  pendingSaveService.profileUpdate.calls[0]?.response.reject(new Error("Connection interrupted"));
  await expect(await canvas.findByText("Could not save preference profile")).toBeVisible();
  await expect(prefer).toHaveValue("Keep this guidance");
  await expect(pendingSaveService.summaryGeneration.calls).toHaveLength(0);
} };
