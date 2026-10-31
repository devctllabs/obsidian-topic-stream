import { useSyncExternalStore } from "react";
import { useQuery } from "@tanstack/react-query";
import type { Schema } from "../../api/contracts";
import { ActionMenu } from "../../shared/components/ActionMenu";
import { IconButton } from "../../shared/components/IconButton";
import { DigestBuildControl } from "../settings/DigestBuildControl";
import type { DigestBuilds, DigestBuildState } from "../settings/digest-builds";
import type { ShellNavigation } from "../shell/navigation";
import type { DigestsService } from "./services";
import { findSavedIssue } from "./saved-result";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";

export function BuildMenu({ definitions, builds }: { definitions: Schema<"DigestDefinition">[]; builds: DigestBuilds }) {
  const states = useSyncExternalStore(builds.subscribe, builds.getSnapshot);
  return <ActionMenu label="Build a Digest Issue" menuLabel="Choose a saved Digest definition" text="Build" disabled={definitions.length === 0} items={definitions.map((definition) => ({
    id: definition.id, label: `Build ${definition.name}${definition.enabled ? "" : " · Manual only"}`,
    disabled: buildBusy(states[definition.id]), onSelect: () => { void builds.build(definition.id); }
  }))} />;
}
function buildBusy(state: DigestBuildState | undefined): boolean {
  return Boolean(state && !["succeeded", "dead", "cancelled"].includes(state.status));
}
export function ArchiveBuilds({ definitions, builds, navigation, client, onOpen }: { definitions: Schema<"DigestDefinition">[]; builds: DigestBuilds; navigation: ShellNavigation; client: DigestsService; onOpen: (id: string) => void }) {
  const states = useSyncExternalStore(builds.subscribe, builds.getSnapshot);
  return <div>{definitions.map((definition) => <div key={definition.id} className="topic-stream-archive-build">
    {states[definition.id] && <p className="topic-stream-metadata">{definition.name}</p>}
    <DigestBuildControl id={definition.id} name={definition.name} builds={builds} />
    {states[definition.id]?.jobId && <div className="topic-stream-row-tools"><IconButton icon="refresh" label={`Check build status for ${definition.name}`} onClick={() => { void builds.refresh(definition.id); }} /><IconButton icon="tasks" label={`View build task for ${definition.name}`} onClick={() => navigation.openTask(states[definition.id]!.jobId!)} /></div>}
    {states[definition.id]?.status === "succeeded" && <BuildResult client={client} definition={definition} jobId={states[definition.id]!.jobId!} onOpen={onOpen} />}
  </div>)}</div>;
}
function BuildResult({ client, definition, jobId, onOpen }: { client: DigestsService; definition: Schema<"DigestDefinition">; jobId: string; onOpen: (id: string) => void }) {
  const result = useQuery({ queryKey: ["digest-result", jobId], queryFn: ({ signal }) => findSavedIssue(client, jobId, signal, definition.id), retry: false, staleTime: Infinity, refetchOnWindowFocus: false, refetchOnReconnect: false });
  if (result.data) return <button type="button" className="topic-stream-refresh" aria-label={`Open built issue for ${definition.name}`} onClick={() => onOpen(result.data!.id)}>Open built issue</button>;
  return <>{result.error && <ApiErrorMessage title="Could not find the saved issue" error={result.error} />}{!result.isPending && <button type="button" className="topic-stream-refresh" onClick={() => { void result.refetch(); }}>Check saved issue</button>}</>;
}
