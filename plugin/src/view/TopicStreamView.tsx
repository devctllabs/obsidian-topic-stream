import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";

import { TopicStreamClient, type FeedPageRequest } from "../api/client";
import { FEED_PAGE_SIZE } from "../api/url";
import type { FeedEntry, ItemKind, TopicMode } from "../api/types";
import { ApiErrorMessage } from "./ApiErrorMessage";

interface TopicStreamViewProps {
  client: TopicStreamClient;
}

export function TopicStreamView({ client }: TopicStreamViewProps) {
  const [generation, setGeneration] = useState(0);
  const sentinel = useRef<HTMLDivElement>(null);
  const query = useInfiniteQuery({
    queryKey: ["feed", generation],
    initialPageParam: { limit: FEED_PAGE_SIZE, cursor: null, snapshot: null } as FeedPageRequest,
    queryFn: ({ pageParam }) => client.getFeed(pageParam),
    getNextPageParam: (lastPage, pages): FeedPageRequest | undefined => {
      if (lastPage.nextCursor === null) {
        return undefined;
      }
      return { limit: FEED_PAGE_SIZE, cursor: lastPage.nextCursor, snapshot: pages[0]?.snapshot ?? null };
    }
  });

  useEffect(() => {
    const element = sentinel.current;
    if (element === null || !query.hasNextPage) {
      return;
    }
    const observer = new IntersectionObserver(([entry]) => {
      if (entry?.isIntersecting === true && !query.isFetchingNextPage) {
        void query.fetchNextPage();
      }
    }, { rootMargin: "240px" });
    observer.observe(element);
    return () => observer.disconnect();
  }, [query.fetchNextPage, query.hasNextPage, query.isFetchingNextPage]);

  const entries = useMemo(
    () => query.data?.pages.flatMap((page) => page.items) ?? [],
    [query.data]
  );

  return (
    <main className="topic-stream-root">
      <header className="topic-stream-header">
        <div>
          <p className="topic-stream-kicker">Personal briefing</p>
          <h1>Topic Stream</h1>
        </div>
        <button
          className="topic-stream-refresh"
          type="button"
          disabled={query.isFetching}
          onClick={() => setGeneration((current) => current + 1)}
          aria-label="Refresh feed"
        >
          {query.isFetching && !query.isFetchingNextPage ? "Refreshing…" : "Refresh"}
        </button>
      </header>

      <SourcePanel client={client} />
	  <TopicPanel client={client} />

      {query.isPending && <FeedMessage title="Building your feed" detail="Retrieving the most relevant new items." />}
      {query.isError && <ApiErrorMessage title="Feed unavailable" error={query.error} />}
      {!query.isPending && !query.isError && entries.length === 0 && (
        <FeedMessage title="Nothing here yet" detail="Add a source or loosen a topic filter, then refresh." />
      )}

      <section className="topic-stream-list" aria-label="Personalized news feed">
        {entries.map((entry) => <FeedCard client={client} entry={entry} key={entry.item.id} />)}
      </section>

      <div className="topic-stream-sentinel" ref={sentinel} aria-hidden="true" />
      {query.isFetchingNextPage && <p className="topic-stream-loading-more">Loading older items…</p>}
    </main>
  );
}

const itemKinds: Array<{ value: ItemKind; label: string }> = [
  { value: "article", label: "Articles" },
  { value: "paper", label: "Papers" },
  { value: "video", label: "Videos" },
  { value: "repository", label: "Repositories" }
];

function TopicPanel({ client }: { client: TopicStreamClient }) {
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [include, setInclude] = useState("");
  const [exclude, setExclude] = useState("");
  const [mode, setMode] = useState<TopicMode>("ordinary");
  const [sourceIds, setSourceIds] = useState<string[]>([]);
  const [selectedKinds, setSelectedKinds] = useState<ItemKind[]>([]);
  const sources = useQuery({ queryKey: ["sources"], queryFn: () => client.listSources() });
  const topics = useQuery({ queryKey: ["topics"], queryFn: () => client.listTopics() });
  const createTopic = useMutation({
    mutationFn: () => client.createTopic({
      name, sourceIds, itemKinds: selectedKinds, include, exclude, mode, enabled: true
    }),
    onSuccess: async () => {
      setName("");
      setInclude("");
      setExclude("");
      setMode("ordinary");
      setSourceIds([]);
      setSelectedKinds([]);
      await queryClient.invalidateQueries({ queryKey: ["topics"] });
    }
  });

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (name.trim() !== "") {
      createTopic.mutate();
    }
  };

  const toggleKind = (kind: ItemKind) => {
    setSelectedKinds((current) => current.includes(kind)
      ? current.filter((candidate) => candidate !== kind)
      : [...current, kind]);
  };

  return (
    <details className="topic-stream-topics">
      <summary>Topics <span>{topics.data?.length ?? 0}</span></summary>
      <div className="topic-stream-topics-body">
        <form className="topic-stream-topic-form" onSubmit={submit}>
          <label>Name<input value={name} maxLength={80} onChange={(event) => setName(event.target.value)} placeholder="Practical AI" /></label>
          <label>Include<textarea value={include} maxLength={4000} rows={2} onChange={(event) => setInclude(event.target.value)} placeholder="Technical advances in inference" /></label>
          <label>Exclude<textarea value={exclude} maxLength={4000} rows={2} onChange={(event) => setExclude(event.target.value)} placeholder="Beginner tutorials and hype" /></label>
          <label>Mode<select value={mode} onChange={(event) => setMode(event.target.value as TopicMode)}><option value="ordinary">Ordinary</option><option value="strict">Strict</option></select></label>
          <label>Sources<select multiple value={sourceIds} onChange={(event) => setSourceIds(Array.from(event.currentTarget.selectedOptions, (option) => option.value))}>{sources.data?.map((source) => <option key={source.id} value={source.id}>{source.name}</option>)}</select><small>No selection means all sources</small></label>
          <fieldset><legend>Item kinds <small>none means all</small></legend>{itemKinds.map(({ value, label }) => <label key={value}><input type="checkbox" checked={selectedKinds.includes(value)} onChange={() => toggleKind(value)} />{label}</label>)}</fieldset>
          <button type="submit" disabled={createTopic.isPending}>{createTopic.isPending ? "Adding…" : "Add topic"}</button>
        </form>
        {createTopic.isError && <ApiErrorMessage title="Could not add topic" error={createTopic.error} />}
        {topics.isError && <ApiErrorMessage title="Could not load topics" error={topics.error} />}
        <ul className="topic-stream-topic-list">
          {topics.data?.map((topic) => <li key={topic.id}><span><strong>{topic.name}</strong><small>{topic.include || "All matching recent items"}</small></span><span>{topic.mode} · r{topic.revision}</span></li>)}
        </ul>
      </div>
    </details>
  );
}

