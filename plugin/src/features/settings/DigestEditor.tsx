import type { Schema } from "../../api/contracts";
import type { ShellNavigation } from "../shell/navigation";
import { useState, type SyntheticEvent } from "react";
import { useForm, useWatch } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation } from "@tanstack/react-query";
import { ApiError } from "../../api/errors";
import { DigestConflictReview } from "./DigestConflictReview";
import { digestDraft, digestSchema } from "./digest-draft";
import { SettingsTextField } from "./SettingsTextField";
import { DigestCalendar } from "./DigestCalendar";
import { DigestTopics } from "./DigestTopics";
import { EditorExitDialog, useEditorExit } from "./editor-exit";
import { IconButton } from "../../shared/components/IconButton";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";

export interface DigestEditorProps {
  digest: Schema<"DigestDefinition"> | null;
  topics: Schema<"Topic">[];
  navigation: ShellNavigation;
  onSave: (input: Schema<"DigestInput">, version: number | null) => Promise<void>;
  onCancel: () => void;
  loadLatest: () => Promise<Schema<"DigestDefinition">>;
}
export function DigestEditor({ digest, topics, navigation, onSave, onCancel, loadLatest }: DigestEditorProps) {
  const [initial, setInitial] = useState(() => digestDraft(digest));
  const [version, setVersion] = useState(digest?.version ?? null);
  const form = useForm<Schema<"DigestInput">>({ defaultValues: initial, resolver: zodResolver(digestSchema) });
  const [enabled, recurrence, selected] = useWatch({ control: form.control, name: ["enabled", "recurrence", "topics"] });
  const save = useMutation({ mutationFn: (input: Schema<"DigestInput">) => onSave(input, version), onSuccess: (_value, input) => form.reset(input) });
  const saveDraft = async (): Promise<boolean> => {
    if (save.isPending) return false;
    let success = false;
    await form.handleSubmit(async (input) => { try { await save.mutateAsync(input); success = true; } catch { /* Mutation retains the error and draft. */ } })();
    return success;
  };
  const exit = useEditorExit({ navigation, dirty: form.formState.isDirty, save: saveDraft, discard: () => form.reset(initial) });
  const acceptLatest = (value: Schema<"DigestDefinition">, keepDraft: boolean) => {
    const draft = form.getValues();
    const saved = digestDraft(value);
    setInitial(saved); setVersion(value.version); form.reset(saved);
    if (keepDraft) for (const field of digestFields) form.setValue(field, draft[field], { shouldDirty: true });
    save.reset();
  };
  const submit = (event: SyntheticEvent<HTMLFormElement>) => { event.preventDefault(); void saveDraft(); };
  return <><form noValidate className="topic-stream-settings-form" onSubmit={submit}>
    <div className="topic-stream-section-header"><h2>{digest ? "Edit digest definition" : "Add digest definition"}</h2><IconButton icon="back" label="Back" onClick={() => exit.request(onCancel)} /></div>
    <fieldset disabled={save.isPending}>
      <SettingsTextField label="Name" autoFocus maxLength={120} binding={form.register("name")} error={form.formState.errors.name} />
      <DigestTopics topics={topics} value={selected} onChange={(value) => form.setValue("topics", value, { shouldDirty: true })} />
      <label>Delivery<select value={enabled ? "scheduled" : "manual"} onChange={(event) => form.setValue("enabled", event.target.value === "scheduled", { shouldDirty: true })}><option value="manual">Manual only</option><option value="scheduled">Scheduled</option></select></label>
      {enabled && <><DigestCalendar value={recurrence} onChange={(value) => form.setValue("recurrence", value, { shouldDirty: true })} /><SettingsTextField label="IANA timezone" binding={form.register("timezone")} error={form.formState.errors.timezone} /></>}
    </fieldset>
    <p className="topic-stream-metadata">Build is always available. Saving does not build a Digest.</p>
    <button type="submit" className="topic-stream-primary-action" disabled={save.isPending}>{digest ? "Save definition" : "Create definition"}</button>
    {save.error instanceof ApiError && save.error.status === 412 && <DigestConflictReview load={loadLatest} onAccept={acceptLatest} />}
    <DigestValidation invalid={Object.keys(form.formState.errors).length > 0} saving={save.isPending} error={save.error} />
  </form><EditorExitDialog exit={exit} saving={save.isPending} resource="digest definition" /></>;
}
function DigestValidation({ invalid, saving, error }: { invalid: boolean; saving: boolean; error: Error | null }) {
  return <>{invalid && <p role="alert">Check the name, selected Topics and calendar values. Each Topic needs an Item limit from 1 to 15.</p>}{saving && <p role="status">Saving…</p>}{error && <ApiErrorMessage title="Could not save digest definition. Your draft is kept." error={error} />}</>;
}

const digestFields = ["name", "topics", "timezone", "recurrence", "enabled"] as const;
