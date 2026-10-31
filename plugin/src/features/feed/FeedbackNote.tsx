import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type KeyboardEvent } from "react";
import { createPortal } from "react-dom";
import type { Feedback } from "../../api/contracts";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import { IconButton } from "../../shared/components/IconButton";
import { focusNextToAnchor, placeAnchoredSurface } from "../../shared/components/anchored-surface";

export interface FeedbackNoteProps {
  anchor: HTMLButtonElement; sentiment: Feedback["sentiment"]; comment: string;
  pending: boolean; sendingNote: boolean; error: Error | null;
  onChange: (value: string) => void; onSend: () => void; onClose: (restoreFocus: boolean) => void;
  topicCorrection?: { label: string; onSelect: () => void } | undefined;
}
export function FeedbackNote({ anchor, sentiment, comment, pending, sendingNote, error, onChange, onSend, onClose, topicCorrection }: FeedbackNoteProps) {
  const panel = useRef<HTMLDivElement>(null);
  const [position, setPosition] = useState<CSSProperties>({ visibility: "hidden" });
  const document = anchor.ownerDocument;
  useLayoutEffect(() => { if (panel.current) setPosition(placeAnchoredSurface(anchor, panel.current)); }, [anchor, error]);
  useLayoutEffect(() => {
    if (position.visibility === "visible") panel.current?.querySelector<HTMLElement>(pending ? "button" : "textarea")?.focus({ preventScroll: true });
  }, [position.visibility, pending]);
  useEffect(() => {
    const dismiss = (event: Event) => { if (!event.composedPath().includes(panel.current!)) onClose(false); };
    const resize = () => onClose(true);
    document.addEventListener("pointerdown", dismiss);
    document.addEventListener("scroll", dismiss, true);
    document.defaultView?.addEventListener("resize", resize);
    return () => {
      document.removeEventListener("pointerdown", dismiss);
      document.removeEventListener("scroll", dismiss, true);
      document.defaultView?.removeEventListener("resize", resize);
    };
  }, [anchor, document, onClose]);
  return createPortal(<div className={anchor.closest(".theme-dark") ? "theme-dark" : "theme-light"}>
    <div ref={panel} className="topic-stream-root topic-stream-note-panel" style={position} role="dialog" aria-label="Feedback note" onKeyDown={(event) => moveNote(event, anchor, onClose)}>
      <div className="topic-stream-section-header"><h2>{sentiment === "like" ? "Useful" : "Not for me"}</h2><IconButton icon="close" label="Close note" onClick={() => onClose(true)} /></div>
      <form onSubmit={(event) => { event.preventDefault(); if (!pending && comment.trim()) onSend(); }}>
        <label className="topic-stream-comment"><span>Optional note for future recommendations</span><textarea rows={3} value={comment} disabled={pending} maxLength={2000} onChange={(event) => onChange(event.target.value)} placeholder="What should we know?" /></label>
        <div className="topic-stream-note-footer"><span role="status" className="topic-stream-metadata">{pending ? sendingNote ? "Sending note…" : "Saving feedback…" : ""}</span>
          {topicCorrection && <button type="button" className="topic-stream-note-correction" disabled={pending} onClick={topicCorrection.onSelect}>{topicCorrection.label}</button>}
          <IconButton icon="send" type="submit" className="topic-stream-primary-action" label="Send note" disabled={pending || !comment.trim()} /></div>
      </form>
      {error && <ApiErrorMessage title={sendingNote ? "Could not send note" : "Could not save feedback"} error={error} />}
    </div>
  </div>, document.body);
}

function moveNote(event: KeyboardEvent<HTMLDivElement>, anchor: HTMLButtonElement, onClose: (restore: boolean) => void): void {
  if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); onClose(true); return; }
  if (event.key !== "Tab") return;
  const controls = Array.from(event.currentTarget.querySelectorAll<HTMLElement>("button:not(:disabled),textarea:not(:disabled)"));
  const boundary = event.shiftKey ? controls[0] : controls.at(-1);
  if (event.target !== boundary) return;
  event.preventDefault(); focusNextToAnchor(anchor, event.shiftKey); onClose(false);
}
