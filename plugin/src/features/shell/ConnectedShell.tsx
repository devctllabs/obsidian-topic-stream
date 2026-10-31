import type { WorkspaceStatePort } from "../settings/device-state";
import { TransitionNotifications } from "../settings/transition-notifications";
import { useTransitionObservation } from "../settings/use-transition-observation";
import { JobActions, type JobActionService } from "../../shared/jobs/job-actions";
import { connectJobResources } from "../../shared/jobs/resources";
import { useCallback, useContext, useMemo, useEffect, useSyncExternalStore } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { Item } from "../../api/contracts";
import { patchItemState } from "../feed/cache";
import { retainFeedUnread } from "../feed/unread-membership";
import { FeedView } from "../feed/FeedView";
import { SearchView } from "../search/SearchView";
import type { SearchService } from "../search/services";
import { MarkdownRendererContext, type MarkdownRendererPort } from "../reader/markdown";
import { ReaderBody } from "../reader/ReaderBody";
import { ReaderImages, type ReaderImageService } from "../reader/images";
import { ItemSummaries, type ItemSummaryService } from "../reader/item-summaries";
import { summaryCapability } from "../reader/SummaryBody";
import { DigestsWorkspace } from "../digests/DigestsWorkspace";
import { DigestReadiness } from "../digests/readiness";
import { TasksWorkspace } from "../tasks/TasksWorkspace";
import type { TasksService } from "../tasks/services";
import type { DigestsService } from "../digests/services";
import type { CapturedCapabilities } from "../digests/DigestReader";
import type { FeedPreferences } from "../feed/preferences";
import type { ViewEvents } from "../../shared/events";
import type { FeedItemCapabilities } from "../feed/capabilities";
import type { PreferenceProfileService, TopicsWorkspaceService, DigestDefinitionService, WorkspaceSettingsService } from "../settings/services";
import { SettingsWorkspace } from "../settings/SettingsWorkspace";
import type { SourcesWorkspaceService } from "../settings/SourcesSection";
import { TopicStreamShell, type SectionProps } from "./TopicStreamShell";
import type { ShellNavigation } from "./navigation";

import { DigestBuilds, type DigestBuildService } from "../settings/digest-builds";
import { connectDigestBuilds } from "../settings/digest-build-resources";
import { useSnapshotWorkflow, type SnapshotHost } from "../snapshots/use-snapshot-workflow";

export function ConnectedShell({ client, navigation, localState, notify, feedPreferences, events, capabilities, markdown, snapshots }: { client: SearchService & DigestsService & TasksService & ReaderImageService & ItemSummaryService & SourcesWorkspaceService & TopicsWorkspaceService & PreferenceProfileService & DigestDefinitionService & WorkspaceSettingsService & DigestBuildService & JobActionService & Pick<import("../../api/client").TopicStreamClient, "getSystemStatus" | "getItem">; navigation: ShellNavigation; localState: WorkspaceStatePort; notify: (message: string) => void; feedPreferences: FeedPreferences; events?: ViewEvents | undefined; capabilities?: FeedItemCapabilities | undefined; markdown?: MarkdownRendererPort | undefined; snapshots?: SnapshotHost | undefined }) {
  const inheritedMarkdown = useContext(MarkdownRendererContext);
  const renderer = markdown ?? inheritedMarkdown;
  const images = useMemo(() => new ReaderImages(client), [client]);
  useEffect(() => () => images.dispose(), [images]);
  const queries = useQueryClient();
  useEffect(() => connectJobResources(queries, events), [queries, events]);
  const readiness = useMemo(() => new DigestReadiness(client, localState, queries, notify), [client, localState, queries, notify]);
  useEffect(() => readiness.connect(events), [readiness, events]);
  const digestState = useSyncExternalStore(readiness.subscribe, readiness.getSnapshot);
  const jobs = useMemo(() => new JobActions(client), [client]);
  useEffect(() => () => jobs.dispose(), [jobs]);
  const summaries = useMemo(() => new ItemSummaries(queries, client, jobs), [queries, client, jobs]);
  useEffect(() => summaries.connect(events), [summaries, events]);
  const snapshot = useSnapshotWorkflow(snapshots, client, summaries);
  const { enabled: canSave, saveItem, saveCaptured, saveIssue } = snapshot;
  const itemCapabilities = useMemo<FeedItemCapabilities>(() => ({ summary: summaryCapability(summaries), ...(canSave ? { saveToVault: saveItem } : {}), ...(renderer ? { renderReader: (item) => <ReaderBody key={`${item.id}:${item.summarySourceVersion}`} item={item} renderer={renderer} images={images} reloadContent={() => client.getItem(item.id)} /> } : {}), ...capabilities }), [renderer, capabilities, images, client, summaries, canSave, saveItem]);
  const capturedCapabilities = useCallback<CapturedCapabilities>((issueId, saved) => ({ ...capabilities, ...(canSave ? { saveToVault: () => saveCaptured(issueId, saved) } : {}), summary: summaryCapability(summaries, { issueId, captured: saved }), ...(renderer ? { renderReader: (item) => <ReaderBody key={`${issueId}:${item.id}:${item.summarySourceVersion}`} item={item} renderer={renderer} images={images} /> } : {}) }), [capabilities, summaries, renderer, images, canSave, saveCaptured]);
  const readingChanged = useCallback((id: string, state: Item["readingState"]) => {
    if (state === "read" && feedPreferences.getSnapshot() === "unread") retainFeedUnread(queries);
    patchItemState(queries, id, { readingState: state });
  }, [queries, feedPreferences]);
  const transitions = useMemo(() => new TransitionNotifications(client, localState, notify), [client, localState, notify]);
  useEffect(() => () => transitions.dispose(), [transitions]);
  const transitionError = useTransitionObservation(client, localState, transitions);
  const builds = useMemo(() => new DigestBuilds({ generateDigest: (id, requestId) => client.generateDigest(id, requestId), getJob: (id) => queries.query({ queryKey: ["job", id], queryFn: () => client.getJob(id), staleTime: 0, retry: false }) }, localState), [client, localState, queries]);
  useEffect(() => () => builds.dispose(), [builds]);
  useEffect(() => connectDigestBuilds(builds, queries, events), [builds, queries, events]);
  const screens = useMemo(() => ({
    feed: (props: SectionProps) => <FeedView client={client} feedPreferences={feedPreferences} events={events} capabilities={itemCapabilities} onReadingState={readingChanged} {...props} />,
    settings: () => <SettingsWorkspace client={client} navigation={navigation} builds={builds} jobs={jobs} transitions={transitions} />,
    digests: (props: SectionProps) => <DigestsWorkspace client={client} builds={builds} readiness={readiness} capabilitiesFor={capturedCapabilities} onReadingState={readingChanged} saveIssue={canSave ? saveIssue : undefined} {...props} />,
    search: (props: SectionProps) => <SearchView client={client} capabilities={itemCapabilities} onReadingState={readingChanged} {...props} />,
    tasks: (props: SectionProps) => <TasksWorkspace client={client} actions={jobs} transitionError={transitionError} {...props} />
  }), [client, navigation, builds, jobs, transitions, feedPreferences, events, itemCapabilities, readingChanged, capturedCapabilities, readiness, transitionError, canSave, saveIssue]);
  return <><TopicStreamShell navigation={navigation} screens={screens} unseenIssues={digestState.unseen.length} />{snapshot.dialog}</>;
}

