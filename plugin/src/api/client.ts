import { httpError } from "./errors";

import type { Feedback, FeedPage, ItemImageDownload, JobAccepted, PathParameters, QueryParameters, RequestBody, ResponseBody, Source, SourceInput, Topic, TopicInput } from "./contracts";
import { isCancellation, waitForResponse, type HttpRequest, type HttpResponse, type HttpTransport } from "./transport";
import { buildFeedUrl, type FeedPageRequest } from "./url";
import type { Schema } from "./contracts";

export type { FeedPageRequest } from "./url";

export class TopicStreamClient {
  public constructor(private readonly baseUrl: string, private readonly transport: HttpTransport, private readonly signal?: AbortSignal) {}

  public async getFeed(request: FeedPageRequest): Promise<FeedPage> {
    const response = await this.request<FeedPage>({
      url: buildFeedUrl(this.baseUrl, request),
      method: "GET"
    }, 200);
    return response;
  }

  public refreshFeed(input: RequestBody<"refreshFeed">): Promise<FeedPage> {
    return this.request({ url: new URL("/api/v1/feed/refresh", normalizedBaseUrl(this.baseUrl)).toString(), method: "POST", contentType: "application/json", body: JSON.stringify(input) }, 200);
  }

  public searchItems(input: QueryParameters<"searchItems">): Promise<Schema<"SearchPage">> {
    const url = new URL("/api/v1/search", normalizedBaseUrl(this.baseUrl));
    for (const [name, value] of Object.entries(input)) if (value !== undefined) url.searchParams.set(name, String(value));
    return this.request({ url: url.toString(), method: "GET" }, 200);
  }

  public getItem(itemId: string): Promise<Schema<"Item">> {
    return this.request({ url: new URL(`/api/v1/items/${encodeURIComponent(itemId)}`, normalizedBaseUrl(this.baseUrl)).toString(), method: "GET" }, 200);
  }

  public getItemSummary(itemId: string, sourceVersion: string): Promise<Schema<"ItemSummary">> {
    const url = new URL(`/api/v1/items/${encodeURIComponent(itemId)}/ai-summary`, normalizedBaseUrl(this.baseUrl));
    url.searchParams.set("sourceVersion", sourceVersion);
    return this.request({ url: url.toString(), method: "GET" }, 200);
  }
  public requestItemSummary(itemId: string): Promise<JobAccepted> {
    return this.request({ url: new URL(`/api/v1/items/${encodeURIComponent(itemId)}/ai-summary`, normalizedBaseUrl(this.baseUrl)).toString(), method: "POST", contentType: "application/json", body: "{}" }, 202);
  }
  public requestDigestItemSummary(issueId: string, itemId: string): Promise<JobAccepted> {
    return this.request({ url: new URL(`/api/v1/digest-issues/${encodeURIComponent(issueId)}/items/${encodeURIComponent(itemId)}/ai-summary`, normalizedBaseUrl(this.baseUrl)).toString(), method: "POST", contentType: "application/json", body: "{}" }, 202);
  }
  public getDigestIssue(issueId: string): Promise<Schema<"DigestIssue">> {
    return this.request({ url: new URL(`/api/v1/digest-issues/${encodeURIComponent(issueId)}`, normalizedBaseUrl(this.baseUrl)).toString(), method: "GET" }, 200);
  }
  public listDigestIssues(input: QueryParameters<"listDigestIssues">): Promise<Schema<"DigestIssueList">> {
    const url = new URL("/api/v1/digest-issues", normalizedBaseUrl(this.baseUrl));
    for (const [name, value] of Object.entries(input)) if (value !== undefined) url.searchParams.set(name, String(value));
    return this.request({ url: url.toString(), method: "GET" }, 200);
  }

  public async downloadItemImage(itemId: string, imageId: string): Promise<ItemImageDownload> {
    const response = await this.response({ url: new URL(`/api/v1/items/${encodeURIComponent(itemId)}/images/${encodeURIComponent(imageId)}`, normalizedBaseUrl(this.baseUrl)).toString(), method: "GET" }, 200);
    const bytes = response.arrayBuffer;
    const contentType = Object.entries(response.headers ?? {}).find(([name]) => name.toLowerCase() === "content-type")?.[1].split(";")[0]?.trim().toLowerCase();
    if (!bytes || !contentType?.startsWith("image/") || bytes.byteLength === 0 || bytes.byteLength > 10 * 1024 * 1024) throw new Error("Topic Stream returned an invalid image.");
    return { bytes, contentType };
  }

