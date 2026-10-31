import { useInfiniteQuery, useIsMutating, useMutation, useQueryClient, type InfiniteData } from "@tanstack/react-query";
import type { FeedPage } from "../../api/contracts";
import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";

import type { FeedPageRequest } from "../../api/client";
import { FEED_PAGE_SIZE } from "../../api/url";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";

import { SplitPane } from "../shell/SplitPane";
import type { SectionProps } from "../shell/TopicStreamShell";
import { IconButton } from "../../shared/components/IconButton";
import { ConnectedItemCard } from "./FeedCard";
import { FeedMessage } from "./FeedMessage";
import { TopicSwitcher } from "./TopicSwitcher";
import { feedPageParams, refreshFeedTraversal } from "./refresh-feed";
import type { FeedService } from "./services";
import type { FeedPreferences } from "./preferences";
import { FeedRow } from "./FeedRow";
import { ItemActionListNotice, type ItemActionContext, type ItemActionResult } from "./item-actions";
import { clearFeedUnread, retainFeedUnread, useUnreadMembership } from "./unread-membership";
import { reconcilePageFeedback } from "./item-feedback";
import { useFeedUpdates } from "./feed-updates";
import type { ViewEvents } from "../../shared/events";
import type { FeedItemCapabilities } from "./capabilities";

interface FeedViewProps extends SectionProps { client: FeedService; feedPreferences: FeedPreferences; onReadingState: ItemActionContext["onReadingState"]; events?: ViewEvents | undefined; capabilities?: FeedItemCapabilities | undefined; }

export function FeedView({ client, feedPreferences, events, capabilities, onReadingState, ...props }: FeedViewProps) {
  const navigation = useSyncExternalStore(props.navigation.subscribe, props.navigation.getSnapshot);
  const sentinel = useRef<HTMLDivElement>(null);
  const [topicId, setTopicId] = useState<string | null>(null);
  const [detailsId, setDetailsId] = useState<string | null>(null);
  const [actionResult, setActionResult] = useState<ItemActionResult | null>(null);
  const filter = useSyncExternalStore(feedPreferences.subscribe, feedPreferences.getSnapshot);
  const queries = useQueryClient();
  const retained = useUnreadMembership();
  const retainUnread = useCallback(() => {
    if (filter !== "unread") return;
    retainFeedUnread(queries);
  }, [filter, queries]);
  const updates = useFeedUpdates(client, events, retainUnread);
  const selections = useRef(new Map<string | null, string | null>());
  const [heads, setHeads] = useState(new Map<string | null, { snapshot: string; revision: number }>());
  const { query, failure, refresh, refreshing } = useFeedPages(client, topicId, (selected, hasNew, snapshot) => {
    updates.clear(selected);
    clearFeedUnread(queries, selected);
    if (hasNew) {
      props.navigation.rememberPosition(`${feedPositionKey(selected)}:list`, 0);
      setHeads((current) => new Map(current).set(selected, { snapshot, revision: (current.get(selected)?.revision ?? 0) + 1 }));
    }
  });

  const { hasNextPage, isFetchingNextPage, fetchNextPage } = query;
  const entries = useMemo(() => query.data?.pages.flatMap((page) => page.items) ?? [], [query.data]);
  const visible = visibleEntries(entries, filter, retained.get(topicId));
  const autoLoad = visible.length > 0 && !query.isFetchNextPageError;
  useEffect(() => {
    const element = sentinel.current;
    const Observer = element?.ownerDocument.defaultView?.IntersectionObserver;
    if (element === null || !hasNextPage || Observer === undefined || refreshing || !autoLoad) {
      return;
    }
    const observer = new Observer(([entry]) => {
      if (entry?.isIntersecting === true && !isFetchingNextPage) {
        void fetchNextPage();
      }
    }, { rootMargin: "240px" });
    observer.observe(element);
    return () => observer.disconnect();
  }, [fetchNextPage, hasNextPage, isFetchingNextPage, refreshing, autoLoad]);
  const selected = entries.find((entry) => entry.item.id === navigation.selections[props.section]);
  const listRevision = feedRevision(query.data, heads.get(topicId));
  const changeTopic = (id: string | null) => {
    if (id === topicId) return;
    selections.current.set(topicId, navigation.selections[props.section] ?? null);
    props.navigation.select(props.section, selections.current.get(id) ?? null);
    setTopicId(id);
  };
  return <SplitPane {...props} positionKey={feedPositionKey(topicId)} listRevision={listRevision} list={<>
    <div className="topic-stream-section-header topic-stream-feed-header"><div className="topic-stream-feed-heading"><h2>Feed</h2><TopicSwitcher client={client} value={topicId} onChange={changeTopic} /></div>
      <FeedRefresh available={updates.available.has(topicId)} fetching={query.isFetching} pending={refreshing} error={failure} onRefresh={refresh} />
      <FeedRefreshFailure error={failure} saved={Boolean(query.data)} />
    </div>
    <FeedFilter preferences={feedPreferences} value={filter} />
    <ItemActionListNotice result={actionResult} selectedId={selected?.item.id} />
    {updates.error && <ApiErrorMessage title="Could not update saved Feed. Refresh to try again." error={updates.error} />}
    <FeedFeedback pending={query.isPending} error={query.error} olderError={query.isFetchNextPageError} saved={Boolean(query.data)} onRetry={() => { void query.refetch(); }} onSettings={() => props.navigation.open("settings")} />
    <EmptyFeedResults unread={filter === "unread"} ready={query.isSuccess} count={visible.length} hasMore={hasNextPage} />
    <ul className="topic-stream-reading-list" aria-label="Personalized news feed">
      {visible.map((entry) => <FeedRow key={entry.item.id} client={client} entry={entry} topicId={topicId} onResult={(message) => setActionResult(message ? { itemId: entry.item.id, message } : null)} capabilities={capabilities} selected={navigation.selections[props.section] === entry.item.id}
        onOpen={() => { setDetailsId(null); props.navigation.select(props.section, entry.item.id); }}
        onDetails={() => { setDetailsId(entry.item.id); props.navigation.select(props.section, entry.item.id); }} onReadingState={onReadingState} />)}
    </ul>
    <div className="topic-stream-sentinel" ref={sentinel} aria-hidden="true" />
    <OlderItemsButton visible={hasNextPage && visible.length === 0 && !query.isFetchNextPageError} pending={isFetchingNextPage || refreshing} onLoad={() => { void fetchNextPage(); }} />
    {query.isFetchingNextPage && <p className="topic-stream-loading-more">Loading older items…</p>}
    <FeedPaginationError active={query.isFetchNextPageError} error={query.error} onRetry={() => { void fetchNextPage(); }} />
  </>} detail={selected ? <ConnectedItemCard key={selected.item.id} client={client} entry={selected} details={detailsId === selected.item.id} topicId={topicId} onDetails={() => setDetailsId(selected.item.id)} onReadingState={onReadingState} onResult={(message) => setActionResult(message ? { itemId: selected.item.id, message } : null)} actionResult={actionResult} capabilities={capabilities} /> : <p className="topic-stream-metadata">This item is not in the loaded feed. Return to the list to choose another.</p>} />;
}

