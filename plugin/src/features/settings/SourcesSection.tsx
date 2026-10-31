import { EditorPane } from "./EditorPane";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from "react";
import type { JobAccepted, Source, SourceInput } from "../../api/contracts";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import { ActionMenu } from "../../shared/components/ActionMenu";
import type { ShellNavigation } from "../shell/navigation";
import { SourceEditor } from "./SourceEditor";
import { SourceRow } from "./SourceRow";
import { SourceDeleteDialog } from "./SourceDeleteDialog";
import { cacheSource } from "./source-cache";
import type { SourceEditingService, SourceService } from "./services";
import type { SourceActions, SourceSyncState, SourceWorkflowService } from "./source-actions";

export type SourcesWorkspaceService = SourceWorkflowService & SourceEditingService & Pick<SourceService, "listSources" | "syncAllSources">;

export function SourcesSection({ service, actions, navigation }: { service: SourcesWorkspaceService; actions: SourceActions; navigation: ShellNavigation }) {
  const queries = useQueryClient();
  const sources = useQuery({ queryKey: ["sources"], queryFn: () => service.listSources() });
  const syncs = useSyncExternalStore(actions.subscribe, actions.getSnapshot);
  useSourceMetadataReconciliation(syncs);
  const [editing, setEditing] = useState<Source | null | undefined>();
  const [deleting, setDeleting] = useState<Source | null>(null);
  const syncAll = useMutation({ mutationFn: () => service.syncAllSources() });
  const list = useRef<HTMLUListElement>(null);
  const add = useRef<HTMLButtonElement>(null);
  const restore = useRef<string | null>(null);
  useLayoutEffect(() => {
    if (editing !== undefined || deleting || !restore.current) return;
    const trigger = Array.from(list.current?.querySelectorAll<HTMLButtonElement>("[data-source-actions]") ?? []).find((button) => button.dataset.sourceActions === restore.current);
    (trigger ?? add.current)?.focus({ preventScroll: true });
    restore.current = null;
  }, [editing, deleting]);
  const remove = useMutation({
    mutationFn: (source: Source) => service.deleteSource(source.id),
    onSuccess: async (_value, source) => {
      await queries.cancelQueries({ queryKey: ["sources"] });
      queries.setQueryData<Source[]>(["sources"], (current = []) => current.filter((value) => value.id !== source.id));
      void queries.invalidateQueries({ queryKey: ["topics"] });
      restore.current = "add";
      setDeleting(null);
    }
  });
  const save = async (input: SourceInput) => {
    const source = editing ? await service.updateSource(editing.id, input) : await actions.create(input);
    await cacheSource(queries, source);
    restore.current = source.id;
    setEditing(undefined);
  };
  return <>
    <div className="topic-stream-resource-list" inert={editing !== undefined} aria-hidden={editing !== undefined}>
    <div className="topic-stream-resource-header"><h2>Sources</h2>
      <div className="topic-stream-form-actions"><button ref={add} type="button" className="topic-stream-primary-action" onClick={() => { restore.current = "add"; setEditing(null); }}>Add source</button>
        <ActionMenu label="More source actions" menuLabel="Source collection actions" items={[
          { id: "sync-all", label: "Sync all", disabled: syncAll.isPending, onSelect: () => syncAll.mutate() },
          { id: "refresh", label: "Refresh sources", disabled: sources.isFetching, onSelect: () => { void sources.refetch(); } }
        ]} />
      </div>
    </div>
    <SyncAllFeedback job={syncAll.data} error={syncAll.error} navigation={navigation} />
    {sources.isPending && <p role="status" className="topic-stream-metadata">Loading sources…</p>}
    {sources.isError && <ApiErrorMessage title="Could not load sources" error={sources.error} />}
    {sources.data?.length === 0 && <p className="topic-stream-metadata">Add a source to start collecting items.</p>}
    <ul className="topic-stream-source-rows" ref={list} aria-label="Sources">{sources.data?.map((source) => <SourceRow key={source.id} source={source} state={syncs[source.id]} actions={actions} service={service} onOpenJob={(id) => navigation.openTask(id)}
      onEdit={(value) => { restore.current = value.id; setEditing(value); }}
      onDelete={(value) => { restore.current = value.id; remove.reset(); setDeleting(value); }} />)}</ul>
    </div>
    {editing !== undefined && <EditorPane label="Source editor"><SourceEditor key={editing?.id ?? "new"} source={editing} navigation={navigation} onSave={save} onCancel={() => setEditing(undefined)} /></EditorPane>}
    {deleting && <SourceDeleteDialog source={deleting} pending={remove.isPending} error={remove.error} onConfirm={() => remove.mutate(deleting)} onCancel={() => setDeleting(null)} />}
  </>;
}

function SyncAllFeedback({ job, error, navigation }: { job: JobAccepted | undefined; error: Error | null; navigation: ShellNavigation }) {
  if (job) return <div className="topic-stream-form-actions"><p role="status" className="topic-stream-metadata">Sync queued</p><button type="button" className="topic-stream-text-button" onClick={() => navigation.openTask(job.jobId)}>View task</button></div>;
  if (error) return <ApiErrorMessage title="Could not start Sync all" error={error} />;
  return null;
}

function useSourceMetadataReconciliation(syncs: Record<string, SourceSyncState>): void {
  const queries = useQueryClient();
  const reconciled = useRef(new Set<string>());
  useEffect(() => {
    const terminal = Object.values(syncs).filter((state) => state.jobId && ["succeeded", "dead", "cancelled"].includes(state.status));
    let changed = false;
    for (const state of terminal) {
      if (state.jobId && !reconciled.current.has(state.jobId)) { reconciled.current.add(state.jobId); changed = true; }
    }
    if (changed) void queries.invalidateQueries({ queryKey: ["sources"] });
  }, [queries, syncs]);
}
