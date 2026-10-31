import type { QueryClient } from "@tanstack/react-query";
import type { ViewEvents } from "../../shared/events";
import type { Schema } from "../../api/contracts";
import type { DigestBuilds } from "./digest-builds";

export function connectDigestBuilds(builds: DigestBuilds, queries: QueryClient, events?: ViewEvents): () => void {
  const unsubscribeEvents = events?.subscribe((event) => {
    for (const [id, state] of Object.entries(builds.getSnapshot())) {
      if (event.type === "reconnected" || (event.type === "jobChanged" && event.jobId === state.jobId)) void builds.refresh(id);
    }
  });
  const unsubscribeCache = queries.getQueryCache().subscribe((event) => {
    if (event.type === "updated" && event.action.type === "success" && event.query.queryHash.startsWith('["job",')) void builds.observe(event.query.state.data as Schema<"Job">);
  });
  return () => { unsubscribeEvents?.(); unsubscribeCache(); };
}
