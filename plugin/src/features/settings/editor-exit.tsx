import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import type { ShellNavigation } from "../shell/navigation";

interface EditorExitOptions {
  navigation?: ShellNavigation | undefined; dirty: boolean; save: () => Promise<boolean>; discard: () => void;
  isDirty?: (() => boolean) | undefined;
  beforePrompt?: (() => void) | undefined;
}
export function useEditorExit(options: EditorExitOptions) {
  const { navigation } = options;
  const current = useRef(options);
  useLayoutEffect(() => { current.current = options; });
  const [proceed, setProceed] = useState<(() => void) | null>(null);
  const request = useCallback((next: () => void) => {
    const state = current.current;
    if (!(state.isDirty ? state.isDirty() : state.dirty)) { next(); return; }
    state.beforePrompt?.();
    setProceed(() => next);
  }, []);
  useEffect(() => navigation?.setLeaveGuard(request), [navigation, request]);
  return {
    open: proceed !== null,
    request,
    stay: () => setProceed(null),
    discard: () => { current.current.discard(); proceed?.(); setProceed(null); },
    save: async () => { if (await current.current.save()) proceed?.(); setProceed(null); }
  };
}

export function EditorExitDialog({ exit, saving, resource }: { exit: ReturnType<typeof useEditorExit>; saving: boolean; resource?: string }) {
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => { if (exit.open) dialog.current?.showModal(); }, [exit.open]);
  if (!exit.open) return null;
  return <dialog className="topic-stream-dialog" ref={dialog} aria-label="Unsaved changes" onCancel={(event) => { event.preventDefault(); exit.stay(); }}>
    <h2>{resource ? `Save ${resource} changes?` : "Save your changes?"}</h2><p>You have unsaved changes in this form.</p>
    <div className="topic-stream-form-actions">
      <button type="button" disabled={saving} onClick={() => { void exit.save(); }}>Save</button>
      <button type="button" disabled={saving} onClick={exit.discard}>Discard</button>
      <button type="button" disabled={saving} onClick={exit.stay}>Stay</button>
    </div>
  </dialog>;
}
