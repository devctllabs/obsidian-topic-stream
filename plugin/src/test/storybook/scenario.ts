import type { Feedback, FeedPage, Item, ItemImageDownload, JobAccepted, QueryParameters, RequestBody, Schema, Source, SourceInput, Topic, TopicInput } from "../../api/contracts";
import type { FeedPageRequest } from "../../api/url";
import type { FeedService } from "../../features/feed/services";
import type { SourceService, TopicService } from "../../features/settings/services";
import { ControlledAction, FixtureEvents } from "./fake-services";
import type { Deferred } from "./deferred";
import { fixtureId, makeWorkspaceSettings, makeCollections, makeIssue, makeJob, makeModel, makeProfile, makeRetrievalStatus, makeSummary } from "./fixtures";

/** Test-only service capabilities; feature owners drive their real controls against these ports. */
export class FixtureScenario implements FeedService, SourceService, TopicService {
  public collections = makeCollections();
  public notices: string[] = [];
  public notify = (message: string): void => { this.notices.push(message); };
  public systemStatus: Schema<"SystemStatus"> = { status: "ready", version: "fixture", workspaceId: fixtureId(1), components: [], now: "2026-10-27T09:00:00Z" };
  public getSystemStatus = () => Promise.resolve(structuredClone(this.systemStatus));
  public workspaceSettings = makeWorkspaceSettings();
  public readonly workspaceUpdate = new ControlledAction<{ input: RequestBody<"updateWorkspaceSettings">; version: number }, Schema<"WorkspaceSettings">>();
  public getWorkspaceSettings = () => Promise.resolve(structuredClone(this.workspaceSettings));
  public updateWorkspaceSettings = async (input: RequestBody<"updateWorkspaceSettings">, version: number) => {
    const saved = await this.workspaceUpdate.invoke({ input, version });
    this.workspaceSettings = saved; return saved;
  };
  public digests: Schema<"DigestDefinition">[] = [];
  public readonly digestCreation = new ControlledAction<Schema<"DigestInput">, Schema<"DigestDefinition">>();
  public readonly digestUpdate = new ControlledAction<{ id: string; input: RequestBody<"updateDigest">; version: number }, Schema<"DigestDefinition">>();
  public readonly digestDeletion = new ControlledAction<{ id: string; version: number }, void>();
  public listDigests = () => Promise.resolve(structuredClone(this.digests));
  public createDigest = async (input: Schema<"DigestInput">) => {
    const saved = await this.digestCreation.invoke(input);
    this.digests = [...this.digests, saved];
    return saved;
  };
  public updateDigest = async (id: string, input: RequestBody<"updateDigest">, version: number) => {
    const saved = await this.digestUpdate.invoke({ id, input, version });
    this.digests = this.digests.map((digest) => digest.id === id ? saved : digest);
    return saved;
  };
  public deleteDigest = async (id: string, version: number) => {
    await this.digestDeletion.invoke({ id, version });
    this.digests = this.digests.filter((digest) => digest.id !== id);
  };
  public generateDigest = (digestId: string, requestId: string) => this.digestBuild.invoke({ digestId, requestId });
  public profile = makeProfile();
  public readonly profileUpdate = new ControlledAction<{ input: Partial<Pick<Schema<"PreferenceProfile">, "prefer" | "avoid">>; version: number }, Schema<"PreferenceProfile">>();
  public getPreferenceProfile = () => Promise.resolve(structuredClone(this.profile));
  public updatePreferenceProfile = async (input: Partial<Pick<Schema<"PreferenceProfile">, "prefer" | "avoid">>, version: number) => {
    const value = await this.profileUpdate.invoke({ input, version });
    this.profile = value;
    return value;
  };
  public issue = makeIssue(this.collections.entries);
  public issues = [this.issue];
  public getDigestIssue = (id: string): Promise<Schema<"DigestIssue">> => {
    const issue = this.issues.find((value) => value.id === id);
    return issue ? Promise.resolve(structuredClone(issue)) : Promise.reject(new Error("Digest Issue unavailable."));
  };
  public listDigestIssues = (input: QueryParameters<"listDigestIssues">): Promise<Schema<"DigestIssueList">> => {
    const offset = Number(input.cursor ?? 0);
    const limit = input.limit ?? 20;
    const issues = this.issues.filter((issue) => !input.digestId || issue.digestId === input.digestId).sort((a, b) => Date.parse(b.generatedAt) - Date.parse(a.generatedAt) || b.id.localeCompare(a.id));
    return Promise.resolve({ items: issues.slice(offset, offset + limit).map(({ id, digestId, generationJobId, title, status, scheduledFor, generatedAt }) => ({ id, digestId, generationJobId, title, status, scheduledFor, generatedAt })), nextCursor: offset + limit < issues.length ? String(offset + limit) : null });
  };
  public requestDigestItemSummary = (issueId: string, itemId: string): Promise<JobAccepted> => {
    const saved = this.issue.sections.flatMap((section) => section.items).find(({ item }) => item.id === itemId);
    return saved && this.issue.id === issueId ? this.summaryGeneration.invoke({ issueId, itemId, sourceVersion: saved.item.summarySourceVersion }) : Promise.reject(new Error("Saved Item unavailable."));
  };
  public listEmbeddingModelOptions = () => Promise.resolve(this.models.map((model) => ({ model: structuredClone(model), selectable: model.enabled && model.initializationStatus === "ready" })));
  public getRetrievalStatus = () => Promise.resolve(structuredClone(this.retrievalStatus));
  public retrievalStatus = makeRetrievalStatus();
  public readonly models = [makeModel(0), makeModel(1)];
  public readonly jobs = new Map<string, Schema<"Job">>([[fixtureId(400), makeJob()]]);
  public readonly summaries = new Map<string, Schema<"ItemSummary">>();
  public readonly events = new FixtureEvents();
  public feedResponse: Deferred<FeedPage> | null = null;
  public readonly sourceCreation = new ControlledAction<SourceInput, Source>();
  public readonly sourceUpdate = new ControlledAction<{ id: string; input: RequestBody<"updateSource"> }, Source>();
  public readonly sourceDeletion = new ControlledAction<string, void>();
  public updateSource = async (id: string, input: RequestBody<"updateSource">): Promise<Source> => {
    const saved = await this.sourceUpdate.invoke({ id, input });
    this.collections.sources = this.collections.sources.map((source) => source.id === id ? saved : source);
    return saved;
  };
  public deleteSource = async (id: string): Promise<void> => {
    await this.sourceDeletion.invoke(id);
    this.collections.sources = this.collections.sources.filter((source) => source.id !== id);
  };
  public readonly topicCreation = new ControlledAction<TopicInput, Topic>();
  public readonly topicUpdate = new ControlledAction<{ id: string; input: RequestBody<"updateTopic"> }, Topic>();
  public readonly topicDeletion = new ControlledAction<string, void>();
  public updateTopic = async (id: string, input: RequestBody<"updateTopic">): Promise<Topic> => {
    const saved = await this.topicUpdate.invoke({ id, input });
    this.collections.topics = this.collections.topics.map((topic) => topic.id === id ? saved : topic);
    return saved;
  };
  public deleteTopic = async (id: string): Promise<void> => {
    await this.topicDeletion.invoke(id);
    this.collections.topics = this.collections.topics.filter((topic) => topic.id !== id);
  };
  public readonly sourceSync = new ControlledAction<string | null, JobAccepted>();
  public readonly feedback = new ControlledAction<{ itemId: string; sentiment: Feedback["sentiment"]; comment: string | null }, Feedback>();
  public readonly summaryGeneration = new ControlledAction<{ itemId: string; sourceVersion: string; issueId: string | null }, JobAccepted>();
  public readonly imageDownload = new ControlledAction<{ itemId: string; imageId: string }, ArrayBuffer>();
  public downloadItemImage = async (itemId: string, imageId: string): Promise<ItemImageDownload> => ({ bytes: await this.imageDownload.invoke({ itemId, imageId }), contentType: "image/svg+xml" });
  public readonly digestBuild = new ControlledAction<{ digestId: string; requestId: string }, JobAccepted>();
  public readonly jobCancellation = new ControlledAction<string, Schema<"Job">>();
  public cancelJob = (jobId: string) => this.jobCancellation.invoke(jobId);
  public retryJob = (jobId: string) => this.jobAction.invoke({ jobId, action: "retry" });
  public readonly jobAction = new ControlledAction<{ jobId: string; action: "cancel" | "retry" }, JobAccepted>();

