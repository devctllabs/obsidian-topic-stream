import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useEffect, useState, useSyncExternalStore } from "react";
import type { Schema } from "../../api/contracts";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import { ActionMenu } from "../../shared/components/ActionMenu";
import { IconButton } from "../../shared/components/IconButton";
import { SplitPane } from "../shell/SplitPane";
import type { SectionProps } from "../shell/TopicStreamShell";
import type { DigestsService } from "./services";
import type { FeedService } from "../feed/services";
import type { ItemActionContext } from "../feed/item-actions";
import { DigestContents, DigestEdition, editionItemKey, type EditionSelection } from "./DigestEdition";
import { DigestReader, type CapturedCapabilities } from "./DigestReader";
import type { DigestBuilds } from "../settings/digest-builds";
import type { ShellNavigation } from "../shell/navigation";
import { ArchiveBuilds, BuildMenu } from "./ArchiveBuilds";
import type { DigestReadiness } from "./readiness";
import type { SaveIssue } from "../snapshots/use-snapshot-workflow";

export function DigestsWorkspace({ client, capabilitiesFor, onReadingState, builds, readiness, saveIssue, ...props }: SectionProps & { client: DigestsService & FeedService; capabilitiesFor: CapturedCapabilities; onReadingState: ItemActionContext["onReadingState"]; builds: DigestBuilds; readiness: DigestReadiness; saveIssue?: SaveIssue | undefined }) {
  const navigation = useSyncExternalStore(props.navigation.subscribe, props.navigation.getSnapshot);
  const issueId = navigation.selections.digests;
  const [selection, setSelection] = useState<{ issueId: string | undefined; current: EditionSelection | null; last: EditionSelection | null }>({ issueId, current: null, last: null });
  const item = selection.issueId === issueId ? selection.current : null;
  const lastItem = selection.issueId === issueId ? selection.last : null;
  const [digestId, setDigestId] = useState("");
  const issue = useQuery({ queryKey: ["digest-issue", issueId], queryFn: () => client.getDigestIssue(issueId!), enabled: Boolean(issueId), staleTime: Infinity, retry: false, refetchOnWindowFocus: false, refetchOnReconnect: false });
  const loadedIssueId = issue.data?.id;
  useEffect(() => { if (props.active && loadedIssueId) void readiness.open(loadedIssueId); }, [props.active, loadedIssueId, readiness]);
  const openItem = (value: EditionSelection) => setSelection({ issueId, current: value, last: value });
  const back = () => { if (item) setSelection((value) => ({ ...value, current: null })); else props.navigation.select("digests", null); };
  const archive = <DigestArchive client={client} builds={builds} readiness={readiness} navigation={props.navigation} digestId={digestId} onFilter={(value) => { props.navigation.select("digests", null); setDigestId(value); }} onOpen={(id) => { setSelection({ issueId: id, current: null, last: null }); props.navigation.select("digests", id); }} />;
  const detail = <IssueContent saveIssue={saveIssue} issue={issue.data} item={item} lastItem={lastItem} client={client} onOpen={openItem} onReadingState={onReadingState} capabilitiesFor={capabilitiesFor} fallback={<IssueFeedback error={issue.error} onRetry={() => { void issue.refetch(); }} />} />;
  return <SplitPane {...props} selection={item ? editionItemKey(item) : issueId ?? null} onBack={back} backLabel={item ? "Back to issue" : "Back to archive"} emptyMessage="Choose an issue to read" positionKey={item ? `digest-contents:${issueId}` : `digest-archive:${digestId}`} list={item && issue.data ? <DigestContents issue={issue.data} selected={item} onOpen={openItem} client={client} onReadingState={onReadingState} /> : archive} detail={detail} />;
}

function IssueContent({ issue, item, lastItem, client, onOpen, onReadingState, capabilitiesFor, fallback, saveIssue }: { issue: Schema<"DigestIssue"> | undefined; item: EditionSelection | null; lastItem: EditionSelection | null; client: FeedService; onOpen: (selection: EditionSelection) => void; onReadingState: ItemActionContext["onReadingState"]; capabilitiesFor: CapturedCapabilities; fallback: import("react").ReactNode; saveIssue: SaveIssue | undefined }) {
  if (!issue) return fallback;
  const section = item ? issue.sections[item.section] : undefined;
  const saved = section?.items.find((saved) => saved.item.id === item?.itemId);
  if (saved && item) return <DigestReader key={editionItemKey(item)} client={client} issueId={issue.id} saved={saved} topicId={section!.topicId} onDetails={() => undefined} onResult={() => undefined} onReadingState={onReadingState} capabilitiesFor={capabilitiesFor} />;
  return <DigestEdition saveIssue={saveIssue} issue={issue} client={client} onOpen={onOpen} onReadingState={onReadingState} returnItem={lastItem} capabilitiesFor={capabilitiesFor} />;
}

