import { QueryClient } from "@tanstack/react-query";
import { expect, it } from "vitest";
import { patchItemState } from "./cache";
import { makeEntry, makeIssue } from "../../test/storybook/fixtures";

it("updates personal state across saved editions without replacing captured content or source identity", () => {
  const queries = new QueryClient();
  const entry = makeEntry();
  const issue = makeIssue([entry]);
  queries.setQueryData(["digest-issue", issue.id], issue);
  queries.setQueryData(["feed", "all"], { pages: [{ items: [entry], snapshot: "unchanged" }], pageParams: [null] });
  patchItemState(queries, entry.item.id, { ...entry.item, title: "The newer canonical title", contentMarkdown: "Changed canonical source", summarySourceVersion: "new-source-version", readingState: "read" });
  const saved = queries.getQueryData<typeof issue>(["digest-issue", issue.id])!.sections[0]!.items[0]!;
  expect(saved.item.title).toBe(entry.item.title);
  expect(saved.item.contentMarkdown).toBe(entry.item.contentMarkdown);
  expect(saved.item.summarySourceVersion).toBe(entry.item.summarySourceVersion);
  expect(saved.item.readingState).toBe("read");
  expect(saved.explanation).toEqual(entry.explanation);
  expect(queries.getQueryData<{ pages: { snapshot: string }[] }>(["feed", "all"])!.pages[0]!.snapshot).toBe("unchanged");
});
