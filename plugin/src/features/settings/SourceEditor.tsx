import type { Source, SourceInput } from "../../api/contracts";
import { useState, type SyntheticEvent } from "react";
import { useForm, useWatch } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation } from "@tanstack/react-query";
import type { ShellNavigation } from "../shell/navigation";
import { SourceAdapterFields, SourceAdvanced } from "./SourceFields";
import { SettingsTextField } from "./SettingsTextField";
import { defaultSourceConfig, sourceDraft, sourceSchema } from "./source-draft";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import { IconButton } from "../../shared/components/IconButton";
import { EditorExitDialog, useEditorExit } from "./editor-exit";

export interface SourceEditorProps {
  source: Source | null;
  navigation: ShellNavigation;
  onSave: (input: SourceInput) => Promise<void>;
  onCancel: () => void;
}
export function SourceEditor({ source, navigation, onSave, onCancel }: SourceEditorProps) {
  const [initial] = useState(() => sourceDraft(source));
  const form = useForm<SourceInput>({ defaultValues: initial, resolver: zodResolver(sourceSchema) });
  const config = useWatch({ control: form.control, name: "config" });
  const save = useMutation({ mutationFn: (input: SourceInput) => onSave(input), onSuccess: (_value, input) => form.reset(input) });
  const saveDraft = async (): Promise<boolean> => {
    if (save.isPending) return false;
    let success = false;
    await form.handleSubmit(async (input) => {
      try { await save.mutateAsync(input); success = true; } catch { /* The mutation owns the visible error. */ }
    })();
    return success;
  };
  const exit = useEditorExit({ navigation, dirty: form.formState.isDirty, save: saveDraft, discard: () => form.reset(initial),
    isDirty: () => sourceFields.some((field) => form.getFieldState(field).isDirty)
  });
  const submit = (event: SyntheticEvent<HTMLFormElement>) => { event.preventDefault(); void saveDraft(); };
  return <><form className="topic-stream-settings-form" noValidate onSubmit={submit}>
    <div className="topic-stream-section-header"><h2>{source ? "Edit source" : "Add source"}</h2><IconButton icon="back" label="Back" onClick={() => exit.request(onCancel)} /></div>
    <fieldset disabled={save.isPending}>
      <SettingsTextField label="Name" autoFocus maxLength={120} binding={form.register("name")} error={form.formState.errors.name} />
      <label>Adapter<select value={config.kind} onChange={(event) => {
        const kind = sourceKinds.find(([key]) => key === event.target.value)?.[0];
        if (kind) { form.setValue("config", defaultSourceConfig(kind), { shouldDirty: true }); form.clearErrors("config"); }
      }}>{sourceKinds.map(([kind, label]) => <option key={kind} value={kind}>{label}</option>)}</select></label>
      <SourceAdapterFields config={config} form={form} />
      <SourceAdvanced config={config} form={form} />
    </fieldset>
    <div className="topic-stream-form-actions"><button type="submit" className="topic-stream-primary-action" disabled={save.isPending}>{source ? "Save source" : "Create source"}</button>
      <span role="status" className="topic-stream-metadata">{save.isPending ? "Saving…" : ""}</span>
    </div>
    {save.isError && <ApiErrorMessage title="Could not save source" error={save.error} />}
  </form><EditorExitDialog exit={exit} saving={save.isPending} resource="source" /></>;
}
const sourceFields = ["name", "config", "enabled", "pollIntervalMinutes"] as const;
const sourceKinds = [["rss", "RSS"], ["atom", "Atom"], ["arxiv", "arXiv"], ["youtube", "YouTube"], ["github", "GitHub Trending"]] as const;
