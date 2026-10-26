import { requestUrl, type RequestUrlParam, type RequestUrlResponse } from "obsidian";

import { httpError } from "./errors";

import type { Feedback, FeedPage, JobAccepted, Source, SourceInput, Topic, TopicInput } from "./types";
import { buildFeedUrl, type FeedPageRequest } from "./url";

export type { FeedPageRequest } from "./url";

export class TopicStreamClient {
  public constructor(private readonly baseUrl: string) {}

  public async getFeed(request: FeedPageRequest): Promise<FeedPage> {
    const response = await this.request<FeedPage>({
      url: buildFeedUrl(this.baseUrl, request),
      method: "GET"
    }, 200);
    return response;
  }

  public async setFeedback(itemId: string, sentiment: "like" | "dislike", comment: string | null): Promise<Feedback> {
    const url = new URL(`/api/v1/items/${encodeURIComponent(itemId)}/feedback`, normalizedBaseUrl(this.baseUrl));
    const response = await this.request<Feedback>({
      url: url.toString(),
      method: "PUT",
      contentType: "application/json",
      body: JSON.stringify({ sentiment, comment })
    }, 200);
    return response;
  }

  public async listSources(): Promise<Source[]> {
    const response = await this.request<{ items: Source[] }>({
      url: new URL("/api/v1/sources", normalizedBaseUrl(this.baseUrl)).toString(),
      method: "GET"
    }, 200);
    return response.items;
  }

  public async createSource(input: SourceInput): Promise<Source> {
    const response = await this.request<Source>({
      url: new URL("/api/v1/sources", normalizedBaseUrl(this.baseUrl)).toString(),
      method: "POST",
      contentType: "application/json",
      body: JSON.stringify(input)
    }, 201);
    return response;
  }

  public async syncAllSources(): Promise<JobAccepted> {
    const response = await this.request<JobAccepted>({
      url: new URL("/api/v1/sources/sync", normalizedBaseUrl(this.baseUrl)).toString(),
      method: "POST"
    }, 202);
    return response;
  }

  public async listTopics(): Promise<Topic[]> {
	const response = await this.request<{ items: Topic[] }>({
	  url: new URL("/api/v1/topics", normalizedBaseUrl(this.baseUrl)).toString(),
	  method: "GET"
	}, 200);
	return response.items;
  }

  public async createTopic(input: TopicInput): Promise<Topic> {
	const response = await this.request<Topic>({
	  url: new URL("/api/v1/topics", normalizedBaseUrl(this.baseUrl)).toString(),
	  method: "POST",
	  contentType: "application/json",
	  body: JSON.stringify(input)
	}, 201);
	return response;
  }

  private async request<T>(options: RequestUrlParam, expectedStatus: number): Promise<T> {
    let response: RequestUrlResponse;
    try {
      response = await requestUrl({ ...options, throw: false });
    } catch (error) {
      if (typeof error === "object" && error !== null && "name" in error && error.name === "AbortError") throw error;
      throw new Error("Could not connect to Topic Stream.");
    }
    if (response.status !== expectedStatus) {
      throw httpError(response.status, response.text);
    }
    try {
      const value: unknown = response.json;
      if (value === undefined || value === null) throw new Error("Missing response data");
      return value as T;
    } catch {
      throw new Error("Topic Stream returned an invalid response.");
    }
  }
}

function normalizedBaseUrl(baseUrl: string): string {
  return baseUrl.endsWith("/") ? baseUrl : `${baseUrl}/`;
}