  public getFeed = (request: FeedPageRequest): Promise<FeedPage> => {
    if (this.feedResponse) return this.feedResponse.promise;
    const offset = Number(request.cursor ?? 0);
    const items = this.collections.entries.filter(({ item }) => item.visibility === "visible" && (request.topicId === undefined || item.topicIds.includes(request.topicId)));
    return Promise.resolve({ items: items.slice(offset, offset + request.limit),
      nextCursor: offset + request.limit < items.length ? String(offset + request.limit) : null,
      snapshot: "fixture-snapshot", generatedAt: "2026-10-27T09:00:00Z" });
  };

  public refreshFeed = (input: RequestBody<"refreshFeed">): Promise<FeedPage> => this.getFeed({ limit: input.limit ?? 25, cursor: null, snapshot: null, ...(input.topicId ? { topicId: input.topicId } : {}) });
  public searchItems = (request: QueryParameters<"searchItems">): Promise<Schema<"SearchPage">> => {
    const offset = Number(request.cursor ?? 0);
    const entries = this.collections.entries.filter(({ item }) => matchesSearch(item, request));
    const limit = request.limit ?? 20;
    return Promise.resolve({ items: structuredClone(entries.slice(offset, offset + limit)), nextCursor: offset + limit < entries.length ? String(offset + limit) : null });
  };
  public setReadingState = (id: string, state: Item["readingState"]): Promise<void> => {
    this.collections.entries = this.collections.entries.map((entry) => entry.item.id === id ? { ...entry, item: { ...entry.item, readingState: state } } : entry);
    return Promise.resolve();
  };
  public getItem = (id: string): Promise<Item> => {
    const item = this.collections.entries.find((entry) => entry.item.id === id)?.item;
    return item ? Promise.resolve(structuredClone(item)) : Promise.reject(new Error("Item unavailable."));
  };
  public setVisibility = (id: string, visibility: Item["visibility"]): Promise<void> => {
    this.collections.entries = this.collections.entries.map((entry) => entry.item.id === id ? { ...entry, item: { ...entry.item, visibility } } : entry);
    return Promise.resolve();
  };
  private readonly originalTopics = new Map<string, string[]>();
  public setTopicCorrection = (id: string, input: RequestBody<"setItemTopicCorrection">): Promise<void> => {
    this.collections.entries = this.collections.entries.map((entry) => {
      if (entry.item.id !== id) return entry;
      if (!this.originalTopics.has(id)) this.originalTopics.set(id, [...entry.item.topicIds]);
      const topicIds = entry.item.topicIds.filter((topic) => topic !== input.topicId);
      if (input.membership === "include" || (input.membership === "automatic" && this.originalTopics.get(id)!.includes(input.topicId))) topicIds.push(input.topicId);
      return { ...entry, item: { ...entry.item, topicIds } };
    });
    return Promise.resolve();
  };