function FeedRefresh({ available, fetching, pending, error, onRefresh }: { available: boolean; fetching: boolean; pending: boolean; error: Error | undefined; onRefresh: () => void }) {
  const title = error ? `Refresh failed. ${error.message}` : available ? "Updates available. Refresh feed" : "Refresh feed";
  return <IconButton icon="refresh" label="Refresh feed" title={title} className={available ? "topic-stream-refresh-available" : ""} disabled={fetching || pending} aria-busy={pending} onClick={onRefresh} />;
}
function FeedRefreshFailure({ error, saved }: { error: Error | undefined; saved: boolean }) {
  if (!error || !saved) return null;
  return <p role="alert" className="topic-stream-refresh-failure" title={error.message} aria-label={`Refresh failed. ${error.message}. Showing saved Feed.`}>Refresh failed. Showing saved Feed.</p>;
}
function visibleEntries(entries: import("../../api/contracts").FeedEntry[], filter: "all" | "unread", retained: ReadonlySet<string> | undefined) {
  return entries.filter(({ item }) => item.visibility === "visible" && (filter === "all" || item.readingState === "unread" || retained?.has(item.id)));
}

function FeedFilter({ preferences, value }: { preferences: FeedPreferences; value: "all" | "unread" }) {
  const save = useMutation({ mutationFn: preferences.save, retry: false });
  return <><div className="topic-stream-feed-filter" role="group" aria-label="Show items">
    <button type="button" aria-pressed={value === "all"} disabled={save.isPending} onClick={() => save.mutate("all")}>All</button>
    <button type="button" aria-pressed={value === "unread"} disabled={save.isPending} onClick={() => save.mutate("unread")}>Unread</button>
  </div>{save.error && <ApiErrorMessage title="Could not save Feed filter. Your previous choice is kept." error={save.error} />}</>;
}
function EmptyFeedResults({ unread, ready, count, hasMore }: { unread: boolean; ready: boolean; count: number; hasMore: boolean }) {
  if (!ready || count > 0) return null;
  if (hasMore) return <FeedMessage title={unread ? "No unread items in loaded pages" : "Nothing visible in loaded pages"} detail="Older pages may still contain items to read." />;
  return unread ? <FeedMessage title="No unread items" detail="You’re caught up with this Feed." /> : <FeedMessage title="Nothing here yet" detail="Refresh to check for new recommendations." />;
}
function OlderItemsButton({ visible, pending, onLoad }: { visible: boolean; pending: boolean; onLoad: () => void }) {
  return visible ? <button type="button" className="topic-stream-refresh" disabled={pending} onClick={onLoad}>Load older items</button> : null;
}

