export interface FeedPageRequest {
  limit: number;
  cursor: string | null;
  snapshot: string | null;
}

export const FEED_PAGE_SIZE = 25;

export function buildFeedUrl(baseUrl: string, request: FeedPageRequest): string {
  const url = new URL("/api/v1/feed", normalizedBaseUrl(baseUrl));
  url.searchParams.set("limit", String(request.limit));
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
