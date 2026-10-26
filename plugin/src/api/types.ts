export interface Feedback {
  sentiment: "like" | "dislike";
  comment: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface Item {
  id: string;
  sourceId: string;
  title: string;
  url: string;
  canonicalUrl: string;
  excerpt: string;
  contentMarkdown: string;
  authors: string[];
  publishedAt: string | null;
  discoveredAt: string;
  topicIds: string[];
  readingState: "unread" | "read";
  visibility: "visible" | "hidden";
  feedback: Feedback | null;
}

export interface FeedEntry {
  item: Item;
  score: number;
  explanation: {
    summary: string;
    signals: Record<string, number>;
    pipelineVersion: string;
  };
}

export interface FeedPage {
  items: FeedEntry[];
  nextCursor: string | null;
  snapshot: string;
  generatedAt: string;
}

export interface Source {
  id: string;
  name: string;
  kind: "rss" | "atom" | "arxiv" | "github" | "youtube";
  url: string;
  enabled: boolean;
  pollIntervalMinutes: number;
  lastSyncedAt: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface SourceInput {
  name: string;
  kind: "rss" | "atom";
  url: string;
  enabled: boolean;
  pollIntervalMinutes: number;
}

export interface JobAccepted {
  jobId: string;
}

export type ItemKind = "article" | "paper" | "video" | "repository";
export type TopicMode = "ordinary" | "strict";

export interface Topic {
  id: string;
  name: string;
  sourceIds: string[];
  itemKinds: ItemKind[];
  include: string;
  exclude: string;
  mode: TopicMode;
  enabled: boolean;
  revision: number;
  createdAt: string;
  updatedAt: string;
}

export interface TopicInput {
  name: string;
  sourceIds: string[];
  itemKinds: ItemKind[];
  include: string;
  exclude: string;
  mode: TopicMode;
  enabled: boolean;
}
