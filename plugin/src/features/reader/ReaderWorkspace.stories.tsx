import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent, waitFor, within } from "storybook/test";
import { ConnectedShell } from "../shell/ConnectedShell";
import { ShellNavigation } from "../shell/navigation";
import { interactiveScenario } from "../../test/storybook/interactive-scenario";
import { memoryWorkspaceState } from "../../test/storybook/local-state";
import { memoryFeedPreferences } from "../../test/storybook/feed-preferences";
import { fixtureId, makeEntry } from "../../test/storybook/fixtures";
import type { MarkdownRendererPort } from "./markdown";
import { fixtureMarkdown } from "../../test/storybook/markdown";
import { deferred } from "../../test/storybook/deferred";
import { fixtureImage } from "../../test/storybook/images";
import type { Item, ItemImageDownload } from "../../api/contracts";

const client = interactiveScenario();
client.collections.entries = [makeEntry(0, { contentMarkdown: "A canonical paragraph available only in the full Reader." })];
const renderMarkdown = fn<MarkdownRendererPort["render"]>(fixtureMarkdown.render);
const args = { client, navigation: new ShellNavigation({ primary: "search", screen: "search" }), localState: memoryWorkspaceState(), notify: client.notify, feedPreferences: memoryFeedPreferences(), markdown: { render: renderMarkdown } };
const meta = { title: "Features/Reader/Views/ReaderWorkspace", component: ConnectedShell, args, parameters: { shell: true } } satisfies Meta<typeof ConnectedShell>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Loaded: Story = { play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /^A closer look/ }));
  await expect(await canvas.findByText("A canonical paragraph available only in the full Reader.")).toBeVisible();
  await expect(renderMarkdown).toHaveBeenCalledTimes(1);
  await expect(canvas.getByRole("heading", { name: "A closer look at retrieval systems · 1" })).toBeVisible();
} };

const emptyClient = interactiveScenario();
emptyClient.collections.entries = [makeEntry(0, { contentMarkdown: "", contentOrigin: "empty", summarySupported: false })];
const emptyRenderer = fn<MarkdownRendererPort["render"]>(fixtureMarkdown.render);
export const EmptyContent: Story = { args: { ...args, client: emptyClient, navigation: new ShellNavigation({ primary: "search", screen: "search" }), markdown: { render: emptyRenderer } }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /^A closer look/ }));
  await expect(canvas.getByText("Full content is unavailable for this Item.")).toBeVisible();
  const pane = within(canvas.getByRole("region", { name: "Reading pane" }));
  await expect(pane.getByText(emptyClient.collections.entries[0]!.item.excerpt)).toBeVisible();
  await expect(pane.getByRole("heading", { name: "A closer look at retrieval systems · 1" })).toBeVisible();
  await expect(emptyRenderer).not.toHaveBeenCalled();
} };

const recoverContent = fn<(id: string) => Promise<Item>>(async () => ({ ...emptyClient.collections.entries[0]!.item, contentMarkdown: "Recovered canonical source text.", contentOrigin: "article_body" }));
const recoverClient = Object.assign(interactiveScenario(), { getItem: recoverContent });
recoverClient.collections.entries = structuredClone(emptyClient.collections.entries);
export const ContentRecovery: Story = { args: { ...args, client: recoverClient, navigation: new ShellNavigation({ primary: "search", screen: "search" }) },
  beforeEach: () => { recoverContent.mockRejectedValueOnce(new Error("The content service is temporarily unavailable.")); }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /^A closer look/ }));
  await expect(recoverContent).not.toHaveBeenCalled();
  await userEvent.click(canvas.getByRole("button", { name: "Check for full content" }));
  await expect(await canvas.findByRole("alert")).toHaveTextContent("Could not reload this Item");
  await expect(canvas.getByRole("heading", { name: /^A closer look/ })).toBeVisible();
  await userEvent.click(canvas.getByRole("button", { name: "Check for full content" }));
  await expect(await canvas.findByText("Recovered canonical source text.")).toBeVisible();
  await expect(recoverContent).toHaveBeenCalledTimes(2);
} };

const errorRenderer = fn<MarkdownRendererPort["render"]>(fixtureMarkdown.render);
const releaseFailed = fn();
export const RenderError: Story = { args: { ...args, navigation: new ShellNavigation({ primary: "search", screen: "search" }), markdown: { render: errorRenderer } },
  beforeEach: () => { errorRenderer.mockImplementationOnce(() => ({ ready: Promise.reject(new Error("Host rendering failed.")), dispose: releaseFailed })); }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /^A closer look/ }));
  await expect(await canvas.findByRole("alert")).toHaveTextContent("Could not display this Item");
  await expect(releaseFailed).toHaveBeenCalledTimes(1);
  await userEvent.click(canvas.getByRole("button", { name: "Try again" }));
  await expect(await canvas.findByText("A canonical paragraph available only in the full Reader.")).toBeVisible();
  await expect(errorRenderer).toHaveBeenCalledTimes(2);
  await expect(releaseFailed).toHaveBeenCalledTimes(1);
} };
export const RenderUnavailable: Story = { ...RenderError, beforeEach: () => { errorRenderer.mockImplementation(() => ({ ready: Promise.reject(new Error("Host rendering failed.")), dispose: releaseFailed })); }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /^A closer look/ }));
  await expect(await canvas.findByRole("alert")).toHaveTextContent("Could not display this Item");
  await expect(canvas.getByRole("button", { name: "Try again" })).toBeEnabled();
} };

