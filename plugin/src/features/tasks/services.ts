import type { TopicStreamClient } from "../../api/client";
export type TasksService = Pick<TopicStreamClient, "listJobs" | "getJob" | "getRetrievalStatus" | "listEmbeddingModelOptions" | "listDigestIssues" | "listDigests" | "getDigestIssue">;
