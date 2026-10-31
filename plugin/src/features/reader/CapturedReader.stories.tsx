import { useEffect, useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent, within } from "storybook/test";
import { ConnectedItemCard } from "../feed/FeedCard";
import { ReaderBody } from "./ReaderBody";
import { ReaderImages } from "./images";
import { ItemSummaries } from "./item-summaries";
import { summaryCapability } from "./SummaryBody";
import { JobActions } from "../../shared/jobs/job-actions";
import { fixtureMarkdown } from "../../test/storybook/markdown";
import { interactiveScenario } from "../../test/storybook/interactive-scenario";
import { fixtureId, makeEntry, makeSummary } from "../../test/storybook/fixtures";

const source = makeEntry(0, { contentMarkdown: "The latest canonical article must never replace a saved Issue's text." });
const saved = { item: { ...source.item, summarySourceVersion: "saved-text-v1", contentMarkdown: "This exact article text was captured with the Digest Issue." }, summary: "The saved edition excerpt.", explanation: source.explanation, aiSummary: makeSummary({ sourceVersion: "saved-text-v1", state: "ready", canGenerate: false, unavailableReason: "historical_input", result: { ...makeSummary({ state: "ready", sourceVersion: "saved-text-v1" }).result!, text: "Annotation for the saved article version." } }) };
const client = Object.assign(interactiveScenario(), { getItemSummary: fn(async () => makeSummary({ state: "ready", result: { ...makeSummary({ state: "ready" }).result!, text: "Annotation for the latest canonical version." } })), setReadingState: fn(async () => undefined), requestItemSummary: fn(async () => ({ jobId: fixtureId(401) })) });

/** Consumer probe for 08; every visible Item/Reader/Summary component is production code. */
function CapturedReaderProbe() {
  const queries = useQueryClient();
  const images = useMemo(() => new ReaderImages(client), []);
  const jobs = useMemo(() => new JobActions(client), []);
  const summaries = useMemo(() => new ItemSummaries(queries, client, jobs), [queries, jobs]);
  useEffect(() => { const close = summaries.connect(); return () => { close(); images.dispose(); jobs.dispose(); }; }, [summaries, images, jobs]);
  const capabilities = (captured: boolean) => ({ summary: summaryCapability(summaries, captured ? { issueId: fixtureId(500), captured: saved } : undefined), renderReader: (item: typeof source.item) => <ReaderBody item={item} renderer={fixtureMarkdown} images={images} /> });
  return <><section aria-label="Saved Item"><ConnectedItemCard client={client} entry={{ ...source, item: saved.item }} details={false} topicId={null} onDetails={() => undefined} onReadingState={() => undefined} onResult={() => undefined} capabilities={capabilities(true)} /></section>
    <section aria-label="Canonical Item"><ConnectedItemCard client={client} entry={source} details={false} topicId={null} onDetails={() => undefined} onReadingState={() => undefined} onResult={() => undefined} capabilities={capabilities(false)} /></section></>;
}
const meta = { title: "Features/Reader/Views/CapturedReader", component: CapturedReaderProbe, tags: ["!dev", "!autodocs"] } satisfies Meta<typeof CapturedReaderProbe>;
export default meta;
type Story = StoryObj<typeof meta>;
export const ExactSavedInput: Story = { play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const capture = within(canvas.getByRole("region", { name: "Saved Item" }));
  const canonical = within(canvas.getByRole("region", { name: "Canonical Item" }));
  await expect(await capture.findByText(saved.item.contentMarkdown)).toBeVisible();
  await userEvent.click(await capture.findByRole("button", { name: "AI summary" }));
  await expect(await capture.findByText("Annotation for the saved article version.")).toBeVisible();
  await expect(canonical.queryByText("Annotation for the latest canonical version.")).not.toBeInTheDocument();
  await userEvent.click(await canonical.findByRole("button", { name: "AI summary" }));
  await expect(await canonical.findByText("Annotation for the latest canonical version.")).toBeVisible();
  await expect(client.getItemSummary).toHaveBeenCalledTimes(1);
  await expect(client.getItemSummary).toHaveBeenCalledWith(source.item.id, source.item.summarySourceVersion);
  await expect(client.requestItemSummary).not.toHaveBeenCalled();
  await expect(client.setReadingState).not.toHaveBeenCalled();
} };
