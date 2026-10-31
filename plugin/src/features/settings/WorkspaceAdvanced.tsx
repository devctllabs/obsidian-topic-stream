import type { UseFormReturn } from "react-hook-form";
import type { WorkspaceDraft } from "./workspace-draft";
export function WorkspaceAdvanced({ form }: { form: UseFormReturn<WorkspaceDraft> }) {
  return <details><summary>Advanced</summary><div className="topic-stream-advanced-fields">
    <h3>Retention</h3><label>Full content retention (days)<input type="number" min={1} placeholder="Keep indefinitely" {...form.register("retention.fullContentDays", { setValueAs: (value: unknown) => value === "" || value === null ? null : Number(value) })} /></label>
    <h3>Retrieval</h3>{numbers.map(([field, label, min, max, step]) => <label key={field}>{label}<input type="number" min={min} max={max} step={step} {...form.register(`retrieval.${field}`, { valueAsNumber: true })} /></label>)}
    <label className="topic-stream-checkbox"><input type="checkbox" {...form.register("retrieval.llmRerankEnabled")} />Use AI reranking for Digests</label>
    <label className="topic-stream-checkbox"><input type="checkbox" {...form.register("retrieval.digestExplorationEnabled")} />Include exploratory Digest candidates</label>
    <h3>Instructions</h3><p className="topic-stream-metadata">Saving creates a new settings revision. Existing summaries keep their captured instructions.</p>
    {instructions.map(([field, label]) => <label key={field}>{label}<textarea rows={3} maxLength={12000} {...form.register(`instructions.${field}`)} /></label>)}
  </div></details>;
}
const numbers = [
  ["candidateLimit", "Candidate limit", 1, 100, 1], ["rerankLimit", "Reranking limit", 1, 2000, 1], ["rrfK", "Rank fusion constant", 1, undefined, 1],
  ["recencyHalfLifeDays", "Recency half-life (days)", 0.001, undefined, "any"], ["recencyMaxBoost", "Maximum recency boost", 0, 1, "any"],
  ["feedSourceCap", "Feed per-source cap", 1, undefined, 1], ["digestSourceCap", "Digest per-source cap", 1, undefined, 1], ["digestMmrLambda", "Digest relevance balance", 0, 1, "any"]
] as const;
const instructions = [["summaryChunk", "Summary chunks"], ["summaryReduce", "Summary reduction"], ["summaryFinal", "Final summary"], ["digestRerank", "Digest reranking"]] as const;