function SourcePanel({ client }: { client: TopicStreamClient }) {
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const sources = useQuery({ queryKey: ["sources"], queryFn: () => client.listSources() });
  const createSource = useMutation({
    mutationFn: () => client.createSource({ name, url, kind: "rss", enabled: true, pollIntervalMinutes: 60 }),
    onSuccess: async () => {
      setName("");
      setUrl("");
      await queryClient.invalidateQueries({ queryKey: ["sources"] });
    }
  });
  const syncAll = useMutation({ mutationFn: () => client.syncAllSources() });

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (name.trim() !== "" && url.trim() !== "") {
      createSource.mutate();
    }
  };

  return (
    <details className="topic-stream-sources">
      <summary>Sources <span>{sources.data?.length ?? 0}</span></summary>
      <div className="topic-stream-sources-body">
        <form className="topic-stream-source-form" onSubmit={submit}>
          <label>Name<input value={name} onChange={(event) => setName(event.target.value)} placeholder="AI research" /></label>
          <label>RSS or Atom URL<input value={url} onChange={(event) => setUrl(event.target.value)} placeholder="https://example.com/feed.xml" type="url" /></label>
          <button type="submit" disabled={createSource.isPending}>Add source</button>
        </form>
        {createSource.isError && <ApiErrorMessage title="Could not add source" error={createSource.error} />}
        {sources.isError && <ApiErrorMessage title="Could not load sources" error={sources.error} />}
        <ul className="topic-stream-source-list">
          {sources.data?.map((source) => (
            <li key={source.id}>
              <span><strong>{source.name}</strong><small>{source.url}</small></span>
              <span>{source.enabled ? `${source.pollIntervalMinutes} min` : "Paused"}</span>
            </li>
          ))}
        </ul>
        <button className="topic-stream-sync-all" type="button" disabled={syncAll.isPending} onClick={() => syncAll.mutate()}>
          {syncAll.isPending ? "Queueing…" : "Sync all now"}
        </button>
        {syncAll.isSuccess && <p className="topic-stream-job-note">Sync queued · {syncAll.data.jobId.slice(0, 8)}</p>}
        {syncAll.isError && <ApiErrorMessage title="Could not start sync" error={syncAll.error} />}
      </div>
    </details>
  );
}

function FeedCard({ client, entry }: { client: TopicStreamClient; entry: FeedEntry }) {
  const [comment, setComment] = useState(entry.item.feedback?.comment ?? "");
  const feedback = useMutation({
    mutationFn: (sentiment: "like" | "dislike") => client.setFeedback(
      entry.item.id,
      sentiment,
      comment.trim() === "" ? null : comment.trim()
    )
  });
  const currentFeedback = feedback.data ?? entry.item.feedback;
  const date = entry.item.publishedAt === null
    ? "Recently discovered"
    : new Intl.DateTimeFormat(undefined, { dateStyle: "medium" }).format(new Date(entry.item.publishedAt));

  return (
    <article className="topic-stream-card">
      <div className="topic-stream-card-meta">
        <span>{date}</span>
        <span>{entry.explanation.summary}</span>
      </div>
      <h2><a href={entry.item.url}>{entry.item.title}</a></h2>
      <p>{entry.item.excerpt}</p>
      <label className="topic-stream-comment">
        <span>Optional note for future recommendations</span>
        <textarea
          value={comment}
          maxLength={2000}
          rows={2}
          onChange={(event) => setComment(event.target.value)}
          placeholder="Why was this useful or not for you?"
        />
      </label>
      <div className="topic-stream-card-actions" aria-label={`Actions for ${entry.item.title}`}>
        <button
          type="button"
          aria-label="Like"
          aria-pressed={currentFeedback?.sentiment === "like"}
          disabled={feedback.isPending}
          onClick={() => feedback.mutate("like")}
        >Useful</button>
        <button
          type="button"
          aria-label="Dislike"
          aria-pressed={currentFeedback?.sentiment === "dislike"}
          disabled={feedback.isPending}
          onClick={() => feedback.mutate("dislike")}
        >Not for me</button>
      </div>
      {feedback.isError && <ApiErrorMessage title="Could not save feedback" error={feedback.error} />}
    </article>
  );
}

function FeedMessage({ title, detail }: { title: string; detail: string }) {
  return (
    <section className="topic-stream-message" role="status">
      <h2>{title}</h2>
      <p>{detail}</p>
    </section>
  );
}