  public listSources = (): Promise<Source[]> => Promise.resolve(structuredClone(this.collections.sources));
  public listTopics = (): Promise<Topic[]> => Promise.resolve(structuredClone(this.collections.topics));

  public createSource = async (input: SourceInput): Promise<Source> => {
    const source = await this.sourceCreation.invoke(input);
    this.collections.sources = [...this.collections.sources, source];
    return source;
  };

  public createTopic = async (input: TopicInput): Promise<Topic> => {
    const topic = await this.topicCreation.invoke(input);
    this.collections.topics = [...this.collections.topics, topic];
    return topic;
  };

  public syncAllSources = (): Promise<JobAccepted> => this.sourceSync.invoke(null);
  public syncSource = (id: string): Promise<JobAccepted> => this.sourceSync.invoke(id);
  public getJob = (id: string): Promise<Schema<"Job">> => {
    const job = this.jobs.get(id);
    return job ? Promise.resolve(structuredClone(job)) : Promise.reject(new Error("Job unavailable"));
  };
  public listJobs = (input: QueryParameters<"listJobs">): Promise<Schema<"JobList">> => {
    const offset = Number(input.cursor ?? 0);
    const limit = input.limit ?? 30;
    const jobs = [...this.jobs.values()].filter((job) => !input.status || job.status === input.status).sort((a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt) || b.id.localeCompare(a.id));
    return Promise.resolve({ items: jobs.slice(offset, offset + limit), nextCursor: offset + limit < jobs.length ? String(offset + limit) : null });
  };
  public setFeedback = (itemId: string, sentiment: Feedback["sentiment"], comment: string | null): Promise<Feedback> =>
    this.feedback.invoke({ itemId, sentiment, comment });

  public readSummary(item: Item): Schema<"ItemSummary"> {
    return this.summaries.get(`${item.id}:${item.summarySourceVersion}`) ?? makeSummary({
      sourceVersion: item.summarySourceVersion, supported: item.summarySupported,
      canGenerate: item.summarySupported, unavailableReason: item.summarySupported ? null : "unsupported_input"
    });
  }
  public getItemSummary = async (id: string, sourceVersion: string): Promise<Schema<"ItemSummary">> => {
    const saved = this.summaries.get(`${id}:${sourceVersion}`);
    const item = this.collections.entries.find(({ item }) => item.id === id)?.item;
    const supported = item?.summarySupported ?? true;
    return structuredClone(saved ?? makeSummary({ sourceVersion, supported, canGenerate: supported && this.workspaceSettings.generation.enabled, unavailableReason: supported ? this.workspaceSettings.generation.enabled ? null : "generation_disabled" : "unsupported_input" }));
  };
  public requestItemSummary = (id: string): Promise<JobAccepted> => {
    const item = this.collections.entries.find(({ item }) => item.id === id)?.item;
    return item ? this.summaryGeneration.invoke({ itemId: id, sourceVersion: item.summarySourceVersion, issueId: null }) : Promise.reject(new Error("Item unavailable."));
  };
}

function matchesSearch(item: Item, request: QueryParameters<"searchItems">): boolean {
  return (!request.q || item.title.toLowerCase().includes(request.q.toLowerCase()))
    && (!request.topicId || item.topicIds.includes(request.topicId)) && (!request.sourceId || item.sourceIds.includes(request.sourceId))
    && matchesPublicationDate(item, request);
}
function matchesPublicationDate(item: Item, request: QueryParameters<"searchItems">): boolean {
  const published = Date.parse(item.publishedAt ?? item.discoveredAt);
  return (!request.publishedAfter || published >= Date.parse(request.publishedAfter)) && (!request.publishedBefore || published <= Date.parse(request.publishedBefore));
}
