import type { FeedEntry, Item, Schema, Source, Topic } from "../../api/contracts";

export const FIXTURE_TIME = "2026-10-27T09:00:00Z";
export const FIXTURE_VERSION = "a".repeat(64);
export const fixtureId = (value: number): string => `00000000-0000-4000-8000-${String(value).padStart(12, "0")}`;
export function makeProfile(): Schema<"PreferenceProfile"> {
  return { prefer: "Practical engineering", avoid: "Promotional content", version: 1, revision: 1, updatedAt: FIXTURE_TIME };
}

export function makeSource(index = 0, overrides: Partial<Source> = {}): Source {
  return {
    id: fixtureId(100 + index), name: `Source ${index + 1}`,
    config: { kind: "rss", url: `https://example.org/source-${index + 1}.xml`, allowPrivateNetwork: false },
    enabled: true, pollIntervalMinutes: 60, lastSyncedAt: FIXTURE_TIME,
    createdAt: FIXTURE_TIME, updatedAt: FIXTURE_TIME, ...overrides
  };
}

export function makeTopic(index = 0, overrides: Partial<Topic> = {}): Topic {
  return {
    id: fixtureId(200 + index), name: ["Systems", "Retrieval", "Tools", "Research", "Reading"][index % 5] + ` ${index + 1}`,
    sourceIds: [], itemKinds: [], include: "Practical engineering and research", exclude: "",
    mode: "ordinary", enabled: true, revision: 1, createdAt: FIXTURE_TIME, updatedAt: FIXTURE_TIME, ...overrides
  };
}

export function makeItem(index = 0, overrides: Partial<Item> = {}): Item {
  return {
    id: fixtureId(300 + index), sourceIds: [fixtureId(100 + index % 20)], kind: "article",
    details: { type: "article", description: "Synthetic article fixture" },
    title: ["A closer look at retrieval systems", "Notas de ingeniería sobre búsquedas", "Designing a durable reading workspace"][index % 3] + ` · ${index + 1}`,
    url: `https://example.org/items/${index + 1}`, canonicalUrl: `https://example.org/items/${index + 1}`,
    excerpt: "A synthetic excerpt about practical tradeoffs, careful measurement, and reliable systems.",
    contentMarkdown: "## Context\n\nSynthetic article content for a deterministic UI fixture.\n\n## Observations\n\nA deliberately ordinary paragraph with a [source](https://example.org).",
    contentOrigin: "article_body", summarySupported: true, summarySourceVersion: FIXTURE_VERSION,
    authors: ["Example Author"], publishedAt: FIXTURE_TIME, discoveredAt: FIXTURE_TIME,
    topicIds: [fixtureId(200 + index % 10)], readingState: "unread", visibility: "visible",
    feedback: null, images: [], ...overrides
  };
}

export function makeEntry(index = 0, overrides: Partial<Item> = {}): FeedEntry {
  return { item: makeItem(index, overrides), score: 0.8, explanation: {
    summary: "Matched the saved Topic criteria", signals: { semantic: 0.8 }, pipelineVersion: "synthetic-fixture-v1"
  } };
}

export function makeJob(overrides: Partial<Schema<"Job">> = {}): Schema<"Job"> {
  return {
    id: fixtureId(400), kind: "reindex-workspace", queue: "maintenance", status: "running",
    attempt: 1, maxAttempts: 5, cancellationRequested: false,
    createdAt: FIXTURE_TIME, updatedAt: FIXTURE_TIME, ...overrides
  };
}

export function makeSummary(overrides: Partial<Schema<"ItemSummary">> = {}): Schema<"ItemSummary"> {
  const state = overrides.state ?? "not_requested";
  const sourceVersion = overrides.sourceVersion ?? FIXTURE_VERSION;
  return {
    sourceVersion, supported: true, canGenerate: true, unavailableReason: null, state,
    jobId: state === "not_requested" ? null : fixtureId(401), failure: state === "failed" ? "Synthetic provider failure" : null,
    retryable: state === "failed",
    result: state === "ready" ? {
      id: fixtureId(402), generationJobId: fixtureId(401), sourceVersion,
      text: "Synthetic AI summary fixture. This text is not provider evidence.",
      model: "fixture-summary-model", language: "same-as-source", maxInputChars: 48_000,
      maxSummaryCharacters: 800, instructionsVersion: "fixture-v1", algorithmVersion: "fixture-v1", createdAt: FIXTURE_TIME
    } : null,
    ...overrides
  };
}

