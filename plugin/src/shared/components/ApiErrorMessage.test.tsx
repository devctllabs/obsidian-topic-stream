import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { httpError } from "../../api/errors";
import { ApiErrorMessage } from "./ApiErrorMessage";

describe("ApiErrorMessage", () => {
  it("shows the safe detail and every field issue as plain text", () => {
    const error = httpError(400, JSON.stringify({
      type: "urn:topic-stream:problem:invalid-source", code: "invalid_source", title: "Invalid source", status: 400,
      detail: "Check the source fields.", errors: [{ field: "name", message: "This field is required." }, { field: "config.url", message: "<script>unsafe</script>" }]
    }));
    const html = renderToStaticMarkup(<ApiErrorMessage error={error} title="Could not add source" />);
    expect(html).toContain('role="alert"');
    expect(html).toContain("Check the source fields.");
    expect(html).toContain("name");
    expect(html).toContain("This field is required.");
    expect(html).toContain("config.url");
    expect(html).toContain("&lt;script&gt;");
    expect(html).not.toContain("<script>");
  });

  it("shows that a pending projection has already saved the change", () => {
    const error = httpError(503, JSON.stringify({
      type: "urn:topic-stream:problem:projection-pending", code: "projection_pending", title: "Retrieval projection pending", status: 503,
      detail: "The canonical change was saved, but its retrieval projection is still pending.", canonicalCommitted: true, projectionRevision: "revision"
    }));
    const html = renderToStaticMarkup(<ApiErrorMessage error={error} title="Could not save feedback" />);
    expect(html).toContain("Change saved");
    expect(html).not.toContain("Could not save feedback");
    expect(html).toContain("was saved");
  });

  it("handles a network error without inventing server facts", () => {
    const html = renderToStaticMarkup(<ApiErrorMessage error={new Error("Could not connect to Topic Stream.")} title="Feed unavailable" />);
    expect(html).toContain("Could not connect to Topic Stream.");
    expect(html).not.toContain("<ul>");
  });
});
