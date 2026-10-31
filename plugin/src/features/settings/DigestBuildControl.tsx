import { useEffect, useSyncExternalStore } from "react";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import type { DigestBuilds, DigestBuildState } from "./digest-builds";

export function DigestBuildControl({ id, name, builds }: { id: string; name: string; builds: DigestBuilds }) {
  useEffect(() => { void builds.restore(id); }, [builds, id]);
  const states = useSyncExternalStore(builds.subscribe, builds.getSnapshot);
  const state = states[id];
  if (!state) return null;
  return <div><BuildStatus state={state} /><BuildRequestButton id={id} name={name} builds={builds} state={state} /></div>;
}
function BuildStatus({ state }: { state: DigestBuildState }) {
  if (state.status === "uncertain") return <ApiErrorMessage title="Build response unavailable. Retry this request to check the same Build." error={state.error} />;
  if (state.status === "unavailable") return <ApiErrorMessage title={state.jobId ? "Could not read build status" : "Could not read saved build request"} error={state.error} />;
  return <p role="status" className="topic-stream-metadata">{labels[state.status]}</p>;
}
const labels = { requesting: "Requesting build…", queued: "Build queued", running: "Building…", retry_wait: "Retry scheduled", succeeded: "Build complete", dead: "Build needs attention", cancelled: "Build cancelled" };

function BuildRequestButton({ id, name, builds, state }: { id: string; name: string; builds: DigestBuilds; state: DigestBuildState | undefined }) {
  if (state?.status === "unavailable" && !state.jobId) return <button type="button" className="topic-stream-refresh" aria-label={`Check saved build request for ${name}`} onClick={() => { void builds.restore(id); }}>Check saved request</button>;
  if (state?.status === "uncertain") return <button type="button" className="topic-stream-refresh" aria-label={`Retry build request for ${name}`} onClick={() => { void builds.build(id); }}>Retry request</button>;
  return null;
}
