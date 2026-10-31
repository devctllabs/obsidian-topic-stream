import type { QueryClient } from "@tanstack/react-query";
import type { ViewEvents } from "../events";

/** Settings and Task Center observe the same authoritative Job queries. No timers or replay. */
export function connectJobResources(queries: QueryClient, events?: ViewEvents): () => void {
  return events?.subscribe((event) => {
    if (event.type === "jobChanged") {
      void queries.invalidateQueries({ queryKey: ["job", event.jobId] }, { cancelRefetch: false });
      void queries.invalidateQueries({ queryKey: ["jobs"] }, { cancelRefetch: false });
      void queries.invalidateQueries({ queryKey: ["retrieval-status"] }, { cancelRefetch: false });
    }
    if (event.type === "reconnected") {
      for (const scope of ["job", "jobs", "retrieval-status"]) void queries.invalidateQueries({ queryKey: [scope] }, { cancelRefetch: false });
    }
  }) ?? (() => undefined);
}
