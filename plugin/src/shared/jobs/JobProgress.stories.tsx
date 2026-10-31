import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, within } from "storybook/test";
import { JobProgress } from "./JobProgress";
import { FIXTURE_TIME, makeJob } from "../../test/storybook/fixtures";
const meta = { title: "Shared/Jobs/JobProgress", component: JobProgress, args: { job: makeJob({ progress: { phase: "indexing", processedItems: 35, totalItems: 100, updatedAt: FIXTURE_TIME } }) } } satisfies Meta<typeof JobProgress>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Loaded: Story = {};
export const UnknownTotal: Story = { args: { job: makeJob({ progress: { phase: "preparing", processedItems: 0, totalItems: null, updatedAt: FIXTURE_TIME } }) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await expect(canvas.getByText("0 Items · total not known yet")).toBeVisible();
  await expect(canvas.getByRole("progressbar")).not.toHaveAttribute("value");
} };
export const EmptyCohort: Story = { args: { job: makeJob({ progress: { phase: "activating", processedItems: 0, totalItems: 0, updatedAt: FIXTURE_TIME } }) }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await expect(canvas.getByText("0 / 0 Items")).toBeVisible();
  await expect(canvas.getByText("Running")).toBeVisible();
} };
