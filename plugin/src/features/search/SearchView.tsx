import { useInfiniteQuery } from "@tanstack/react-query";
import { useState, useSyncExternalStore } from "react";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import { ApiError } from "../../api/errors";
import { ConnectedItemCard } from "../feed/FeedCard";
import { ItemActionListNotice, type ItemActionContext, type ItemActionResult } from "../feed/item-actions";
import type { FeedItemCapabilities } from "../feed/capabilities";
import { FeedRow } from "../feed/FeedRow";
import { FeedMessage } from "../feed/FeedMessage";
import { SplitPane } from "../shell/SplitPane";
import type { SectionProps } from "../shell/TopicStreamShell";
import type { SearchService } from "./services";
import { SearchFilters, type SearchFilterValues } from "./SearchFilters";

export function SearchView({ client, onReadingState, capabilities, ...props }: SectionProps & { client: SearchService; onReadingState: ItemActionContext["onReadingState"]; capabilities?: FeedItemCapabilities | undefined }) {
  const navigation = useSyncExternalStore(props.navigation.subscribe, props.navigation.getSnapshot);
  const [draft, setDraft] = useState("");
  const [query, setQuery] = useState("");
  const [session, setSession] = useState(0);
  const [filters, setFilters] = useState<SearchFilterValues>({});
  const [details, setDetails] = useState(false);
  const [result, setResult] = useState<ItemActionResult | null>(null);
  const request = { ...filters, ...(query ? { q: query, mode: "hybrid" as const } : {}) };
  const pages = useInfiniteQuery({ queryKey: ["search", session, request], gcTime: 0, staleTime: Infinity,
    retry: false, refetchOnWindowFocus: false, refetchOnReconnect: false, initialPageParam: null as string | null,
    queryFn: ({ pageParam }) => client.searchItems({ ...request, limit: 20, ...(pageParam ? { cursor: pageParam } : {}) }),
    getNextPageParam: (page, loaded) => loaded.length < 5 ? page.nextCursor : null });
  const entries = pages.data?.pages.flatMap((page) => page.items) ?? [];
  const selected = entries.find(({ item }) => item.id === navigation.selections[props.section]);
  return <SplitPane {...props} positionKey={`search:${session}`} list={<>
    <div className="topic-stream-section-header"><h2>Search</h2><SearchFilters client={client} value={filters} onApply={(value) => { props.navigation.select(props.section, null); setFilters(value); setSession((current) => current + 1); }} /></div>
    <form className="topic-stream-search-form" onSubmit={(event) => { event.preventDefault(); props.navigation.select(props.section, null); setQuery(draft.trim()); setSession((value) => value + 1); }}>
      <input type="search" aria-label="Search the archive" placeholder="Search the archive" value={draft} onChange={(event) => setDraft(event.target.value)} />
      <button type="submit" className="topic-stream-refresh">Search</button>
    </form>
    <SearchFeedback pending={pages.isPending} error={pages.isFetchNextPageError ? null : pages.error} empty={pages.isSuccess && entries.length === 0} filtered={Object.keys(request).length > 0} navigation={props.navigation} onRetry={() => { void pages.refetch(); }} />
    <ItemActionListNotice result={result} selectedId={selected?.item.id} />
    <ul className="topic-stream-reading-list" aria-label="Search results">
      {entries.map((entry) => <FeedRow key={entry.item.id} client={client} entry={entry} topicId={filters.topicId ?? null} selected={selected?.item.id === entry.item.id} capabilities={capabilities}
        onOpen={() => { setDetails(false); props.navigation.select(props.section, entry.item.id); }}
        onDetails={() => { setDetails(true); props.navigation.select(props.section, entry.item.id); }} onReadingState={onReadingState} onResult={(message) => setResult(message ? { itemId: entry.item.id, message } : null)} />)}
    </ul>
    <SearchMoreResults hasNext={pages.hasNextPage} count={entries.length} busy={pages.isFetchingNextPage} error={pages.isFetchNextPageError ? pages.error : null} onLoad={() => { void pages.fetchNextPage(); }} />
  </>} detail={selected ? <ConnectedItemCard key={selected.item.id} client={client} entry={selected} details={details} topicId={filters.topicId ?? null} onDetails={() => setDetails(true)} onReadingState={onReadingState} onResult={(message) => setResult(message ? { itemId: selected.item.id, message } : null)} actionResult={result} capabilities={capabilities} /> : <p className="topic-stream-metadata">This Item is not in the loaded results. Return to the list to choose another.</p>} />;
}

function SearchFeedback({ pending, error, empty, filtered, navigation, onRetry }: { pending: boolean; error: Error | null; empty: boolean; filtered: boolean; navigation: SectionProps["navigation"]; onRetry: () => void }) {
  if (pending) return <FeedMessage title="Loading the archive" detail="Finding saved Items…" />;
  if (error) return <SearchError error={error} navigation={navigation} onRetry={onRetry} />;
  if (!empty) return null;
  return filtered ? <FeedMessage title="No matching Items" detail="Try another query or adjust your filters." /> : <FeedMessage title="No saved Items yet" detail="Add a Source in Settings to start your archive." />;
}

function SearchError({ error, navigation, onRetry }: { error: Error; navigation: SectionProps["navigation"]; onRetry: () => void }) {
  const preparing = error instanceof ApiError && error.code === "retrieval_unavailable";
  return <><ApiErrorMessage title={preparing ? "Search is temporarily unavailable" : "Could not search the archive"} error={error} />
    <div className="topic-stream-form-actions"><button type="button" className="topic-stream-refresh" onClick={onRetry}>Try again</button>
      {preparing && <><button type="button" className="topic-stream-refresh" onClick={() => navigation.open("settings")}>Open Settings</button><button type="button" className="topic-stream-refresh" onClick={() => navigation.open("tasks")}>Open Tasks</button></>}
    </div></>;
}

function SearchMoreResults({ hasNext, count, busy, error, onLoad }: { hasNext: boolean; count: number; busy: boolean; error: Error | null; onLoad: () => void }) {
  if (error) return <div className="topic-stream-pagination-error"><ApiErrorMessage title="Could not load more results" error={error} /><button type="button" className="topic-stream-refresh" disabled={busy} onClick={onLoad}>Try again</button></div>;
  if (count >= 100) return <p role="status" className="topic-stream-metadata">Showing the first 100 results. Refine your search for more.</p>;
  return hasNext ? <button type="button" className="topic-stream-refresh" disabled={busy} onClick={onLoad}>Load more results</button> : null;
}