const lateRendererClient = interactiveScenario();
lateRendererClient.collections.entries = [makeEntry(0, { contentMarkdown: "The first source paragraph." }), makeEntry(1, { contentMarkdown: "The second source remains readable." })];
const lateReady = deferred<void>();
const releaseLate = fn();
const lateRenderer = fn<MarkdownRendererPort["render"]>(fixtureMarkdown.render);
export const PendingRendererCleanup: Story = { args: { ...args, client: lateRendererClient, navigation: new ShellNavigation({ primary: "search", screen: "search" }), markdown: { render: lateRenderer } }, parameters: { frame: "mobile" },
  beforeEach: () => { lateRenderer.mockImplementationOnce((markdown, target) => { const native = fixtureMarkdown.render(markdown, target); return { ready: lateReady.promise, dispose: () => { native.dispose(); releaseLate(); } }; }); }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /^A closer look/ }));
  await expect(await canvas.findByText("Loading article…")).toBeVisible();
  await userEvent.click(canvas.getByRole("button", { name: "Back to list" }));
  await expect(releaseLate).toHaveBeenCalledTimes(1);
  await userEvent.click(canvas.getByRole("button", { name: /^Notas de ingeniería/ }));
  await expect(await canvas.findByText("The second source remains readable.")).toBeVisible();
  lateReady.resolve();
  await waitFor(() => expect(canvas.queryByText("Loading article…")).not.toBeInTheDocument());
  await expect(canvas.getByText("The second source remains readable.")).toBeVisible();
  await expect(canvas.queryByText("The first source paragraph.")).not.toBeInTheDocument();
  await expect(releaseLate).toHaveBeenCalledTimes(1);
} };

const firstImage = { id: fixtureId(800), url: "https://images.example/first.svg", altText: "First diagram" };
const secondImage = { id: fixtureId(801), url: "https://images.example/second.svg", altText: "Second diagram" };
const thirdImage = { id: fixtureId(802), url: "https://images.example/third.svg", altText: "Third diagram" };
const pendingImages = deferred<ItemImageDownload>();
const downloadImages = fn(async (_itemId: string, _imageId: string) => pendingImages.promise);
const imageClient = Object.assign(interactiveScenario(), { downloadItemImage: downloadImages });
imageClient.collections.entries = [
  makeEntry(0, { contentMarkdown: `Readable article before images.\n\n![First diagram](${firstImage.url})\n\n![Second diagram](${secondImage.url})`, images: [firstImage, secondImage] }),
  makeEntry(1, { contentMarkdown: `Another Item keeps its own consent.\n\n![Third diagram](${thirdImage.url})`, images: [thirdImage] })
];
export const ImageConsent: Story = { args: { ...args, client: imageClient, navigation: new ShellNavigation({ primary: "search", screen: "search" }), markdown: fixtureMarkdown }, parameters: { frame: "mobile" }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /^A closer look/ }));
  const first = await canvas.findByRole("button", { name: /First diagram.*Load all images in this Item/ });
  await expect(downloadImages).not.toHaveBeenCalled();
  first.focus(); await userEvent.keyboard("{Enter}");
  await waitFor(() => expect(downloadImages).toHaveBeenCalledTimes(2));
  await expect(downloadImages).toHaveBeenCalledWith(fixtureId(300), firstImage.id);
  await expect(downloadImages).toHaveBeenCalledWith(fixtureId(300), secondImage.id);
  await expect(canvas.getByText("Readable article before images.")).toBeVisible();
  pendingImages.resolve(fixtureImage());
  const loaded = await canvas.findByRole("img", { name: "First diagram" });
  await waitFor(() => expect((loaded as HTMLImageElement).naturalWidth).toBeGreaterThan(0));
  await expect(canvas.getByRole("img", { name: "Second diagram" })).toBeVisible();
  await userEvent.click(canvas.getByRole("button", { name: "Back to list" }));
  await userEvent.click(canvas.getByRole("button", { name: /^Notas de ingeniería/ }));
  await expect(await canvas.findByRole("button", { name: /Third diagram.*Load all images in this Item/ })).toBeVisible();
  await expect(downloadImages).toHaveBeenCalledTimes(2);
  await userEvent.click(canvas.getByRole("button", { name: "Back to list" }));
  await userEvent.click(canvas.getByRole("button", { name: /^A closer look/ }));
  await expect(await canvas.findByRole("img", { name: "First diagram" })).toBeVisible();
  await expect(canvas.queryByRole("button", { name: /Load all images in this Item/ })).not.toBeInTheDocument();
  await expect(downloadImages).toHaveBeenCalledTimes(2);
} };

