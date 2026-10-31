import type { TransitionNotifications } from "./transition-notifications";
import type { JobActions } from "../../shared/jobs/job-actions";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, useRef, type SyntheticEvent } from "react";
import { useForm, useWatch } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import type { Schema } from "../../api/contracts";
import type { ShellNavigation } from "../shell/navigation";
import type { WorkspaceSettingsService } from "./services";
import { workspaceDraft, workspaceSchema, type WorkspaceDraft } from "./workspace-draft";
import { EmbeddingModelControl } from "./EmbeddingModelControl";
import { WorkspaceGenerationFields } from "./WorkspaceGenerationFields";
import { ApiError } from "../../api/errors";
import { WorkspaceConflictReview } from "./WorkspaceConflictReview";
import { WorkspaceAdvanced } from "./WorkspaceAdvanced";
import { EditorExitDialog, useEditorExit } from "./editor-exit";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";

export function WorkspaceSettingsEditor({ service, navigation, jobs, transitions }: { service: WorkspaceSettingsService; navigation: ShellNavigation; jobs: JobActions; transitions: TransitionNotifications }) {
  const query = useQuery({ queryKey: ["workspace-settings"], queryFn: () => service.getWorkspaceSettings() });
  if (query.data) return <WorkspaceForm initial={query.data} service={service} navigation={navigation} jobs={jobs} transitions={transitions} />;
  if (query.isError) return <div><ApiErrorMessage title="Could not load workspace settings" error={query.error} /><button type="button" onClick={() => { void query.refetch(); }}>Try again</button></div>;
  return <p role="status">Loading workspace settings…</p>;
}
function WorkspaceForm({ initial, service, navigation, jobs, transitions }: { initial: Schema<"WorkspaceSettings">; service: WorkspaceSettingsService; navigation: ShellNavigation; jobs: JobActions; transitions: TransitionNotifications }) {
  const queries = useQueryClient();
  const [trackingError, setTrackingError] = useState<Error | null>(null);
  const selectionBoundary = useRef(0);
  const [base, setBase] = useState(initial);
  const form = useForm<WorkspaceDraft>({ defaultValues: workspaceDraft(initial), resolver: zodResolver(workspaceSchema) });
  const [generationEnabled, embeddingModelId] = useWatch({ control: form.control, name: ["generation.enabled", "embeddingModelId"] });
  const save = useMutation({ onMutate: () => { selectionBoundary.current = queries.getQueryData<Schema<"RetrievalStatus">>(["retrieval-status"])?.latestTransition?.selectionGeneration ?? 0; }, mutationFn: (input: WorkspaceDraft) => service.updateWorkspaceSettings(input, base.version), onSuccess: async (value) => {
    const changedModel = value.embeddingModelId !== base.embeddingModelId;
    setBase(value); form.reset(workspaceDraft(value)); queries.setQueryData(["workspace-settings"], value);
    if (changedModel) {
      try { await transitions.watch(value.embeddingModelId, selectionBoundary.current); setTrackingError(null); } catch { setTrackingError(new Error("Settings are saved. Refresh model status to check preparation.")); }
    }
    void queries.invalidateQueries({ queryKey: ["retrieval-status"] });
  } });
  const saveDraft = async (): Promise<boolean> => {
    if (save.isPending) return false;
    let success = false;
    await form.handleSubmit(async (input) => { try { await save.mutateAsync(input); success = true; } catch { /* Mutation retains the draft and visible error. */ } })();
    return success;
  };
  const exit = useEditorExit({ navigation, dirty: form.formState.isDirty, save: saveDraft, discard: () => form.reset(workspaceDraft(base)) });
  const acceptLatest = (value: Schema<"WorkspaceSettings">, keepDraft: boolean) => {
    const draft = form.getValues();
    setBase(value); form.reset(workspaceDraft(value));
    if (keepDraft) for (const field of workspaceFields) form.setValue(field, draft[field], { shouldDirty: true });
    queries.setQueryData(["workspace-settings"], value); save.reset();
  };
  const submit = (event: SyntheticEvent<HTMLFormElement>) => { event.preventDefault(); void saveDraft(); };
  return <><form className="topic-stream-settings-form" noValidate onSubmit={submit}>
    <h2>Workspace settings</h2>
      <EmbeddingModelControl transitions={transitions} jobs={jobs} selectionDisabled={save.isPending} service={service} navigation={navigation} value={embeddingModelId} onChange={(value) => form.setValue("embeddingModelId", value, { shouldDirty: true })} />
    <fieldset disabled={save.isPending}>
      <WorkspaceGenerationFields form={form} enabled={generationEnabled} />
      <WorkspaceAdvanced form={form} />
    </fieldset>
    <button type="submit" className="topic-stream-primary-action" disabled={save.isPending || !form.formState.isDirty}>Save workspace settings</button>
    <p role="status" className="topic-stream-metadata">{save.isPending ? "Saving…" : save.isSuccess && !form.formState.isDirty ? "Saved" : ""}</p>
    {Object.keys(form.formState.errors).length > 0 && <p role="alert">Check the marked fields and Advanced values before saving.</p>}
    {trackingError && <ApiErrorMessage title="Could not track model preparation" error={trackingError} />}
    {save.error instanceof ApiError && save.error.status === 412 && <WorkspaceConflictReview load={() => service.getWorkspaceSettings()} onAccept={acceptLatest} />}
    {save.isError && <ApiErrorMessage title="Could not save workspace settings. Your draft is kept." error={save.error} />}
  </form><EditorExitDialog exit={exit} saving={save.isPending} resource="workspace settings" /></>;
}

const workspaceFields = ["embeddingModelId", "generation", "summary", "instructions", "retention", "retrieval"] as const;
