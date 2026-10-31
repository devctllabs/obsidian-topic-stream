import type { CSSProperties } from "react";

export function placeAnchoredSurface(anchor: HTMLElement, surface: HTMLElement): CSSProperties {
  const bounds = anchor.getBoundingClientRect();
  const size = surface.getBoundingClientRect();
  const viewport = anchor.ownerDocument.documentElement;
  const left = Math.max(8, Math.min(bounds.right - size.width, viewport.clientWidth - size.width - 8));
  const top = bounds.bottom + size.height + 8 > viewport.clientHeight ? Math.max(8, bounds.top - size.height - 6) : bounds.bottom + 6;
  return { left, top, maxHeight: viewport.clientHeight - top - 8, visibility: "visible" };
}

export function focusNextToAnchor(anchor: HTMLButtonElement, backwards: boolean): void {
  const root = anchor.closest(".topic-stream-root");
  const controls = Array.from(root?.querySelectorAll<HTMLElement>('button:not(:disabled),a[href],input:not(:disabled),select:not(:disabled),textarea:not(:disabled),[tabindex="0"]') ?? [])
    .filter((element) => element.tabIndex >= 0 && element.getClientRects().length > 0);
  const index = controls.indexOf(anchor);
  (controls[index + (backwards ? -1 : 1)] ?? anchor).focus();
}
