import type { FeedFilter, FeedPreferences } from "../../features/feed/preferences";

export function memoryFeedPreferences(initial: FeedFilter = "all"): FeedPreferences {
  let current = initial;
  const listeners = new Set<() => void>();
  return {
    getSnapshot: () => current,
    subscribe: (listener) => { listeners.add(listener); return () => { listeners.delete(listener); }; },
    save: (filter) => { current = filter; for (const listener of listeners) listener(); return Promise.resolve(); }
  };
}
