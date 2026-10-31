import type { FeedPage } from "../../api/contracts";
import type { FeedPageRequest } from "../../api/url";
import { FEED_PAGE_SIZE } from "../../api/url";
import type { FeedService } from "./services";

/** Compare public pages at the saved snapshot; opaque tokens never become client ordering data. */
export async function refreshFeedTraversal(client: Pick<FeedService, "refreshFeed" | "getFeed">, topicId: string | null, previous: FeedPage[]): Promise<{ pages: FeedPage[]; hasNew: boolean }> {
  const scope = topicId ? { topicId } : {};
  const first = await client.refreshFeed({ limit: FEED_PAGE_SIZE, ...scope });
  const savedHead = previous[0];
  if (!savedHead) return { pages: [first], hasNew: first.items.length > 0 };
  const baseline = await client.getFeed({ limit: FEED_PAGE_SIZE, cursor: null, snapshot: savedHead.snapshot, ...scope });
  const priorIds = new Set(baseline.items.map(({ item }) => item.id));
  const hasNew = first.items.some(({ item }) => !priorIds.has(item.id));
  if (hasNew) return { pages: [first], hasNew: true };
  const pages = [first];
  while (pages.length < previous.length) {
    const cursor = pages.at(-1)!.nextCursor;
    if (cursor === null) break;
    pages.push(await client.getFeed({ limit: FEED_PAGE_SIZE, cursor, snapshot: first.snapshot, ...scope }));
  }
  return { pages, hasNew: false };
}

export function feedPageParams(pages: FeedPage[], topicId: string | null): FeedPageRequest[] {
  return pages.map((_page, index) => ({ limit: FEED_PAGE_SIZE, ...(topicId ? { topicId } : {}), cursor: index === 0 ? null : pages[index - 1]!.nextCursor, snapshot: index === 0 ? null : pages[0]!.snapshot }));
}
