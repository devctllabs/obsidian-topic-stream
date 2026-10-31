import type { ReactNode } from "react";

export type HostFrameSize = "leaf" | "narrow" | "mobile";

export function HostFrame({ children, size = "leaf", theme = "light", largeText = false, shell = false }: { children: ReactNode; size?: HostFrameSize; theme?: "light" | "dark"; largeText?: boolean; shell?: boolean }) {
  return <div className={`topic-stream-story-host theme-${theme}${largeText ? " topic-stream-story-large-text" : ""}`}>
    <div className={`workspace-leaf topic-stream-story-leaf topic-stream-story-frame--${size}`}>
      <div className="workspace-leaf-content" data-type="topic-stream-view">
        <div className={`view-content ${shell ? "topic-stream-story-shell" : "topic-stream-root"}`}>{children}</div>
      </div>
    </div>
  </div>;
}
