import type { Feedback, SourceInput, TopicInput } from "../../api/contracts";
import { FixtureScenario } from "./scenario";
import { FIXTURE_TIME, fixtureId, makeJob, makeSource, makeTopic, makeSummary } from "./fixtures";
import { fixtureImage } from "./images";

/** Interactive catalog default. ControlledAction remains available for pending/error tests. */
export function interactiveScenario(): FixtureScenario {
  const scenario = new FixtureScenario();
  scenario.downloadItemImage = (itemId, imageId) => {
    const item = scenario.collections.entries.find(({ item }) => item.id === itemId)?.item;
    return item?.images.some((image) => image.id === imageId) ? Promise.resolve(fixtureImage()) : Promise.reject(new Error("Image unavailable."));
  };
  scenario.requestItemSummary = async (id) => {
    const item = await scenario.getItem(id);
    const state = await scenario.getItemSummary(id, item.summarySourceVersion);
    if (state.state === "ready" && state.jobId) return { jobId: state.jobId };
    if (!state.canGenerate) throw new Error("Summary generation is unavailable.");
    const job = makeJob({ id: fixtureId(700 + scenario.jobs.size), kind: "summarize-item", queue: "generation", status: "succeeded" });
    const summary = makeSummary({ state: "ready", sourceVersion: item.summarySourceVersion, jobId: job.id });
    scenario.jobs.set(job.id, job);
    if (summary.result) summary.result = { ...summary.result, generationJobId: job.id };
    scenario.summaries.set(`${id}:${item.summarySourceVersion}`, summary);
    return { jobId: job.id };
  };
  scenario.requestDigestItemSummary = async (issueId, itemId) => {
    const saved = scenario.issues.find((issue) => issue.id === issueId)?.sections.flatMap((section) => section.items).find(({ item }) => item.id === itemId);
    if (!saved?.aiSummary?.canGenerate || !scenario.workspaceSettings.generation.enabled) throw new Error("Summary generation is unavailable.");
    const job = makeJob({ id: fixtureId(700 + scenario.jobs.size), kind: "summarize-item", queue: "generation", status: "succeeded" });
    scenario.jobs.set(job.id, job); saved.aiSummary = makeSummary({ state: "ready", sourceVersion: saved.item.summarySourceVersion, jobId: job.id });
    return { jobId: job.id };
  };
  scenario.cancelJob = (id) => {
    const value = scenario.jobs.get(id);
    if (!value) return Promise.reject(new Error("Job not found"));
    const job = { ...value, cancellationRequested: true };
    scenario.jobs.set(id, job); return Promise.resolve(job);
  };
  scenario.retryJob = (id) => {
    const source = scenario.jobs.get(id);
    if (!source) return Promise.reject(new Error("Job not found"));
    const job = { ...source, id: fixtureId(700 + scenario.jobs.size), status: "queued" as const, cancellationRequested: false };
    scenario.jobs.set(job.id, job);
    if (job.modelTransition) scenario.retrievalStatus = { ...scenario.retrievalStatus, transitionJobId: job.id, latestTransition: { ...job.modelTransition, jobId: job.id, status: "queued", canRetry: false } };
    return Promise.resolve({ jobId: job.id });
  };
  scenario.updateWorkspaceSettings = (input, version) => {
    if (input.embeddingModelId && input.embeddingModelId !== scenario.workspaceSettings.embeddingModelId) {
      const jobId = fixtureId(700 + scenario.jobs.size);
      const transition = { modelId: input.embeddingModelId, selectionGeneration: (scenario.retrievalStatus.latestTransition?.selectionGeneration ?? 0) + 1, jobId, status: "queued" as const, isCurrent: true, canRetry: false };
      scenario.retrievalStatus = { ...scenario.retrievalStatus, desiredModelId: input.embeddingModelId, transitionJobId: jobId, latestTransition: transition };
      scenario.jobs.set(jobId, makeJob({ id: jobId, status: "queued", modelTransition: transition }));
    }
    scenario.workspaceSettings = { ...scenario.workspaceSettings, ...input, version: version + 1 };
    return Promise.resolve(structuredClone(scenario.workspaceSettings));
  };
  scenario.updatePreferenceProfile = (input, version) => {
    scenario.profile = { ...scenario.profile, ...input, version: version + 1, revision: scenario.profile.revision + 1 };
    return Promise.resolve(scenario.profile);
  };
  scenario.setFeedback = (itemId, sentiment, comment) => {
    const feedback: Feedback = { sentiment, comment, createdAt: FIXTURE_TIME, updatedAt: FIXTURE_TIME };
    const entry = scenario.collections.entries.find(({ item }) => item.id === itemId);
    if (entry) entry.item.feedback = feedback;
    return Promise.resolve(feedback);
  };
  scenario.createSource = (input: SourceInput) => {
    const source = makeSource(scenario.collections.sources.length, input);
    scenario.collections.sources.push(source);
    return Promise.resolve(source);
  };
  scenario.updateSource = (id, input) => {
    const source = scenario.collections.sources.find((value) => value.id === id);
    if (!source) return Promise.reject(new Error("Source not found"));
    const saved = { ...source, ...input };
    scenario.collections.sources = scenario.collections.sources.map((value) => value.id === id ? saved : value);
    return Promise.resolve(saved);
  };
  scenario.deleteSource = (id) => {
    scenario.collections.sources = scenario.collections.sources.filter((source) => source.id !== id);
    return Promise.resolve();
  };
  scenario.syncSource = (id) => {
    const job = makeJob({ id: fixtureId(700 + scenario.jobs.size), kind: "sync-source", queue: "fetch", status: "succeeded" });
    scenario.jobs.set(job.id, job);
    const source = scenario.collections.sources.find((value) => value.id === id);
    if (source) source.lastSyncedAt = FIXTURE_TIME;
    return Promise.resolve({ jobId: job.id });
  };
  scenario.createTopic = (input: TopicInput) => {
    const topic = makeTopic(scenario.collections.topics.length, input);
    scenario.collections.topics.push(topic);
    return Promise.resolve(topic);
  };
  scenario.updateTopic = (id, input) => {
    const topic = scenario.collections.topics.find((value) => value.id === id);
    if (!topic) return Promise.reject(new Error("Topic not found"));
    const value = { ...topic, ...input };
    const changed = ["include", "exclude", "mode", "sourceIds", "itemKinds"].some((field) => JSON.stringify(topic[field as keyof typeof topic]) !== JSON.stringify(value[field as keyof typeof value]));
    const saved = { ...value, revision: topic.revision + Number(changed) };
    scenario.collections.topics = scenario.collections.topics.map((value) => value.id === id ? saved : value);
    return Promise.resolve(saved);
  };
  scenario.deleteTopic = (id) => {
    scenario.collections.topics = scenario.collections.topics.filter((topic) => topic.id !== id);
    return Promise.resolve();
  };
  scenario.syncAllSources = () => {
    const job = makeJob({ id: fixtureId(700 + scenario.jobs.size), kind: "sync-all-sources", queue: "fetch", status: "succeeded" });
    scenario.jobs.set(job.id, job);
    return Promise.resolve({ jobId: job.id });
  };
  scenario.createDigest = (input) => {
    const saved = { ...input, id: fixtureId(900 + scenario.digests.length), version: 1, nextRunAt: input.enabled ? "2026-11-02T12:00:00Z" : null, lastSuccessfulAt: null, createdAt: FIXTURE_TIME, updatedAt: FIXTURE_TIME };
    scenario.digests.push(saved);
    return Promise.resolve(saved);
  };
  scenario.updateDigest = (id, input, version) => {
    const digest = scenario.digests.find((value) => value.id === id);
    if (!digest) return Promise.reject(new Error("Definition not found"));
    const saved = { ...digest, ...input, version: version + 1, nextRunAt: (input.enabled ?? digest.enabled) ? "2026-11-02T12:00:00Z" : null };
    scenario.digests = scenario.digests.map((value) => value.id === id ? saved : value);
    return Promise.resolve(saved);
  };
  scenario.deleteDigest = (id) => {
    scenario.digests = scenario.digests.filter((digest) => digest.id !== id);
    return Promise.resolve();
  };
  scenario.generateDigest = () => {
    const job = makeJob({ id: fixtureId(700 + scenario.jobs.size), kind: "generate-digest", queue: "generation", status: "queued" });
    scenario.jobs.set(job.id, job);
    return Promise.resolve({ jobId: job.id });
  };
  return scenario;
}
