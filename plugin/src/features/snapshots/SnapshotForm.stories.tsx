import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent, waitFor, within } from "storybook/test";
import { makeEntry, makeIssue, makeItem, makeSummary } from "../../test/storybook/fixtures";
import { memorySnapshotVault } from "../../test/storybook/snapshot-vault";
import { fixtureImage } from "../../test/storybook/images";
import { SnapshotForm } from "./SnapshotForm";
import { sanitizeSvg } from "./svg";

const memory = memorySnapshotVault();
const meta = { title: "Features/Snapshots/Modals/SnapshotForm", component: SnapshotForm,
  args: { source: { kind: "item", item: makeItem(), aiSummary: makeSummary({ state: "ready" }) }, services: { vault: memory.vault, images: { downloadItemImage: async () => fixtureImage() }, sanitizeSvg }, preferences: memory.preferences, onSaved: fn(), onCancel: fn() },
  beforeEach: async () => { memory.notes.clear(); await memory.preferences.save({ content: "compact", images: "links" }); },
  parameters: { frame: "narrow" }
} satisfies Meta<typeof SnapshotForm>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Loaded: Story = { play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await expect(canvas.getByLabelText("Folder")).toBeVisible();
  await expect(canvas.getByLabelText("Filename")).toBeVisible();
  await expect(canvas.getByRole("radio", { name: "Compact" })).toBeChecked();
  await expect(canvas.getByRole("radio", { name: "Compact" }).getBoundingClientRect().width).toBeLessThanOrEqual(20);
  await expect(canvas.queryByRole("radio", { name: "Local" })).not.toBeInTheDocument();
  await userEvent.click(canvas.getByRole("radio", { name: "Full" }));
  await expect(canvas.getByRole("radio", { name: "Local" })).toBeVisible();
} };
export const SaveCompact: Story = { play: async ({ canvasElement, args }) => {
  const canvas = within(canvasElement);
  await userEvent.type(canvas.getByLabelText("Folder"), "Reading//Weekly/");
  await userEvent.clear(canvas.getByLabelText("Filename")); await userEvent.type(canvas.getByLabelText("Filename"), "New snapshot");
  await userEvent.click(canvas.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(args.onSaved).toHaveBeenCalledWith({ path: "Reading/Weekly/New snapshot.md", localImages: 0, linkedImages: 0 }));
  await expect(memory.notes.get("Reading/Weekly/New snapshot.md")?.markdown).toContain("## AI summary");
} };
export const ValidationErrors: Story = { play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.clear(canvas.getByLabelText("Filename")); await userEvent.type(canvas.getByLabelText("Filename"), "../overwrite");
  await userEvent.click(canvas.getByRole("button", { name: "Save" }));
  await expect(await canvas.findByRole("alert")).toHaveTextContent("filename");
  await expect(memory.notes.size).toBe(0);
} };
export const FullUnavailable: Story = { args: { source: { kind: "item", item: makeItem(1, { contentOrigin: "article_description", summarySupported: false, contentMarkdown: "Description only" }), aiSummary: null } }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(canvas.getByRole("radio", { name: "Full" }));
  await expect(canvas.getByText(/This Item has no article body/)).toBeVisible();
  await expect(canvas.getByRole("button", { name: "Save" })).toBeDisabled();
  await expect(canvas.queryByRole("radio", { name: "Local" })).not.toBeInTheDocument();
  await expect(memory.notes.size).toBe(0);
} };
export const SwitchToCompact: Story = { ...FullUnavailable, tags: ["!dev", "!autodocs"], play: async (context) => {
  await FullUnavailable.play!(context);
  const canvas = within(context.canvasElement);
  await userEvent.click(canvas.getByRole("button", { name: "Use Compact" }));
  await expect(canvas.getByRole("radio", { name: "Compact" })).toBeChecked();
  await userEvent.click(canvas.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(context.args.onSaved).toHaveBeenCalled());
} };
const mixed = makeIssue([makeEntry(), makeEntry(1, { contentOrigin: "paper_abstract", summarySupported: false })]);
export const MixedDigest: Story = { args: { source: { kind: "digest_issue", issue: mixed } }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement); await userEvent.click(canvas.getByRole("radio", { name: "Full" }));
  await userEvent.click(canvas.getByRole("button", { name: "Save" }));
  await expect(await canvas.findByRole("alert")).toHaveTextContent("Confirm the mixed result");
  await expect(memory.notes.size).toBe(0);
} };
export const ConfirmMixedDigest: Story = { ...MixedDigest, tags: ["!dev", "!autodocs"], play: async (context) => {
  await MixedDigest.play!(context);
  const canvas = within(context.canvasElement);
  await userEvent.click(canvas.getByRole("checkbox", { name: "Include these excerpts in the Full snapshot" }));
  await userEvent.click(canvas.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(context.args.onSaved).toHaveBeenCalled());
  await expect([...memory.notes.values()][0]?.markdown).toContain("Excerpt only");
} };
export const CancelMixedDigest: Story = { ...MixedDigest, tags: ["!dev", "!autodocs"], play: async (context) => {
  await MixedDigest.play!(context); const canvas = within(context.canvasElement);
  await userEvent.click(canvas.getByRole("button", { name: "Cancel" }));
  await expect(context.args.onCancel).toHaveBeenCalled(); await expect(memory.notes.size).toBe(0);
} };
export const SaveError: Story = { args: { services: { ...meta.args.services, vault: { write: async () => { throw new Error("A file already exists. Choose a different filename."); } } } }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement); await userEvent.click(canvas.getByRole("button", { name: "Save" }));
  await expect(await canvas.findByRole("alert")).toHaveTextContent("A file already exists");
  await expect(canvas.getByRole("button", { name: "Save" })).toBeEnabled();
} };
export const SavePending: Story = { args: { services: { ...meta.args.services, vault: { write: (_plan, signal) => new Promise((_resolve, reject) => signal.addEventListener("abort", () => reject(new Error(String(signal.reason))), { once: true })) } } }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement); await userEvent.click(canvas.getByRole("button", { name: "Save" }));
  await expect(await canvas.findByRole("status")).toHaveTextContent("Preparing snapshot");
  await expect(canvas.getByRole("button", { name: "Saving…" })).toBeDisabled();
} };
const svgUrl = "https://example.org/captured-diagram.svg";
const malicious = '<svg xmlns="http://www.w3.org/2000/svg" onload="evil()" width="100" height="100"><script>evil()</script><foreignObject><iframe src="https://evil.invalid"/></foreignObject><style>@import url(https://evil.invalid)</style><image href="https://evil.invalid/x"/><use href="https://evil.invalid/x"/><a href="javascript:evil()"><rect width="20" height="20" fill="url(https://evil.invalid/x)"/></a><animate attributeName="href" values="javascript:evil()"/><circle cx="50" cy="50" r="20" fill="#f2b38a"/></svg>';
export const SanitizedLocalSvgRegression: Story = { tags: ["!dev", "!autodocs"], args: { source: { kind: "item", item: makeItem(0, { contentMarkdown: `<figure><img src="${svgUrl}" alt=""><figcaption>Captured diagram caption</figcaption></figure>`, images: [{ id: "saved-svg", url: svgUrl, altText: null }] }), aiSummary: null }, services: { ...meta.args.services, images: { downloadItemImage: async () => ({ bytes: new TextEncoder().encode(malicious).buffer, contentType: "image/svg+xml" }) } } }, play: async ({ canvasElement, args }) => {
  const canvas = within(canvasElement); await userEvent.click(canvas.getByRole("radio", { name: "Full" })); await userEvent.click(canvas.getByRole("radio", { name: "Local" }));
  await userEvent.click(canvas.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(args.onSaved).toHaveBeenCalledWith(expect.objectContaining({ localImages: 1, linkedImages: 0 })));
  const saved = [...memory.notes.values()][0]!;
  await expect(saved.markdown).toContain("![Captured diagram caption]");
  const clean = new TextDecoder().decode(saved.attachments[0]!.bytes);
  await expect(clean).toContain("<circle");
  await expect(clean).not.toMatch(/script|onload|foreignObject|iframe|<style|<image|<use|href|animate|url\(/i);
} };
export const Light: Story = { globals: { theme: "light" } };
export const Narrow: Story = { parameters: { frame: "narrow" } };
export const Mobile: Story = { parameters: { frame: "mobile" } };
export const LargeText: Story = { parameters: { frame: "mobile", fontScale: 2 }, play: async ({ canvasElement }) => {
  const dialog = within(canvasElement).getByRole("dialog"); await expect(dialog.scrollWidth).toBeLessThanOrEqual(dialog.clientWidth);
} };
