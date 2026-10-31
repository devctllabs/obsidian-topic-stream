import { useLayoutEffect, useRef, useSyncExternalStore, type ReactNode } from "react";
import { IconButton } from "../../shared/components/IconButton";
import type { SectionProps } from "./TopicStreamShell";

/** Feature owners supply list/detail content; this boundary owns pane geometry and restoration. */
export function SplitPane({ navigation, section, active, list, detail, positionKey = section, listRevision = 0, selection, onBack, backLabel = "Back to list", emptyMessage = "Select an item to read" }: SectionProps & { list: ReactNode; detail: ReactNode; positionKey?: string; listRevision?: number; selection?: string | null; onBack?: () => void; backLabel?: string; emptyMessage?: string }) {
  const state = useSyncExternalStore(navigation.subscribe, navigation.getSnapshot);
  const selectedId = selection === undefined ? state.selections[section] : selection;
  const back = () => { if (onBack) onBack(); else navigation.select(section, null); };
  const listRef = useRef<HTMLDivElement>(null);
  const detailRef = useRef<HTMLDivElement>(null);
  const previousSelection = useRef(selectedId);
  const previousPositionKey = useRef(positionKey);
  const previousRevision = useRef(listRevision);
  useLayoutEffect(() => {
    if (!active) return;
    const listElement = listRef.current;
    restoreListPosition(listElement, navigation, positionKey, previousPositionKey.current === positionKey && previousRevision.current !== listRevision);
    if (selectedId && detailRef.current) {
      detailRef.current.scrollTop = navigation.position(`${positionKey}:detail:${selectedId}`);
      if (previousSelection.current !== selectedId && previousPositionKey.current === positionKey) detailRef.current.querySelector<HTMLButtonElement>("button")?.focus({ preventScroll: true });
    } else if (previousSelection.current && previousPositionKey.current === positionKey) {
      restoreListFocus(listElement, previousSelection.current);
    }
    previousSelection.current = selectedId;
    previousPositionKey.current = positionKey;
    previousRevision.current = listRevision;
  }, [active, navigation, positionKey, selectedId, listRevision]);
  return <div className={`topic-stream-split${selectedId ? " topic-stream-split--selected" : ""}`} onKeyDown={(event) => {
    if (event.key === "Escape" && selectedId) { event.preventDefault(); back(); }
  }}>
    <div className="topic-stream-list-pane" ref={listRef} onScroll={(event) => {
      event.currentTarget.dataset.scrolled = String(event.currentTarget.scrollTop > 0);
      if (event.currentTarget.getClientRects().length) navigation.rememberPosition(`${positionKey}:list`, event.currentTarget.scrollTop);
    }}>{list}</div>
    <div className="topic-stream-detail-pane" ref={detailRef} tabIndex={-1} role="region" aria-label="Reading pane" onScroll={(event) => {
      if (selectedId && event.currentTarget.getClientRects().length) navigation.rememberPosition(`${positionKey}:detail:${selectedId}`, event.currentTarget.scrollTop);
    }}>
      {selectedId ? <><div className="topic-stream-detail-toolbar"><IconButton icon="back" label={backLabel} onClick={back} /></div>{detail}</> : <p className="topic-stream-detail-empty">{emptyMessage}</p>}
    </div>
  </div>;
}

function restoreListPosition(list: HTMLElement | null, navigation: SectionProps["navigation"], key: string, reset: boolean): void {
  if (!list) return;
  if (reset) navigation.rememberPosition(`${key}:list`, 0);
  list.scrollTop = navigation.position(`${key}:list`);
  list.dataset.scrolled = String(list.scrollTop > 0);
}

function restoreListFocus(list: HTMLElement | null, selectedId: string): void {
  const trigger = Array.from(list?.querySelectorAll<HTMLElement>("[data-item-id]") ?? []).find((element) => element.dataset.itemId === selectedId);
  trigger?.focus({ preventScroll: true });
}
