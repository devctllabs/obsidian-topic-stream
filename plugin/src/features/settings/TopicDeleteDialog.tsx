import { useEffect, useRef } from "react";
import type { Topic } from "../../api/contracts";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";

export function TopicDeleteDialog({ topic, pending, error, onConfirm, onCancel }: {
  topic: Topic; pending: boolean; error: unknown; onConfirm: () => void; onCancel: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => { dialog.current?.showModal(); }, []);
  return <dialog ref={dialog} className="topic-stream-dialog" aria-label={`Delete ${topic.name}?`} onCancel={(event) => { event.preventDefault(); if (!pending) onCancel(); }}>
    <h2>Delete {topic.name}?</h2><p>Stops future Topic preparation and removes it from Digest Definitions. Saved Digest Issues remain.</p>
    <div className="topic-stream-form-actions"><button type="button" disabled={pending} onClick={onCancel}>Cancel</button>
      <button type="button" disabled={pending} onClick={onConfirm}>Delete topic</button>
    </div>
    {pending && <p role="status">Deleting…</p>}
    {error != null && <ApiErrorMessage title="Could not delete topic" error={error} />}
  </dialog>;
}