  public setReadingState(itemId: string, state: RequestBody<"setItemReadingState">["state"]): Promise<void> {
    return this.request({ url: new URL(`/api/v1/items/${encodeURIComponent(itemId)}/reading-state`, normalizedBaseUrl(this.baseUrl)).toString(), method: "PUT", contentType: "application/json", body: JSON.stringify({ state } satisfies RequestBody<"setItemReadingState">) }, 204);
  }
  public setVisibility(itemId: string, visibility: RequestBody<"setItemVisibility">["visibility"]): Promise<void> {
    return this.request({ url: new URL(`/api/v1/items/${encodeURIComponent(itemId)}/visibility`, normalizedBaseUrl(this.baseUrl)).toString(), method: "PUT", contentType: "application/json", body: JSON.stringify({ visibility } satisfies RequestBody<"setItemVisibility">) }, 204);
  }
  public setTopicCorrection(itemId: string, input: RequestBody<"setItemTopicCorrection">): Promise<void> {
    return this.request({ url: new URL(`/api/v1/items/${encodeURIComponent(itemId)}/topic-corrections`, normalizedBaseUrl(this.baseUrl)).toString(), method: "PUT", contentType: "application/json", body: JSON.stringify(input) }, 204);
  }

  public async setFeedback(itemId: PathParameters<"setItemFeedback">["itemId"], sentiment: RequestBody<"setItemFeedback">["sentiment"], comment: RequestBody<"setItemFeedback">["comment"]): Promise<Feedback> {
    const url = new URL(`/api/v1/items/${encodeURIComponent(itemId)}/feedback`, normalizedBaseUrl(this.baseUrl));
    const input: RequestBody<"setItemFeedback"> = { sentiment, comment };
    const response = await this.request<Feedback>({
      url: url.toString(),
      method: "PUT",
      contentType: "application/json",
      body: JSON.stringify(input)
    }, 200);
    return response;
  }