function FeedFeedback({ pending, error, olderError, saved, onRetry, onSettings }: {
  pending: boolean; error: Error | null; olderError: boolean; saved: boolean;
  onRetry: () => void; onSettings: () => void;
}) {
  return <>{pending && <FeedMessage title="Connecting to Topic Stream" detail="Loading your feed…" />}
    <FeedLoadError error={olderError ? null : error} saved={saved} onRetry={onRetry} onSettings={onSettings} />
  </>;
}

function FeedPaginationError({ active, error, onRetry }: { active: boolean; error: Error | null; onRetry: () => void }) {
  return active ? <FeedLoadError error={error} saved={true} onRetry={onRetry} /> : null;
}
function FeedLoadError({ error, saved, onRetry, onSettings }: { error: Error | null; saved: boolean; onRetry: () => void; onSettings?: () => void }) {
  if (!error) return null;
  return <div className={saved ? "topic-stream-pagination-error" : undefined}><ApiErrorMessage title={saved ? "Could not load older items" : "Feed unavailable"} error={error} />
    <button type="button" className="topic-stream-refresh" onClick={onRetry}>Try again</button>
    {!saved && onSettings && <button type="button" className="topic-stream-refresh" onClick={onSettings}>Open settings</button>}</div>;
}

function feedPositionKey(topicId: string | null): string { return topicId ? `feed:${topicId}` : "feed"; }
function feedRevision(data: InfiniteData<FeedPage> | undefined, head: { snapshot: string; revision: number } | undefined): number {
  return head && head.snapshot === data?.pages[0]?.snapshot ? head.revision : 0;
}

function useFeedPages(client: FeedService, topicId: string | null, onAdopt: (selected: string | null, hasNew: boolean, snapshot: string) => void) {
  const queries = useQueryClient();
  const visited = useRef(new Set<string | null>());
  const [failures, setFailures] = useState(new Map<string | null, Error>());
  const scope = topicId ? { topicId } : {};
  const initialPage: FeedPageRequest = { limit: FEED_PAGE_SIZE, cursor: null, snapshot: null, ...scope };
  const query = useInfiniteQuery({
    queryKey: ["feed", topicId], gcTime: Infinity, staleTime: Infinity, refetchOnWindowFocus: false, refetchOnReconnect: false, retry: false,
    initialPageParam: initialPage,
    queryFn: async ({ pageParam }) => {
      if (pageParam.cursor !== null || visited.current.has(topicId)) {
        const saved = queries.getQueryData<InfiniteData<FeedPage>>(["feed", topicId]);
        return client.getFeed({ ...pageParam, snapshot: pageParam.snapshot ?? saved?.pages[0]?.snapshot ?? null });
      }
      visited.current.add(topicId);
      try { return await client.refreshFeed({ limit: FEED_PAGE_SIZE, ...scope }); }
      catch (error) {
        setFailures((current) => new Map(current).set(topicId, error instanceof Error ? error : new Error("Could not refresh Feed.")));
        return client.getFeed(pageParam);
      }
    },
    getNextPageParam: (lastPage, pages): FeedPageRequest | undefined => lastPage.nextCursor === null ? undefined : { ...initialPage, cursor: lastPage.nextCursor, snapshot: pages[0]?.snapshot ?? null }
  });
  const refresh = useMutation({
    mutationKey: ["feed-refresh"], retry: false,
    mutationFn: (selected: string | null) => {
      const previous = queries.getQueryData<InfiniteData<FeedPage, FeedPageRequest>>(["feed", selected]);
      return refreshFeedTraversal(client, selected, previous?.pages ?? []);
    },
    onSuccess: async ({ pages, hasNew }, selected) => {
      const key = ["feed", selected];
      await queries.cancelQueries({ queryKey: key, exact: true });
      queries.setQueryData(key, { pages, pageParams: feedPageParams(pages, selected) });
      reconcilePageFeedback(queries, pages);
      onAdopt(selected, hasNew, pages[0]!.snapshot);
      setFailures((current) => { const next = new Map(current); next.delete(selected); return next; });
    },
    onError: (error, selected) => { setFailures((current) => new Map(current).set(selected, error)); }
  });
  const refreshing = useIsMutating({ mutationKey: ["feed-refresh"], predicate: (mutation) => mutation.state.variables === topicId }) > 0;
  const requestRefresh = () => {
    if (queries.isMutating({ mutationKey: ["feed-refresh"], predicate: (mutation) => mutation.state.variables === topicId }) === 0) refresh.mutate(topicId);
  };
  return { query, failure: failures.get(topicId), refresh: requestRefresh, refreshing };
}
