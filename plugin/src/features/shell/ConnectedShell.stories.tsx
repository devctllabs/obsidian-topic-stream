import { memoryWorkspaceState } from "../../test/storybook/local-state";
import { memoryFeedPreferences } from "../../test/storybook/feed-preferences";
import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, userEvent, within } from "storybook/test";
import { FixtureScenario } from "../../test/storybook/scenario";
import { deferred } from "../../test/storybook/deferred";
import { ConnectedShell } from "./ConnectedShell";
import { ShellNavigation } from "./navigation";
import { ViewErrorBoundary } from "./ViewErrorBoundary";

function ConnectedPreview({ state = "loaded" }: { state?: "loaded" | "loading" | "empty" | "connection-error" | "refresh-error" }) {
  const [navigation] = useState(() => new ShellNavigation());
  const [client] = useState(() => {
    const scenario = new FixtureScenario();
    if (state === "loading") scenario.feedResponse = deferred();
    if (state === "empty") scenario.collections.entries = [];
    const read = scenario.getFeed;
    let requests = 0;
    scenario.getFeed = (request) => {
      requests += 1;
      if ((state === "connection-error" && requests <= 2) || (state === "refresh-error" && requests > 1)) return Promise.reject(new Error("The backend is unavailable."));
      return read(request);
    };
    return scenario;
  });
  const [localState] = useState(memoryWorkspaceState);
  const [feedPreferences] = useState(memoryFeedPreferences);
  return <ConnectedShell notify={client.notify} localState={localState} client={client} navigation={navigation} feedPreferences={feedPreferences} />;
}

const meta = { title: "Features/Shell/ConnectedShell", component: ConnectedPreview, tags: ["!dev", "!autodocs"], parameters: { shell: true } } satisfies Meta<typeof ConnectedPreview>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Loaded: Story = {};
export const Loading: Story = { args: { state: "loading" } };
export const Empty: Story = { args: { state: "empty" } };
export const ConnectionError: Story = { args: { state: "connection-error" }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await expect(await canvas.findByText("Feed unavailable")).toBeVisible();
  await userEvent.click(canvas.getByRole("button", { name: "Try again" }));
  await expect((await canvas.findAllByRole("button", { name: /A closer look/ }))[0]).toBeVisible();
} };
export const RefreshError: Story = { args: { state: "refresh-error" }, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  const row = (await canvas.findAllByRole("button", { name: /A closer look/ }))[0];
  if (!row) throw new Error("Missing loaded item");
  await userEvent.click(row);
  await userEvent.click(canvas.getByRole("button", { name: "Refresh feed" }));
  await expect(await canvas.findByText("Refresh failed. Showing saved Feed.")).toBeVisible();
  await expect(row).toBeVisible();
  await expect(canvas.getByRole("heading", { level: 1 })).toBeVisible();
} };

function RenderFailureProbe() {
  const [failed, setFailed] = useState(false);
  if (failed) throw new Error("Synthetic render failure");
  return <button type="button" onClick={() => setFailed(true)}>Trigger render failure</button>;
}
export const RenderRecovery: Story = { render: () => <ViewErrorBoundary><RenderFailureProbe /></ViewErrorBoundary>, play: async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await userEvent.click(canvas.getByRole("button", { name: "Trigger render failure" }));
  await expect(canvas.getByRole("alert")).toBeVisible();
  await userEvent.click(canvas.getByRole("button", { name: "Try again" }));
  await expect(canvas.getByRole("button", { name: "Trigger render failure" })).toBeVisible();
} };
