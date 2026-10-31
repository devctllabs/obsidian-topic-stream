export interface ClientPreferences {
  version: 1;
  feedFilter: "all" | "unread";
  snapshot: { content: "compact" | "full"; images: "links" | "remote" | "local" };
}

export function normalizePreferences(input: unknown): ClientPreferences {
  const value = record(input);
  const snapshot = record(value.snapshot);
  const supported = value.version === undefined || value.version === 1;
  return {
    version: 1,
    feedFilter: supported && value.feedFilter === "unread" ? "unread" : "all",
    snapshot: {
      content: supported && snapshot.content === "full" ? "full" : "compact",
      images: supported && (snapshot.images === "remote" || snapshot.images === "local") ? snapshot.images : "links"
    }
  };
}

export function record(value: unknown): Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value) ? value as Record<string, unknown> : {};
}
