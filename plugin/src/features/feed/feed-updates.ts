import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import type { ViewEvents } from "../../shared/events";
import type { FeedService } from "./services";
import { reconcileFeedTraversals } from "./reconcile-feed";

/** A Feed consumer of the host event port; this never creates a connection or publishes. */
export function useFeedUpdates(client: FeedService, events: ViewEvents | undefined, retainUnread: () => void) {
  const queries = useQueryClient();
  const [available, setAvailable] = useState(new Set<string | null>());
  const [error, setError] = useState<Error | null>(null);
  useEffect(() => {
    let disposed = false;
    let running = false;
    let queued = false;
    const reconcile = async () => {
      if (running) { queued = true; return; }
      running = true;
      do {
        queued = false;
        retainUnread();
        try { await reconcileFeedTraversals(queries, client); if (!disposed) setError(null); }
        catch (failure) { if (!disposed) setError(failure instanceof Error ? failure : new Error("Could not update saved Feed.")); }
      } while (queued && !disposed);
      running = false;
    };
    const unsubscribe = events?.subscribe((event) => {
      if (event.type !== "feedInvalidated" && event.type !== "reconnected") return;
      const scopes = queries.getQueriesData({ queryKey: ["feed"] }).map(([key]) => typeof key[1] === "string" ? key[1] : null);
      setAvailable((current) => new Set([...current, ...scopes]));
      void reconcile();
    });
    return () => { disposed = true; unsubscribe?.(); };
  }, [client, events, queries, retainUnread]);
  const clear = (scope: string | null) => { setAvailable((current) => { const next = new Set(current); next.delete(scope); return next; }); setError(null); };
  return { available, error, clear };
}
