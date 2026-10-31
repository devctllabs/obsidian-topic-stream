import type { ReactNode } from "react";

export function EditorPane({ label, children }: { label: string; children: ReactNode }) {
  return <aside className="topic-stream-editor-pane" role="region" aria-label={label}>{children}</aside>;
}
