import { skipToken, useQuery, type InfiniteData, type QueryClient } from "@tanstack/react-query";
import type { FeedPage } from "../../api/contracts";

type Membership = ReadonlyMap<string | null, ReadonlySet<string>>;
const key = ["feed-unread-retention"];
const empty: Membership = new Map();

export function useUnreadMembership(): Membership {
  return useQuery<Membership>({ queryKey: key, queryFn: skipToken, gcTime: Infinity, initialData: empty }).data ?? empty;
}

/** Capture loaded unread membership before another context changes canonical Reading State. */
export function retainFeedUnread(queries: QueryClient): void {
  const next = new Map(queries.getQueryData<Membership>(key));
  for (const [scopeKey, data] of queries.getQueriesData<InfiniteData<FeedPage>>({ queryKey: ["feed"] })) {
    const scope = typeof scopeKey[1] === "string" ? scopeKey[1] : null;
    const ids = data?.pages.flatMap((page) => page.items.filter(({ item }) => item.readingState === "unread").map(({ item }) => item.id)) ?? [];
    next.set(scope, new Set([...(next.get(scope) ?? []), ...ids]));
  }
  queries.setQueryData(key, next);
}

export function clearFeedUnread(queries: QueryClient, scope: string | null): void {
  const next = new Map(queries.getQueryData<Membership>(key));
  next.delete(scope);
  queries.setQueryData(key, next);
}
