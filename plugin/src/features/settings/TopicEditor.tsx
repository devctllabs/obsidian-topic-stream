import type { Source, SourceInput, Topic, TopicInput } from "../../api/contracts";
import type { ShellNavigation } from "../shell/navigation";
import type { SourceActions } from "./source-actions";
import type { SourceService } from "./services";
import { useLayoutEffect, useRef, useState, useSyncExternalStore, type SyntheticEvent } from "react";
import { useForm, useWatch } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { topicDraft, topicSchema } from "./topic-draft";
import { SettingsTextField } from "./SettingsTextField";
import { TopicAdvanced } from "./TopicAdvanced";
import { SourcePicker } from "./SourcePicker";
import { EditorExitDialog, useEditorExit } from "./editor-exit";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import { IconButton } from "../../shared/components/IconButton";
import { SourceEditor } from "./SourceEditor";
import { SourceSyncButton, SourceSyncMessage } from "./SourceSyncStatus";
import { cacheSource } from "./source-cache";

export interface TopicEditorProps {
  topic: Topic | null;
  client: Pick<SourceService, "listSources">;
  sourceActions: SourceActions;
  navigation: ShellNavigation;
  onSave: (input: TopicInput) => Promise<void>;
  onCancel: () => void;
}
export function TopicEditor({ topic, client, sourceActions, navigation, onSave, onCancel }: TopicEditorProps) {
  const queries = useQueryClient();
  const [initial] = useState(() => topicDraft(topic));
  const form = useForm<TopicInput>({ defaultValues: initial, resolver: zodResolver(topicSchema) });
  const [sourceIds, exclude, mode, itemKinds, enabled] = useWatch({ control: form.control, name: ["sourceIds", "exclude", "mode", "itemKinds", "enabled"] });
  const sources = useQuery({ queryKey: ["sources"], queryFn: () => client.listSources() });
  const [createdSources, setCreatedSources] = useState<Source[]>([]);
  const [view, setView] = useState<"form" | "picker" | "source">("form");
  const choose = useRef<HTMLButtonElement>(null);
  const previousView = useRef(view);
  useLayoutEffect(() => {
    if (view === "form" && previousView.current !== "form") choose.current?.focus();
    previousView.current = view;
  }, [view]);
  const save = useMutation({ mutationFn: (input: TopicInput) => onSave(input), onSuccess: (_value, input) => form.reset(input) });
  const saveDraft = async (): Promise<boolean> => {
    if (save.isPending) return false;
    let success = false;
    await form.handleSubmit(async (input) => {
      try { await save.mutateAsync(input); success = true; } catch { /* The mutation owns the visible error. */ }
    })();
    return success;
  };
  const exit = useEditorExit({ navigation, dirty: form.formState.isDirty, save: saveDraft, discard: () => form.reset(initial),
    isDirty: () => topicFields.some((field) => form.getFieldState(field).isDirty), beforePrompt: () => setView("form")
  });
  const submit = (event: SyntheticEvent<HTMLFormElement>) => { event.preventDefault(); void saveDraft(); };
  const createSource = async (input: SourceInput) => {
    const source = await sourceActions.create(input);
    await cacheSource(queries, source);
    setCreatedSources((current) => [...current, source]);
    const selected = form.getValues("sourceIds");
    if (selected.length && !selected.includes(source.id)) form.setValue("sourceIds", [...selected, source.id], { shouldDirty: true });
    setView("form");
  };
  return <>
    {view === "source" ? <SourceEditor source={null} navigation={navigation} onSave={createSource} onCancel={() => setView("form")} /> : view === "picker" ? <SourcePicker sources={sources.data ?? []} selected={sourceIds} loading={sources.isPending} error={sources.error} onRetry={() => { void sources.refetch(); }} onChange={(value) => form.setValue("sourceIds", value, { shouldDirty: true })} onBack={() => setView("form")} onAddSource={() => setView("source")} /> :
      <form className="topic-stream-settings-form" noValidate onSubmit={submit}>
        <div className="topic-stream-section-header"><h2>{topic ? "Edit topic" : "Add topic"}</h2><IconButton icon="back" label="Back" onClick={() => exit.request(onCancel)} /></div>
        {topic && <p className="topic-stream-metadata">Revision {topic.revision}</p>}
        <fieldset disabled={save.isPending}>
          <SettingsTextField label="Name" autoFocus maxLength={80} binding={form.register("name")} error={form.formState.errors.name} />
          <label>Include<textarea rows={4} maxLength={4000} {...form.register("include")} /></label>
          <div><span className="topic-stream-metadata">Sources</span><ScopeSummary ids={sourceIds} sources={sources.data ?? []} />
            <button ref={choose} type="button" className="topic-stream-refresh" onClick={() => setView("picker")}>Choose sources</button>
          </div>
          <TopicAdvanced form={form} selectedKinds={itemKinds} mode={mode} exclude={exclude} enabled={enabled} />
        </fieldset>
        <button type="submit" className="topic-stream-primary-action" disabled={save.isPending}>{topic ? "Save topic" : "Create topic"}</button>
        <CreatedSourceSyncs sources={createdSources} actions={sourceActions} navigation={navigation} />
        <TopicEditorFeedback saving={save.isPending} saveError={save.error} sourceError={sources.error} />
      </form>}
    <EditorExitDialog exit={exit} saving={save.isPending} resource="topic" />
  </>;
}
const topicFields = ["name", "include", "exclude", "sourceIds", "itemKinds", "mode", "enabled"] as const;

function TopicEditorFeedback({ saving, saveError, sourceError }: { saving: boolean; saveError: Error | null; sourceError: Error | null }) {
  return <>
    {saving && <p role="status" className="topic-stream-metadata">Saving…</p>}
    {saveError && <ApiErrorMessage title="Could not save topic" error={saveError} />}
    {sourceError && <ApiErrorMessage title="Could not load sources" error={sourceError} />}
  </>;
}

function ScopeSummary({ ids, sources }: { ids: string[]; sources: Source[] }) {
  if (!ids.length) return <p className="topic-stream-metadata">All sources</p>;
  return <div className="topic-stream-chips">{ids.slice(0, 3).map((id) => {
    const name = sources.find((source) => source.id === id)?.name ?? "Unavailable source";
    return <span key={id} title={name}>{name}</span>;
  })}
    {ids.length > 3 && <span>+{ids.length - 3}</span>}
  </div>;
}

function CreatedSourceSyncs({ sources, actions, navigation }: { sources: Source[]; actions: SourceActions; navigation: ShellNavigation }) {
  const states = useSyncExternalStore(actions.subscribe, actions.getSnapshot);
  return <>{sources.map((source) => <div key={source.id} className="topic-stream-section-header">
    <div><p className="topic-stream-metadata">{source.name}</p><SourceSyncMessage state={states[source.id]} sourceName={source.name} onOpenJob={(id) => navigation.openTask(id)} /></div>
    <SourceSyncButton source={source} state={states[source.id]} actions={actions} />
  </div>)}</>;
}
