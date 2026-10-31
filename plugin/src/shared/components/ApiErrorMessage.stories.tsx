import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, within } from "storybook/test";

import { httpError } from "../../api/errors";
import { ApiErrorMessage } from "./ApiErrorMessage";

const meta = {
  title: "Shared/Feedback/ApiErrorMessage", component: ApiErrorMessage,
  args: { title: "Could not save", error: new Error("Could not connect to Topic Stream.") },
  tags: ["autodocs"]
} satisfies Meta<typeof ApiErrorMessage>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};
export const ValidationErrors: Story = { args: { error: httpError(400, JSON.stringify({
  type: "urn:topic-stream:problem:invalid-source", title: "Invalid source", status: 400,
  detail: "Check the source fields.", errors: [{ field: "name", message: "This field is required." }]
})) } };
export const ProjectionPending: Story = {
  args: { error: httpError(503, JSON.stringify({
    type: "urn:topic-stream:problem:projection-pending", title: "Retrieval projection pending", status: 503,
    code: "projection_pending", canonicalCommitted: true, projectionRevision: "fixture-revision",
    detail: "Your change was saved. Retrieval updates are pending."
  })) },
  play: async ({ canvasElement }) => {
    await expect(within(canvasElement).getByRole("alert")).toHaveTextContent("Change saved");
  }
};
