import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type KeyboardEvent } from "react";
import type { ActionMenuItem } from "./ActionMenu";
import { focusNextToAnchor, placeAnchoredSurface } from "./anchored-surface";

export function MenuSurface({ id, label, items, anchor, edge, onClose }: {
  id: string; label: string; items: ActionMenuItem[]; anchor: HTMLButtonElement;
  edge: "first" | "last"; onClose: (restoreFocus: boolean) => void;
}) {
  const menu = useRef<HTMLDivElement>(null);
  const [position, setPosition] = useState<CSSProperties>({ visibility: "hidden" });
  const document = anchor.ownerDocument;
  const dark = Boolean(anchor.closest(".theme-dark"));
  useLayoutEffect(() => {
    const element = menu.current;
    if (!element) return;
    setPosition(placeAnchoredSurface(anchor, element));
  }, [anchor]);
  useLayoutEffect(() => {
    if (position.visibility !== "visible" || !menu.current) return;
    const enabled = menuButtons(menu.current);
    (edge === "last" ? enabled.at(-1) : enabled[0])?.focus({ preventScroll: true });
  }, [edge, position.visibility]);
  useEffect(() => {
    const dismiss = (event: Event) => {
      const path = event.composedPath();
      if (!path.includes(anchor) && !path.includes(menu.current!)) onClose(false);
    };
    const resized = () => onClose(true);
    document.addEventListener("pointerdown", dismiss);
    document.addEventListener("scroll", dismiss, true);
    document.defaultView?.addEventListener("resize", resized);
    return () => {
      document.removeEventListener("pointerdown", dismiss);
      document.removeEventListener("scroll", dismiss, true);
      document.defaultView?.removeEventListener("resize", resized);
    };
  }, [anchor, document, onClose]);
  return <div className={dark ? "theme-dark" : "theme-light"}>
    <div ref={menu} id={id} className="topic-stream-root topic-stream-action-menu" role="menu" aria-label={label} style={position}
      onKeyDown={(event) => moveMenu(event, anchor, onClose)}>
      {items.map((item) => <button type="button" key={item.id} role={item.checked === undefined ? "menuitem" : "menuitemcheckbox"} aria-checked={item.checked} tabIndex={-1} disabled={item.disabled}
        className={`${item.destructive ? "topic-stream-menu-destructive" : ""} ${item.separator ? "topic-stream-menu-separated" : ""}`}
        onClick={() => { onClose(true); item.onSelect(anchor); }}><span>{item.label}</span>{item.checked && <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="m5 12 4 4L19 6" /></svg>}</button>)}
    </div>
  </div>;
}
function menuButtons(element: HTMLElement): HTMLButtonElement[] {
  return Array.from(element.querySelectorAll<HTMLButtonElement>('button[role^="menuitem"]:not(:disabled)'));
}
function moveMenu(event: KeyboardEvent<HTMLDivElement>, anchor: HTMLButtonElement, onClose: (restoreFocus: boolean) => void): void {
  if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); onClose(true); return; }
  if (event.key === "Tab") { event.preventDefault(); focusNextToAnchor(anchor, event.shiftKey); onClose(false); return; }
  const buttons = menuButtons(event.currentTarget);
  const current = buttons.indexOf(event.target as HTMLButtonElement);
  const index = menuIndex(event.key, current, buttons.length);
  if (index === null) return;
  event.preventDefault(); buttons[index]?.focus({ preventScroll: true });
}
function menuIndex(key: string, current: number, count: number): number | null {
  if (!count) return null;
  if (key === "Home") return 0;
  if (key === "End") return count - 1;
  if (key === "ArrowDown") return (current + 1) % count;
  if (key === "ArrowUp") return (current - 1 + count) % count;
  return null;
}
