import type { operations } from "./generated/openapi";

type FeedQuery = NonNullable<operations["getFeed"]["parameters"]["query"]>;
export type FeedPageRequest = Required<Pick<FeedQuery, "limit">> & Pick<FeedQuery, "topicId"> & {
  cursor: string | null;
  snapshot: string | null;
};

export const FEED_PAGE_SIZE = 25;

export function buildFeedUrl(baseUrl: string, request: FeedPageRequest): string {
  const url = new URL("/api/v1/feed", normalizedBaseUrl(baseUrl));
  url.searchParams.set("limit", String(request.limit));
  if (request.topicId !== undefined) url.searchParams.set("topicId", request.topicId);
  if (request.cursor !== null) {
    url.searchParams.set("cursor", request.cursor);
  }
  if (request.snapshot !== null) {
    url.searchParams.set("snapshot", request.snapshot);
  }
  return url.toString();
}

function normalizedBaseUrl(baseUrl: string): string {
  return baseUrl.endsWith("/") ? baseUrl : `${baseUrl}/`;
}

export function normalizeServerUrl(value: string): string {
  try {
    const url = new URL(value.trim());
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.search || url.hash) {
      throw new Error("Invalid server URL");
    }
    return url.toString().replace(/\/+$/, "");
  } catch {
    throw new Error("Enter an HTTP or HTTPS server URL without credentials, query or fragment.");
  }
}