function DigestArchive({ client, digestId, onFilter, onOpen, builds, navigation, readiness }: { client: DigestsService; digestId: string; onFilter: (id: string) => void; onOpen: (id: string) => void; builds: DigestBuilds; navigation: ShellNavigation; readiness: DigestReadiness }) {
  const observed = useSyncExternalStore(readiness.subscribe, readiness.getSnapshot);
  const definitions = useQuery({ queryKey: ["digests"], queryFn: () => client.listDigests() });
  const savedDefinitions = definitions.data ?? [];
  const issues = useInfiniteQuery({ queryKey: ["digest-archive", digestId], retry: false, staleTime: Infinity, refetchOnWindowFocus: false, refetchOnReconnect: false,
    initialPageParam: null as string | null,
    queryFn: ({ pageParam }) => client.listDigestIssues({ limit: 20, ...(digestId ? { digestId } : {}), ...(pageParam ? { cursor: pageParam } : {}) }),
    getNextPageParam: (page) => page.nextCursor });
  const entries = issues.data?.pages.flatMap((page) => page.items) ?? [];
  return <>
    <div className="topic-stream-section-header"><h2>Digests</h2><div className="topic-stream-row-tools"><IconButton icon="refresh" label="Refresh Digest archive" disabled={issues.isFetching} onClick={() => { void issues.refetch(); void readiness.refresh(); void definitions.refetch(); }} /><BuildMenu definitions={savedDefinitions} builds={builds} /><ActionMenu label="Digest archive actions" menuLabel="Digest archive actions" items={[{ id: "definitions", label: "Manage definitions", onSelect: () => navigation.openSettings("digests") }]} /></div></div>
    {definitions.error && <ApiErrorMessage title="Could not load Digest definitions. Refresh to try again." error={definitions.error} />}
    <ArchiveBuilds definitions={savedDefinitions} builds={builds} navigation={navigation} client={client} onOpen={onOpen} />
    <label className="topic-stream-archive-filter">Digest definition<select value={digestId} onChange={(event) => onFilter(event.target.value)}><option value="">All definitions</option>{savedDefinitions.map((definition) => <option key={definition.id} value={definition.id}>{definition.name}</option>)}</select></label>
    <ArchiveFeedback pending={issues.isPending} error={issues.error} olderError={issues.isFetchNextPageError} empty={issues.isSuccess && entries.length === 0} badgeError={observed.error} onRetry={() => { void issues.refetch(); void readiness.refresh(); }} />
    <ul className="topic-stream-reading-list" aria-label="Digest archive">{entries.map((issue) => <IssueRow key={issue.id} issue={issue} unseen={observed.unseen.includes(issue.id)} definition={savedDefinitions.find((definition) => definition.id === issue.digestId)?.name} onOpen={() => onOpen(issue.id)} />)}</ul>
    {issues.hasNextPage && <button type="button" className="topic-stream-refresh" disabled={issues.isFetchingNextPage} onClick={() => { void issues.fetchNextPage(); }}>Load older issues</button>}
  </>;
}

function ArchiveFeedback({ pending, error, olderError, empty, badgeError, onRetry }: { pending: boolean; error: Error | null; olderError: boolean; empty: boolean; badgeError: Error | null; onRetry: () => void }) {
  return <>{pending && <p role="status" className="topic-stream-metadata">Loading Digest archive…</p>}
    {error && <><ApiErrorMessage title={olderError ? "Could not load older issues" : "Could not load Digest archive"} error={error} />{!olderError && <button type="button" className="topic-stream-refresh" onClick={onRetry}>Try again</button>}</>}
    {!error && badgeError && <ApiErrorMessage title="Could not update new-issue badges. Refresh to try again." error={badgeError} />}
    {empty && <p className="topic-stream-metadata">No saved Digest Issues yet.</p>}
  </>;
}

function IssueFeedback({ error, onRetry }: { error: Error | null; onRetry: () => void }) {
  return error ? <><ApiErrorMessage title="Could not load this Digest Issue" error={error} /><button type="button" className="topic-stream-refresh" onClick={onRetry}>Try again</button></> : <p role="status" className="topic-stream-metadata">Loading saved issue…</p>;
}

function IssueRow({ issue, definition, onOpen, unseen }: { issue: Schema<"DigestIssueSummary">; definition: string | undefined; onOpen: () => void; unseen: boolean }) {
  return <li><button type="button" className="topic-stream-row" data-item-id={issue.id} onClick={onOpen}>
    <span className="topic-stream-row-title">{issue.title}</span><span className="topic-stream-row-meta">{issue.title !== definition && <span>{definition ?? "Saved definition"}</span>}<time dateTime={issue.generatedAt}>{new Date(issue.generatedAt).toLocaleDateString(undefined, { dateStyle: "medium" })}</time>{issue.status === "degraded" && <span>Partial issue</span>}{unseen && <span className="topic-stream-new-issue">New</span>}</span>
  </button></li>;
}
