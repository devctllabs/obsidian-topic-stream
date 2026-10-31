import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, userEvent, waitFor, within } from "storybook/test";

import { FixtureProbe } from "./FixtureProbe";

const meta = {
  title: "Shared/Testing/FixtureProbe", component: FixtureProbe,
  args: { mode: "feed", state: "loaded", itemCount: 30, disabled: false, longTitle: false },
  parameters: { docs: { description: { component: "Synthetic harness composition. Feature owners provide the production UI and its behavior stories." } } },
  tags: ["!dev", "!autodocs"]
} satisfies Meta<typeof FixtureProbe>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Loaded: Story = {
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await expect(canvas.getByRole("heading", { level: 1 })).toBeVisible();
    await userEvent.click(canvas.getByText("Test controls"));
    await expect(canvas.getByText("Not requested · 0 advisories")).toBeVisible();
    const trigger = canvas.getByRole("button", { name: "Open fixture details" });
    trigger.focus();
    await userEvent.keyboard("{Enter}");
    await expect(canvas.getByRole("dialog")).toBeVisible();
    await userEvent.keyboard("{Escape}");
    await waitFor(() => expect(canvas.queryByRole("dialog")).not.toBeInTheDocument());
    await expect(trigger).toHaveFocus();
    await userEvent.click(canvas.getByRole("button", { name: "Request fixture work" }));
    await expect(canvas.getByText("Pending · 0 advisories")).toBeVisible();
    await userEvent.click(canvas.getByRole("button", { name: "Complete fixture work" }));
    await expect(canvas.getByText("Ready · 0 advisories")).toBeVisible();
    await userEvent.click(canvas.getByRole("button", { name: "Emit fixture advisory" }));
    await expect(canvas.getByText("Ready · 1 advisories")).toBeVisible();
  }
};
export const Loading: Story = { args: { state: "loading" } };
export const LoadError: Story = { args: { state: "error" } };
export const EmptyItems: Story = { args: { itemCount: 0 } };
export const OneItem: Story = { args: { itemCount: 1 } };
export const ManyItems: Story = { args: { itemCount: 50 } };
export const Disabled: Story = { args: { disabled: true, itemCount: 1 } };
export const LongContent: Story = { args: { longTitle: true, itemCount: 20 } };
export const ReaderLoaded: Story = { args: { mode: "reader", itemCount: 1 } };
export const DigestLoaded: Story = { args: { mode: "digest" } };
export const IssueContentsLoaded: Story = { args: { mode: "contents" } };
export const Pending: Story = { args: { itemCount: 1 }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(canvas.getByText("Test controls"));
  await userEvent.click(canvas.getByRole("button", { name: "Request fixture work" }));
  await expect(canvas.getByText("Pending · 0 advisories")).toBeVisible();
} };
export const ActionError: Story = { args: { itemCount: 1 }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(canvas.getByText("Test controls"));
  await userEvent.click(canvas.getByRole("button", { name: "Request fixture work" }));
  await userEvent.click(canvas.getByRole("button", { name: "Fail fixture work" }));
  await expect(canvas.getByText("Failed — retry is available · 0 advisories")).toBeVisible();
} };

export const NarrowGeometryRegression: Story = {
  args: { longTitle: true, itemCount: 5 }, parameters: { frame: "narrow", fontScale: 2 }, tags: ["!dev", "!autodocs"],
  play: async ({ canvasElement }) => {
    const leaf = canvasElement.querySelector<HTMLElement>(".workspace-leaf");
    await expect(leaf).not.toBeNull();
    if (!leaf) return;
    await expect(leaf.getBoundingClientRect().width).toBeLessThanOrEqual(460);
    await expect(leaf.scrollWidth).toBeLessThanOrEqual(leaf.clientWidth + 1);
  }
};
export const DarkMobileGeometryRegression: Story = {
  ...NarrowGeometryRegression, globals: { theme: "dark" }, parameters: { frame: "mobile" },
  play: async ({ canvasElement }) => {
    const leaf = canvasElement.querySelector<HTMLElement>(".workspace-leaf");
    await expect(leaf?.getBoundingClientRect().width).toBeLessThanOrEqual(360);
    const title = canvasElement.querySelector<HTMLElement>('[data-type-role="edition-title"]');
    const item = canvasElement.querySelector<HTMLElement>('[data-type-role="item-title"]');
    const topic = canvasElement.querySelector<HTMLElement>('[data-type-role="topic-label"]');
    if (!title || !item || !topic) throw new Error("Missing hierarchy fixture roles");
    await expect(parseFloat(getComputedStyle(title).fontSize)).toBeGreaterThan(parseFloat(getComputedStyle(item).fontSize));
    await expect(parseFloat(getComputedStyle(item).fontSize)).toBeGreaterThan(parseFloat(getComputedStyle(topic).fontSize));
  }
};
