import { useEffect, useRef } from "react";
import type { Source } from "../../api/contracts";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";

export function SourceDeleteDialog({ source, pending, error, onConfirm, onCancel }: {
  source: Source; pending: boolean; error: unknown; onConfirm: () => void; onCancel: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => { dialog.current?.showModal(); }, []);
  return <dialog ref={dialog} className="topic-stream-dialog" aria-label={`Delete ${source.name}?`} onCancel={(event) => { event.preventDefault(); if (!pending) onCancel(); }}>
    <h2>Delete {source.name}?</h2>
    <p>Stops future syncs. Existing Items and saved Digest Issues stay.</p>
    <p className="topic-stream-metadata">Removes this Source from Topic scopes. An empty scope uses all Sources.</p>
    <div className="topic-stream-form-actions"><button type="button" disabled={pending} onClick={onCancel}>Cancel</button>
      <button type="button" disabled={pending} onClick={onConfirm}>Delete source</button>
    </div>
    {pending && <p role="status">Deleting…</p>}
    {error != null && <ApiErrorMessage title="Could not delete source" error={error} />}
  </dialog>;
}
