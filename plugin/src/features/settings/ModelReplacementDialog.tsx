import { useEffect, useRef } from "react";
export function ModelReplacementDialog({ selected, serving, onAccept, onCancel }: { selected: string; serving: string | null; onAccept: () => void; onCancel: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => { dialog.current?.showModal(); }, []);
  return <dialog ref={dialog} className="topic-stream-dialog" aria-label="Replace model preparation?" onCancel={(event) => { event.preventDefault(); onCancel(); }}>
    <h2>Replace model preparation?</h2><p>Saving {selected} will replace the current preparation.</p>
    <p>{serving ? `${serving} remains serving until the selected model is ready.` : "No model is serving yet. Retrieval stays unavailable until the selected model is ready; saved Feed history, Settings and Jobs remain usable."}</p>
    <div className="topic-stream-form-actions"><button type="button" onClick={onCancel}>Keep current selection</button><button type="button" onClick={onAccept}>Use selected model</button></div>
  </dialog>;
}