  public async listSources(): Promise<Source[]> {
    const response = await this.request<ResponseBody<"listSources", 200>>({
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
	const response = await this.request<ResponseBody<"listTopics", 200>>({
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

  public getPreferenceProfile(): Promise<Schema<"PreferenceProfile">> {
    return this.request({ url: new URL("/api/v1/preference-profile", normalizedBaseUrl(this.baseUrl)).toString(), method: "GET" }, 200);
  }
  public updatePreferenceProfile(input: RequestBody<"updatePreferenceProfile">, version: number): Promise<Schema<"PreferenceProfile">> {
    return this.request({
      url: new URL("/api/v1/preference-profile", normalizedBaseUrl(this.baseUrl)).toString(), method: "PATCH",
      headers: { "If-Match": `"${version}"` }, contentType: "application/json", body: JSON.stringify(input)
    }, 200);
  }
  public syncSource(id: string): Promise<JobAccepted> {
    return this.request({ url: new URL(`/api/v1/sources/${encodeURIComponent(id)}/sync`, normalizedBaseUrl(this.baseUrl)).toString(), method: "POST" }, 202);
  }
  public getJob(id: string): Promise<Schema<"Job">> {
    return this.request({ url: new URL(`/api/v1/jobs/${encodeURIComponent(id)}`, normalizedBaseUrl(this.baseUrl)).toString(), method: "GET" }, 200);
  }
  public listJobs(input: QueryParameters<"listJobs">): Promise<Schema<"JobList">> {
    const url = new URL("/api/v1/jobs", normalizedBaseUrl(this.baseUrl));
    for (const [name, value] of Object.entries(input)) if (value !== undefined) url.searchParams.set(name, String(value));
    return this.request({ url: url.toString(), method: "GET" }, 200);
  }
  public updateSource(id: string, input: RequestBody<"updateSource">): Promise<Source> {
    return this.request({ url: new URL(`/api/v1/sources/${encodeURIComponent(id)}`, normalizedBaseUrl(this.baseUrl)).toString(), method: "PATCH", contentType: "application/json", body: JSON.stringify(input) }, 200);
  }
  public deleteSource(id: string): Promise<void> {
    return this.request({ url: new URL(`/api/v1/sources/${encodeURIComponent(id)}`, normalizedBaseUrl(this.baseUrl)).toString(), method: "DELETE" }, 204);
  }
  public updateTopic(id: string, input: RequestBody<"updateTopic">): Promise<Topic> {
    return this.request({ url: new URL(`/api/v1/topics/${encodeURIComponent(id)}`, normalizedBaseUrl(this.baseUrl)).toString(), method: "PATCH", contentType: "application/json", body: JSON.stringify(input) }, 200);
  }
  public deleteTopic(id: string): Promise<void> {
    return this.request({ url: new URL(`/api/v1/topics/${encodeURIComponent(id)}`, normalizedBaseUrl(this.baseUrl)).toString(), method: "DELETE" }, 204);
  }

  public async listDigests(): Promise<Schema<"DigestDefinition">[]> {
    const value = await this.request<Schema<"DigestList">>({ url: new URL("/api/v1/digests", normalizedBaseUrl(this.baseUrl)).toString(), method: "GET" }, 200);
    return value.items;
  }
  public createDigest(input: RequestBody<"createDigest">): Promise<Schema<"DigestDefinition">> {
    return this.request({ url: new URL("/api/v1/digests", normalizedBaseUrl(this.baseUrl)).toString(), method: "POST", contentType: "application/json", body: JSON.stringify(input) }, 201);
  }
  public updateDigest(id: string, input: RequestBody<"updateDigest">, version: number): Promise<Schema<"DigestDefinition">> {
    return this.request({ url: new URL(`/api/v1/digests/${encodeURIComponent(id)}`, normalizedBaseUrl(this.baseUrl)).toString(), method: "PATCH", headers: { "If-Match": `"${version}"` }, contentType: "application/json", body: JSON.stringify(input) }, 200);
  }
  public deleteDigest(id: string, version: number): Promise<void> {
    return this.request({ url: new URL(`/api/v1/digests/${encodeURIComponent(id)}`, normalizedBaseUrl(this.baseUrl)).toString(), method: "DELETE", headers: { "If-Match": `"${version}"` } }, 204);
  }
  public generateDigest(id: string, requestId: string): Promise<JobAccepted> {
    return this.request({ url: new URL(`/api/v1/digests/${encodeURIComponent(id)}/generate`, normalizedBaseUrl(this.baseUrl)).toString(), method: "POST", headers: { "Idempotency-Key": requestId } }, 202);
  }
  public getSystemStatus(): Promise<Schema<"SystemStatus">> {
    return this.request({ url: new URL("/api/v1/system/status", normalizedBaseUrl(this.baseUrl)).toString(), method: "GET" }, 200);
  }

  public getWorkspaceSettings(): Promise<Schema<"WorkspaceSettings">> {
    return this.request({ url: new URL("/api/v1/workspace-settings", normalizedBaseUrl(this.baseUrl)).toString(), method: "GET" }, 200);
  }
  public updateWorkspaceSettings(input: RequestBody<"updateWorkspaceSettings">, version: number): Promise<Schema<"WorkspaceSettings">> {
    return this.request({ url: new URL("/api/v1/workspace-settings", normalizedBaseUrl(this.baseUrl)).toString(), method: "PATCH", headers: { "If-Match": `"${version}"` }, contentType: "application/json", body: JSON.stringify(input) }, 200);
  }

  public async listEmbeddingModelOptions(): Promise<Schema<"EmbeddingModelOption">[]> {
    const value = await this.request<Schema<"EmbeddingModelOptionList">>({ url: new URL("/api/v1/embedding-model-options", normalizedBaseUrl(this.baseUrl)).toString(), method: "GET" }, 200);
    return value.items;
  }
  public getRetrievalStatus(): Promise<Schema<"RetrievalStatus">> {
    return this.request({ url: new URL("/api/v1/retrieval-status", normalizedBaseUrl(this.baseUrl)).toString(), method: "GET" }, 200);
  }

  public cancelJob(id: string): Promise<Schema<"Job">> {
    return this.request({ url: new URL(`/api/v1/jobs/${encodeURIComponent(id)}/cancel`, normalizedBaseUrl(this.baseUrl)).toString(), method: "POST" }, 202);
  }
  public retryJob(id: string): Promise<JobAccepted> {
    return this.request({ url: new URL(`/api/v1/jobs/${encodeURIComponent(id)}/retry`, normalizedBaseUrl(this.baseUrl)).toString(), method: "POST" }, 202);
  }

  private async request<T>(options: HttpRequest, expectedStatus: number): Promise<T> {
    const response = await this.response(options, expectedStatus);
    if (expectedStatus === 204) return undefined as T;
    return responseJson<T>(response);
  }

  private async response(options: HttpRequest, expectedStatus: number): Promise<HttpResponse> {
    let response;
    try {
      response = await waitForResponse(this.transport, options, this.signal);
      if (this.signal?.aborted) throw new DOMException("Connection closed", "AbortError");
    } catch (error) {
      if (isCancellation(error)) throw error;
      throw new Error("Could not connect to Topic Stream.");
    }
    if (response.status !== expectedStatus) {
      throw httpError(response.status, response.text ?? "");
    }
    return response;
  }
}

function responseJson<T>(response: HttpResponse): T {
  try {
    const value: unknown = response.json;
    if (value === undefined || value === null) throw new Error("Missing response data");
    return value as T;
  } catch {
    throw new Error("Topic Stream returned an invalid response.");
  }
}

function normalizedBaseUrl(baseUrl: string): string {
  return baseUrl.endsWith("/") ? baseUrl : `${baseUrl}/`;
}