export function makeModel(index = 0): Schema<"EmbeddingModel"> {
  return {
    id: fixtureId(600 + index), name: `Fixture model ${index + 1}`, endpoint: "http://provider.invalid/v1",
    providerModel: "fixture-embedding", dimensions: 1024, enabled: true, initializationStatus: "ready",
    failureReason: null, version: 1, createdAt: FIXTURE_TIME, updatedAt: FIXTURE_TIME
  };
}

export function makeRetrievalStatus(overrides: Partial<Schema<"RetrievalStatus">> = {}): Schema<"RetrievalStatus"> {
  return {
    desiredModelId: fixtureId(601), servingModelId: fixtureId(600), transitionJobId: fixtureId(400),
    latestTransition: { modelId: fixtureId(601), selectionGeneration: 1, jobId: fixtureId(400), status: "running", isCurrent: true, canRetry: false },
    ...overrides
  };
}

export function makeIssue(items = Array.from({ length: 30 }, (_, index) => makeEntry(index))): Schema<"DigestIssue"> {
  return {
    id: fixtureId(500), digestId: fixtureId(501), generationJobId: fixtureId(502),
    title: "Research notes for the week", status: "complete", scheduledFor: null, generatedAt: FIXTURE_TIME,
    sections: Array.from({ length: 5 }, (_, index) => ({
      topicId: fixtureId(200 + index), topicName: makeTopic(index).name, topicRevision: 1, failure: null,
      items: items.filter((_, position) => position % 5 === index).map((entry) => ({ item: entry.item, summary: entry.item.excerpt, explanation: entry.explanation, aiSummary: null }))
    }))
  };
}

export function makeCollections(counts = { sources: 20, topics: 10, items: 30 }) {
  return {
    sources: Array.from({ length: counts.sources }, (_, index) => makeSource(index)),
    topics: Array.from({ length: counts.topics }, (_, index) => makeTopic(index)),
    entries: Array.from({ length: counts.items }, (_, index) => makeEntry(index))
  };
}

export function makeContentCases(): Record<"article" | "readme" | "abstract" | "description" | "missing", Item> {
  return {
    article: makeItem(),
    readme: makeItem(1, {
      kind: "repository", contentOrigin: "repository_readme", title: "Fixture repository README",
      contentMarkdown: "# Fixture repository\n\nA synthetic README for summary/content fixtures.",
      details: {
        type: "repository", description: "Synthetic repository fixture", homepage: null, programmingLanguage: "TypeScript",
        stars: 100, forks: 10, openIssues: 2, topics: ["retrieval"], defaultBranch: "main", starsInPeriod: 5,
        rank: 1, period: "daily", observedAt: FIXTURE_TIME, snapshotStatus: "complete"
      }
    }),
    abstract: makeItem(2, {
      kind: "paper", contentOrigin: "paper_abstract", summarySupported: false,
      details: { type: "paper", abstract: "A synthetic paper abstract.", categories: ["cs.IR"], doi: null },
      contentMarkdown: "A synthetic paper abstract."
    }),
    description: makeItem(3, { contentOrigin: "article_description", summarySupported: false, contentMarkdown: "Only a synthetic article description." }),
    missing: makeItem(4, { contentOrigin: "empty", summarySupported: false, contentMarkdown: "" })
  };
}

export function makeWorkspaceSettings(overrides: Partial<Schema<"WorkspaceSettings">> = {}): Schema<"WorkspaceSettings"> {
  return { version: 1, embeddingModelId: fixtureId(601),
    generation: { enabled: false, endpoint: null, summaryModel: null, rerankModel: null },
    summary: { language: "same-as-source", maxInputChars: 48000, maxSummaryCharacters: 800 },
    instructions: { summaryChunk: "Extract factual points.", summaryReduce: "Combine the supplied facts.", summaryFinal: "Write a concise summary.", digestRerank: "Rank using reader preferences." },
    retention: { fullContentDays: null },
    retrieval: { candidateLimit: 50, rerankLimit: 30, rrfK: 60, recencyHalfLifeDays: 7, recencyMaxBoost: 0.1, feedSourceCap: 3, digestSourceCap: 2, digestMmrLambda: 0.75, llmRerankEnabled: false, digestExplorationEnabled: true },
    updatedAt: FIXTURE_TIME, ...overrides
  };
}

export function makeDigestDefinition(overrides: Partial<Schema<"DigestDefinition">> = {}): Schema<"DigestDefinition"> {
  return { id: fixtureId(900), name: "Manual research", topics: [{ topicId: fixtureId(200), itemLimit: 5 }], enabled: false, timezone: "Europe/Madrid", recurrence: { frequency: "weekly", weekdays: [1], hour: 12, minute: 0 }, version: 1, nextRunAt: null, lastSuccessfulAt: null, createdAt: FIXTURE_TIME, updatedAt: FIXTURE_TIME, ...overrides };
}
