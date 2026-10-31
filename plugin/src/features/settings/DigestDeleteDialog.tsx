import { useEffect, useRef } from "react";
import type { Schema } from "../../api/contracts";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
export function DigestDeleteDialog({ digest, pending, error, onConfirm, onCancel }: { digest: Schema<"DigestDefinition">; pending: boolean; error: unknown; onConfirm: () => void; onCancel: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => { dialog.current?.showModal(); }, []);
  return <dialog ref={dialog} className="topic-stream-dialog" aria-label={`Delete ${digest.name}?`} onCancel={(event) => { event.preventDefault(); if (!pending) onCancel(); }}>
    <h2>Delete {digest.name}?</h2><p>Removes this Definition, stops future scheduling and cancels unfinished Builds. Saved Digest Issues remain readable.</p>
    <div className="topic-stream-form-actions"><button type="button" disabled={pending} onClick={onCancel}>Cancel</button><button type="button" disabled={pending} onClick={onConfirm}>Delete definition</button></div>
    {pending && <p role="status">Deleting…</p>}{error != null && <ApiErrorMessage title="Could not delete digest definition" error={error} />}
  </dialog>;
}
