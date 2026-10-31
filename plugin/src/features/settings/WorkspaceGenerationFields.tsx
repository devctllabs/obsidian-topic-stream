import type { UseFormReturn } from "react-hook-form";
import type { WorkspaceDraft } from "./workspace-draft";
import { SettingsTextField } from "./SettingsTextField";
export function WorkspaceGenerationFields({ form, enabled }: { form: UseFormReturn<WorkspaceDraft>; enabled: boolean }) {
  return <section><h3>AI summaries</h3>
    <label className="topic-stream-checkbox"><input type="checkbox" {...form.register("generation.enabled")} />Enable AI generation</label>
    <p className="topic-stream-metadata">Generation starts only from an Item’s Summary button. Saved summaries stay readable when disabled.</p>
    {enabled && <div className="topic-stream-advanced-fields">
      <SettingsTextField label="Generation endpoint" binding={form.register("generation.endpoint", { setValueAs: nullableText })} error={form.formState.errors.generation?.endpoint} />
      <SettingsTextField label="Summary model" binding={form.register("generation.summaryModel", { setValueAs: nullableText })} error={form.formState.errors.generation?.summaryModel} />
      <SettingsTextField label="Reranking model" binding={form.register("generation.rerankModel", { setValueAs: nullableText })} error={form.formState.errors.generation?.rerankModel} />
      <p className="topic-stream-metadata">Use models already configured by your operator. Credentials are managed on the server.</p>
      <SettingsTextField label="Summary language" binding={form.register("summary.language")} error={form.formState.errors.summary?.language} />
      <label>Maximum input characters<input type="number" min={1000} max={1000000} {...form.register("summary.maxInputChars", { valueAsNumber: true })} /></label>
      <label>Maximum summary characters<input type="number" min={200} max={4000} {...form.register("summary.maxSummaryCharacters", { valueAsNumber: true })} /></label>
      <p className="topic-stream-metadata">Each attempt captures its starting settings. Changes do not regenerate saved text.</p>
    </div>}
  </section>;
}
function nullableText(value: unknown): string | null { return typeof value === "string" && value.trim() ? value.trim() : null; }
