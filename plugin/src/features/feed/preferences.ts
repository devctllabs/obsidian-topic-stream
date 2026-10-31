export type FeedFilter = "all" | "unread";
export interface FeedPreferences {
  getSnapshot: () => FeedFilter;
  subscribe: (listener: () => void) => () => void;
  save: (filter: FeedFilter) => Promise<void>;
}
