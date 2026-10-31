import type { InfiniteData, QueryClient } from "@tanstack/react-query";
import type { FeedPage, Item, Schema } from "../../api/contracts";

/** Item state is live across scoped traversals; publication order and tokens stay untouched. */
export function patchItemState(queries: QueryClient, id: string, update: Partial<Item>): void {
  for (const scope of ["feed", "search"]) queries.setQueriesData<InfiniteData<Pick<FeedPage, "items">>>({ queryKey: [scope] }, (data) => data && ({ ...data,
    pages: data.pages.map((page) => ({ ...page, items: page.items.map((entry) => entry.item.id === id ? { ...entry, item: { ...entry.item, ...update } } : entry) }))
  }));
  const personal = Object.fromEntries(Object.entries(update).filter(([name]) => ["readingState", "feedback", "visibility"].includes(name)));
  queries.setQueriesData<Schema<"DigestIssue">>({ queryKey: ["digest-issue"] }, (issue) => issue && ({ ...issue,
    sections: issue.sections.map((section) => ({ ...section, items: section.items.map((saved) => saved.item.id === id ? { ...saved, item: { ...saved.item, ...personal } } : saved) }))
  }));
}
