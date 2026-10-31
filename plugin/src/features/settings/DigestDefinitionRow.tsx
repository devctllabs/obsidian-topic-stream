import { useSyncExternalStore } from "react";
import type { Schema } from "../../api/contracts";
import type { ShellNavigation } from "../shell/navigation";
import { ActionMenu, type ActionMenuItem } from "../../shared/components/ActionMenu";
import type { DigestBuilds, DigestBuildState } from "./digest-builds";
import { DigestBuildControl } from "./DigestBuildControl";

type Definition = Schema<"DigestDefinition">;
interface RowProps {
  digest: Definition; builds: DigestBuilds; navigation: ShellNavigation;
  onEdit: (value: Definition) => void; onDelete: (value: Definition) => void;
}
export function DigestDefinitionRow(props: RowProps) {
  const { digest, builds, onEdit } = props;
  const states = useSyncExternalStore(builds.subscribe, builds.getSnapshot);
  return <li><div className="topic-stream-source-row-content">
    <h3><button type="button" className="topic-stream-row-title-button" onClick={() => onEdit(digest)}>{digest.name}</button></h3>
    <p className="topic-stream-metadata">{digest.enabled ? `Scheduled · ${digest.timezone}` : "Manual only"} · {digest.topics.length} Topics</p>
    {digest.nextRunAt && <p className="topic-stream-metadata">Next run {new Date(digest.nextRunAt).toLocaleString("en")}</p>}
    <DigestBuildControl id={digest.id} name={digest.name} builds={builds} />
  </div><ActionMenu label={`More actions for ${digest.name}`} menuLabel={`Actions for ${digest.name}`} triggerProps={{ "data-digest-edit": digest.id }} items={definitionActions({ ...props, state: states[digest.id] })} /></li>;
}
function definitionActions({ digest, builds, navigation, onEdit, onDelete, state }: RowProps & { state: DigestBuildState | undefined }): ActionMenuItem[] {
  const active = Boolean(state && ["requesting", "queued", "running", "retry_wait", "unavailable"].includes(state.status));
  const jobId = state?.jobId;
  const items: ActionMenuItem[] = [
    { id: "edit", label: "Edit", onSelect: () => onEdit(digest) },
    { id: "build", label: "Build", disabled: active, onSelect: () => { void builds.build(digest.id); } }
  ];
  if (jobId) items.push(
    { id: "refresh", label: "Refresh build", onSelect: () => { void builds.refresh(digest.id); } },
    { id: "task", label: "View task", onSelect: () => navigation.openTask(jobId) }
  );
  items.push({ id: "delete", label: "Delete", destructive: true, separator: true, onSelect: () => onDelete(digest) });
  return items;
}
