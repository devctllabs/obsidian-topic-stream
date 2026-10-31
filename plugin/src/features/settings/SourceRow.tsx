import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { Source } from "../../api/contracts";
import { ActionMenu } from "../../shared/components/ActionMenu";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import type { SourceEditingService } from "./services";
import type { SourceActions, SourceSyncState } from "./source-actions";
import { SourceSyncButton, SourceSyncMessage, sourceSyncAction } from "./SourceSyncStatus";
import { cacheSource } from "./source-cache";

export function SourceRow({ source, state, actions, service, onEdit, onDelete, onOpenJob }: {
  source: Source; state: SourceSyncState | undefined; actions: SourceActions; service: SourceEditingService;
  onEdit: (source: Source) => void; onDelete: (source: Source) => void;
  onOpenJob: (id: string) => void;
}) {
  const queries = useQueryClient();
  const toggle = useMutation({ mutationFn: () => service.updateSource(source.id, { enabled: !source.enabled }), onSuccess: (value) => cacheSource(queries, value) });
  const sync = sourceSyncAction(state);
  return <li>
    <div className="topic-stream-source-row-content"><h3><button type="button" className="topic-stream-row-title-button" disabled={toggle.isPending} onClick={() => onEdit(source)}>{source.name}</button></h3>
      <p className="topic-stream-metadata">{adapterNames[source.config.kind]} · {source.lastSyncedAt ? `Last synced ${new Intl.DateTimeFormat("en", { dateStyle: "medium" }).format(new Date(source.lastSyncedAt))}` : "Never synced"}</p>
      {!source.enabled && <p className="topic-stream-metadata">Paused</p>}
      <SourceSyncMessage compact state={state} sourceName={source.name} onOpenJob={onOpenJob} />
      {toggle.isPending && <p role="status" className="topic-stream-metadata">Updating source…</p>}
      {toggle.isError && <ApiErrorMessage title="Could not update source" error={toggle.error} />}
    </div>
    <div className="topic-stream-row-tools">{sync.retry && <SourceSyncButton source={source} state={state} actions={actions} />}
      <ActionMenu label={`More actions for ${source.name}`} menuLabel={`Actions for ${source.name}`} disabled={toggle.isPending} triggerProps={{ "data-source-actions": source.id }} items={[
        { id: "edit", label: "Edit", onSelect: () => onEdit(source) },
        { id: "sync", label: sync.label, disabled: sync.disabled, onSelect: () => { void (sync.check ? actions.refresh(source.id) : actions.sync(source.id)); } },
        ...(state?.jobId ? [{ id: "task", label: "View task", onSelect: () => onOpenJob(state.jobId!) }] : []),
        { id: "toggle", label: source.enabled ? "Pause" : "Enable", onSelect: () => toggle.mutate() },
        { id: "delete", label: "Delete", destructive: true, separator: true, onSelect: () => onDelete(source) }
      ]} />
    </div>
  </li>;
}

const adapterNames = { rss: "RSS", atom: "Atom", arxiv: "arXiv", youtube: "YouTube", github: "GitHub Trending" };
