import type { TopicStreamClient } from "../../api/client";
export type DigestsService = Pick<TopicStreamClient, "listDigestIssues" | "listDigests" | "getDigestIssue">;
