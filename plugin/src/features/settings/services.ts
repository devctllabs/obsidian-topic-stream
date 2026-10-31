import type { TopicStreamClient } from "../../api/client";
import type { RequestBody, Schema, Source, Topic } from "../../api/contracts";

export type SourceService = Pick<TopicStreamClient, "listSources" | "createSource" | "syncAllSources">;
export type TopicService = Pick<TopicStreamClient, "listSources" | "listTopics" | "createTopic">;
export interface PreferenceProfileService {
  getPreferenceProfile: () => Promise<Schema<"PreferenceProfile">>;
  updatePreferenceProfile: (input: RequestBody<"updatePreferenceProfile">, version: number) => Promise<Schema<"PreferenceProfile">>;
}
export interface SourceEditingService {
  updateSource: (id: string, input: RequestBody<"updateSource">) => Promise<Source>;
  deleteSource: (id: string) => Promise<void>;
}
export interface TopicEditingService {
  updateTopic: (id: string, input: RequestBody<"updateTopic">) => Promise<Topic>;
  deleteTopic: (id: string) => Promise<void>;
}
export type TopicsWorkspaceService = TopicService & TopicEditingService;

export type DigestDefinitionService = Pick<TopicStreamClient, "listDigests" | "createDigest" | "updateDigest" | "deleteDigest" | "listTopics">;

export type WorkspaceSettingsService = Pick<TopicStreamClient, "getWorkspaceSettings" | "updateWorkspaceSettings" | "listEmbeddingModelOptions" | "getRetrievalStatus" | "getJob">;
