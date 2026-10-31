import type { TopicStreamClient } from "../../api/client";

export type FeedService = Pick<TopicStreamClient, "getFeed" | "refreshFeed" | "setFeedback" | "listTopics" | "setReadingState" | "setVisibility" | "setTopicCorrection">;
