import { useMutation } from "@tanstack/react-query";
import type { Schema } from "../../api/contracts";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
export function DigestConflictReview({ load, onAccept }: { load: () => Promise<Schema<"DigestDefinition">>; onAccept: (value: Schema<"DigestDefinition">, keepDraft: boolean) => void }) {
  const latest = useMutation({ mutationFn: load });
  const value = latest.data;
  if (value) return <section className="topic-stream-profile-review" aria-label="Latest saved definition"><h3>Saved version {value.version}</h3>
    <p>{value.name} · {value.enabled ? "Scheduled" : "Manual only"} · {value.topics.length} Topics</p>
    <p className="topic-stream-metadata">Keep your draft to review and save it against this version, or use the saved values.</p>
    <div className="topic-stream-form-actions"><button type="button" onClick={() => onAccept(value, true)}>Keep my draft</button><button type="button" onClick={() => onAccept(value, false)}>Use saved values</button></div>
  </section>;
  return <div><button type="button" className="topic-stream-refresh" disabled={latest.isPending} onClick={() => latest.mutate()}>Review latest</button>{latest.isError && <ApiErrorMessage title="Could not load latest definition" error={latest.error} />}</div>;
}
