import type { InfiniteData, QueryClient } from "@tanstack/react-query";
import type { Schema } from "../../api/contracts";
export function storeJob(queries: QueryClient, job: Schema<"Job">): void {
  queries.setQueryData(["job", job.id], job);
  queries.setQueriesData<InfiniteData<Schema<"JobList">>>({ queryKey: ["jobs"] }, (data) => data && ({ ...data, pages: data.pages.map((page) => ({ ...page, items: page.items.map((value) => value.id === job.id ? job : value) })) }));
}
