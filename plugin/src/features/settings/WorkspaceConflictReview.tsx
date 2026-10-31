import { useMutation } from "@tanstack/react-query";
import type { Schema } from "../../api/contracts";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
export function WorkspaceConflictReview({ load, onAccept }: { load: () => Promise<Schema<"WorkspaceSettings">>; onAccept: (value: Schema<"WorkspaceSettings">, keepDraft: boolean) => void }) {
  const latest = useMutation({ mutationFn: load });
  const value = latest.data;
  if (value) return <section className="topic-stream-profile-review" aria-label="Latest saved workspace settings">
    <h3>Saved version {value.version}</h3><p>AI generation {value.generation.enabled ? "enabled" : "disabled"} · Summary language: {value.summary.language}</p>
    <p className="topic-stream-metadata">Keep your draft to review and save it against this version, or use the saved values.</p>
    <div className="topic-stream-form-actions"><button type="button" onClick={() => onAccept(value, true)}>Keep my draft</button><button type="button" onClick={() => onAccept(value, false)}>Use saved values</button></div>
  </section>;
  return <div><button type="button" className="topic-stream-refresh" disabled={latest.isPending} onClick={() => latest.mutate()}>Review latest</button>{latest.isError && <ApiErrorMessage title="Could not load latest workspace settings" error={latest.error} />}</div>;
}