export const BlockedImages: Story = { args: { ...args, client: imageClient, navigation: new ShellNavigation({ primary: "search", screen: "search" }), markdown: fixtureMarkdown }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /^A closer look/ }));
  await expect(await canvas.findByRole("button", { name: /First diagram.*Load all images/ })).toBeVisible();
  await expect(downloadImages).not.toHaveBeenCalled();
} };
const pendingImageClient = Object.assign(interactiveScenario(), { downloadItemImage: fn(async (_itemId: string, _imageId: string) => new Promise<ItemImageDownload>(() => undefined)) });
pendingImageClient.collections.entries = structuredClone(imageClient.collections.entries);
export const ImagesLoading: Story = { args: { ...args, client: pendingImageClient, navigation: new ShellNavigation({ primary: "search", screen: "search" }), markdown: fixtureMarkdown }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /^A closer look/ }));
  await userEvent.click(await canvas.findByRole("button", { name: /First diagram.*Load all images/ }));
  await expect(await canvas.findAllByText("Loading image…")).toHaveLength(2);
  await expect(pendingImageClient.downloadItemImage).toHaveBeenCalledTimes(2);
} };

const partialClient = Object.assign(interactiveScenario(), { downloadItemImage: fn(async (_itemId: string, _imageId: string) => fixtureImage()) });
partialClient.collections.entries = [makeEntry(0, { contentMarkdown: imageClient.collections.entries[0]!.item.contentMarkdown, images: [firstImage, secondImage] })];
export const PartialImageFailure: Story = { args: { ...args, client: partialClient, navigation: new ShellNavigation({ primary: "search", screen: "search" }), markdown: fixtureMarkdown },
  beforeEach: () => { partialClient.downloadItemImage.mockResolvedValueOnce(fixtureImage()).mockRejectedValueOnce(new Error("The second image is unavailable.")); }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /^A closer look/ }));
  await userEvent.click(await canvas.findByRole("button", { name: /First diagram.*Load all images in this Item/ }));
  const first = await canvas.findByRole("img", { name: "First diagram" });
  await expect(await canvas.findByRole("alert")).toHaveTextContent("The second image is unavailable.");
  await expect(canvas.getByText("Readable article before images.")).toBeVisible();
  await userEvent.click(canvas.getByRole("button", { name: "Retry image: Second diagram" }));
  const second = await canvas.findByRole("img", { name: "Second diagram" });
  await waitFor(() => expect((second as HTMLImageElement).naturalWidth).toBeGreaterThan(0));
  await expect(first).toBeInTheDocument();
  await expect(partialClient.downloadItemImage).toHaveBeenCalledTimes(3);
  await expect(canvas.queryByRole("alert")).not.toBeInTheDocument();
} };
export const ImageUnavailable: Story = { ...PartialImageFailure, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /^A closer look/ }));
  await userEvent.click(await canvas.findByRole("button", { name: /First diagram.*Load all images/ }));
  await expect(await canvas.findByRole("alert")).toHaveTextContent("The second image is unavailable.");
  await expect(canvas.getByRole("img", { name: "First diagram" })).toBeVisible();
} };

const longClient = interactiveScenario();
longClient.collections.entries = [makeEntry(0, { title: "Reading a long source without losing the question", contentMarkdown: "# A source heading inside the article\n\n" + Array.from({ length: 25 }, (_, index) => `## Observation ${index + 1}\n\nA useful question makes room for context. Read the original argument carefully, compare its assumptions, and keep the next experiment small.\n\nUna colección útil conserva las preguntas y conecta las ideas con la práctica.`).join("\n\n") + "\n\n```text\n" + "a-long-source-identifier-".repeat(30) + "\n```" })];
export const LongMarkdown: Story = { args: { ...args, client: longClient, navigation: new ShellNavigation({ primary: "search", screen: "search" }), markdown: fixtureMarkdown }, parameters: { frame: "mobile", fontScale: 2 }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(await canvas.findByRole("button", { name: /^Reading a long source/ }));
  const title = canvas.getByRole("heading", { name: "Reading a long source without losing the question" });
  const heading = await canvas.findByRole("heading", { name: "A source heading inside the article" });
  await expect(Number.parseFloat(getComputedStyle(heading).fontSize)).toBeLessThan(Number.parseFloat(getComputedStyle(title).fontSize));
  const pane = canvasElement.querySelector<HTMLElement>(".topic-stream-detail-pane")!;
  await expect(pane.scrollWidth).toBeLessThanOrEqual(pane.clientWidth);
  await expect(pane.scrollHeight).toBeGreaterThan(pane.clientHeight);
} };
