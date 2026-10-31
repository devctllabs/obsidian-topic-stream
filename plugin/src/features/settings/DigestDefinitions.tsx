import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useLayoutEffect, useRef, useState } from "react";
import type { Schema } from "../../api/contracts";
import type { ShellNavigation } from "../shell/navigation";
import type { DigestDefinitionService } from "./services";
import type { DigestBuilds } from "./digest-builds";
import { DigestEditor } from "./DigestEditor";
import { DigestDefinitionRow } from "./DigestDefinitionRow";
import { EditorPane } from "./EditorPane";
import { DigestDeleteDialog } from "./DigestDeleteDialog";
import { ActionMenu } from "../../shared/components/ActionMenu";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";

type Definition = Schema<"DigestDefinition">;
export function DigestDefinitions({ service, navigation, builds }: { service: DigestDefinitionService; navigation: ShellNavigation; builds: DigestBuilds }) {
  const queries = useQueryClient();
  const definitions = useQuery({ queryKey: ["digests"], queryFn: () => service.listDigests() });
  const topics = useQuery({ queryKey: ["topics"], queryFn: () => service.listTopics() });
  const [editing, setEditing] = useState<Definition | null | undefined>();
  const [deleting, setDeleting] = useState<Definition | null>(null);
  const section = useRef<HTMLDivElement>(null);
  const restore = useRef<string | null>(null);
  useLayoutEffect(() => {
    if (editing !== undefined || deleting || !restore.current) return;
    const buttons = Array.from(section.current?.querySelectorAll<HTMLButtonElement>("[data-digest-edit]") ?? []);
    const target = buttons.find((button) => button.dataset.digestEdit === restore.current) ?? section.current?.querySelector<HTMLButtonElement>("[data-digest-add]");
    target?.focus({ preventScroll: true }); restore.current = null;
  }, [editing, deleting]);
  const remove = useMutation({ mutationFn: (value: Definition) => service.deleteDigest(value.id, value.version), onSuccess: async (_value, digest) => {
    await queries.cancelQueries({ queryKey: ["digests"] });
    queries.setQueryData<Definition[]>(["digests"], (current = []) => current.filter((value) => value.id !== digest.id));
    restore.current = "add"; setDeleting(null);
  } });
  const save = async (input: Schema<"DigestInput">, version: number | null) => {
    const value = editing && version !== null ? await service.updateDigest(editing.id, input, version) : await service.createDigest(input);
    await queries.cancelQueries({ queryKey: ["digests"] });
    queries.setQueryData<Definition[]>(["digests"], (current = []) => current.some((entry) => entry.id === value.id) ? current.map((entry) => entry.id === value.id ? value : entry) : [...current, value]);
    void queries.invalidateQueries({ queryKey: ["digests"] });
    restore.current = value.id; setEditing(undefined);
  };
  const loadLatest = async () => {
    const values = await service.listDigests();
    const latest = values.find((value) => value.id === editing?.id);
    if (!latest) throw new Error("This Definition is no longer available.");
    return latest;
  };
  return <div ref={section}>
    <div className="topic-stream-resource-list" inert={editing !== undefined} aria-hidden={editing !== undefined}>
    <div className="topic-stream-resource-header"><h2>Digest definitions</h2><div className="topic-stream-form-actions">
      <button type="button" className="topic-stream-primary-action" data-digest-add onClick={() => { restore.current = "add"; setEditing(null); }}>Add definition</button>
      <ActionMenu label="More definition actions" menuLabel="Digest collection actions" items={[{ id: "refresh", label: "Refresh definitions", disabled: definitions.isFetching, onSelect: () => { void definitions.refetch(); } }]} />
    </div></div>
    <DefinitionFeedback pending={definitions.isPending} error={definitions.error} topicError={topics.error} empty={definitions.data?.length === 0} />
    <ul className="topic-stream-source-rows" aria-label="Digest definitions">{definitions.data?.map((digest) => <DigestDefinitionRow key={digest.id} digest={digest} builds={builds} navigation={navigation}
      onEdit={(value) => { restore.current = value.id; setEditing(value); }}
      onDelete={(value) => { restore.current = value.id; remove.reset(); setDeleting(value); }} />)}</ul>
    </div>
    {editing !== undefined && <EditorPane label="Digest definition editor"><DigestEditor key={editing?.id ?? "new"} digest={editing} loadLatest={loadLatest} topics={topics.data ?? []} navigation={navigation} onSave={save} onCancel={() => setEditing(undefined)} /></EditorPane>}
    {deleting && <DigestDeleteDialog digest={deleting} pending={remove.isPending} error={remove.error} onConfirm={() => remove.mutate(deleting)} onCancel={() => setDeleting(null)} />}
  </div>;
}

function DefinitionFeedback({ pending, error, topicError, empty }: { pending: boolean; error: Error | null; topicError: Error | null; empty: boolean }) {
  return <>{pending && <p role="status">Loading definitions…</p>}{error && <ApiErrorMessage title="Could not load digest definitions" error={error} />}{topicError && <ApiErrorMessage title="Could not load topics" error={topicError} />}{empty && <p className="topic-stream-metadata">Create a Definition to collect Topics into a Digest.</p>}</>;
}
