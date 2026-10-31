import type { InfiniteData, QueryClient } from "@tanstack/react-query";
import type { FeedPage } from "../../api/contracts";
import { FEED_PAGE_SIZE } from "../../api/url";
import { feedPageParams } from "./refresh-feed";
import type { FeedService } from "./services";
import { reconcilePageFeedback } from "./item-feedback";

/** Membership and canonical-state reads retain each open traversal's original head. */
export async function reconcileFeedTraversals(queries: QueryClient, client: Pick<FeedService, "getFeed">): Promise<void> {
  const saved = queries.getQueriesData<InfiniteData<FeedPage>>({ queryKey: ["feed"] });
  await Promise.all(saved.map(async ([key, previous]) => {
    const snapshot = previous?.pages[0]?.snapshot;
    if (!previous || !snapshot) return;
    const topicId = typeof key[1] === "string" ? key[1] : null;
    const scope = topicId ? { topicId } : {};
    const pages = [await client.getFeed({ limit: FEED_PAGE_SIZE, cursor: null, snapshot, ...scope })];
    while (pages.length < previous.pages.length) {
      const cursor = pages.at(-1)!.nextCursor;
      if (cursor === null) break;
      pages.push(await client.getFeed({ limit: FEED_PAGE_SIZE, cursor, snapshot, ...scope }));
    }
    // A newer Refresh or a disposed connection owns its result; a late read cannot replace it.
    if (queries.getQueryData(key) !== previous) return;
    const pageParams = feedPageParams(pages, topicId).map((param) => ({ ...param, snapshot }));
    queries.setQueryData(key, { pages, pageParams });
    reconcilePageFeedback(queries, pages);
  }));
}
