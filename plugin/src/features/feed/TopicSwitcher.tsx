import { useQuery } from "@tanstack/react-query";
import { createPortal } from "react-dom";
import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type KeyboardEvent } from "react";
import type { Topic } from "../../api/contracts";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import { focusNextToAnchor, placeAnchoredSurface } from "../../shared/components/anchored-surface";
import type { FeedService } from "./services";

export function TopicSwitcher({ client, value, onChange }: { client: Pick<FeedService, "listTopics">; value: string | null; onChange: (id: string | null) => void }) {
  const topics = useQuery({ queryKey: ["topics"], queryFn: () => client.listTopics() });
  const [anchor, setAnchor] = useState<HTMLButtonElement | null>(null);
  const name = value ? topics.data?.find((topic) => topic.id === value)?.name ?? "Unavailable Topic" : "All Topics";
  const close = (restore: boolean) => { if (restore) anchor?.focus({ preventScroll: true }); setAnchor(null); };
  return <><button type="button" className="topic-stream-topic-trigger" aria-haspopup="dialog" aria-expanded={Boolean(anchor)} onClick={(event) => setAnchor(anchor ? null : event.currentTarget)} title={name}>
    <span>{name}</span><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="m6 9 6 6 6-6" /></svg>
  </button>{anchor && <TopicPicker anchor={anchor} topics={topics.data ?? []} value={value} pending={topics.isPending} error={topics.error} onRetry={() => { void topics.refetch(); }} onClose={close} onChange={(id) => { close(true); onChange(id); }} />}</>;
}

export function TopicPicker({ anchor, topics, value, pending, error, onRetry, onClose, onChange, title = "Choose feed Topic", allowAll = true, errorTitle = "Could not load Topics" }: {
  anchor: HTMLButtonElement; topics: Topic[]; value: string | null; pending: boolean; error: Error | null;
  onRetry?: (() => void) | undefined; onClose: (restore: boolean) => void; onChange: (id: string | null) => void;
  title?: string; allowAll?: boolean; errorTitle?: string;
}) {
  const panel = useRef<HTMLDivElement>(null);
  const [position, setPosition] = useState<CSSProperties>({ visibility: "hidden" });
  const [search, setSearch] = useState("");
  const document = anchor.ownerDocument;
  const filtered = topics.filter((topic) => topic.enabled && topic.name.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()));
  useLayoutEffect(() => { if (panel.current) setPosition(placeAnchoredSurface(anchor, panel.current)); }, [anchor, filtered.length, error, pending]);
  useLayoutEffect(() => { if (position.visibility === "visible") panel.current?.querySelector("input")?.focus({ preventScroll: true }); }, [position.visibility]);
  useEffect(() => {
    const dismiss = (event: Event) => { if (!event.composedPath().includes(panel.current!) && !event.composedPath().includes(anchor)) onClose(false); };
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
  return createPortal(<div className={anchor.closest(".theme-dark") ? "theme-dark" : "theme-light"}><div ref={panel} role="dialog" aria-label={title} aria-busy={pending} className="topic-stream-root topic-stream-topic-picker" style={position} onKeyDown={(event) => movePicker(event, anchor, onClose)}>
    {!allowAll && <h2>{title}</h2>}
    <label>Find a Topic<input type="search" value={search} onChange={(event) => setSearch(event.target.value)} /></label>
    <div role="listbox" aria-label="Feed Topics">{allowAll && <TopicOption id={null} name="All Topics" selected={value === null} onChange={onChange} />}
      {filtered.map((topic) => <TopicOption key={topic.id} id={topic.id} name={topic.name} selected={value === topic.id} disabled={pending} onChange={onChange} />)}</div>
    <PickerFeedback pending={pending} error={error} errorTitle={errorTitle} empty={filtered.length === 0 && Boolean(search.trim())} onRetry={onRetry} />
  </div></div>, document.body);
}

function TopicOption({ id, name, selected, onChange, disabled = false }: { id: string | null; name: string; selected: boolean; onChange: (id: string | null) => void; disabled?: boolean }) {
  return <button type="button" role="option" aria-selected={selected} disabled={disabled} tabIndex={-1} onClick={() => onChange(id)}>{name}</button>;
}
function PickerFeedback({ pending, error, errorTitle, empty, onRetry }: { pending: boolean; error: Error | null; errorTitle: string; empty: boolean; onRetry?: (() => void) | undefined }) {
  if (error) return <><ApiErrorMessage title={errorTitle} error={error} />{onRetry && <button type="button" className="topic-stream-refresh" onClick={onRetry}>Try again</button>}</>;
  if (pending) return <p role="status" className="topic-stream-metadata">Loading Topics…</p>;
  return empty ? <p className="topic-stream-metadata">No matching Topics</p> : null;
}
function movePicker(event: KeyboardEvent<HTMLDivElement>, anchor: HTMLButtonElement, onClose: (restore: boolean) => void): void {
  if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); onClose(true); return; }
  if (event.key === "Tab") { movePickerTab(event, anchor, onClose); return; }
  const options = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="option"]:not(:disabled)'));
  const current = options.indexOf(event.target as HTMLButtonElement);
  if (["Home", "End"].includes(event.key) && current < 0) return;
  const index = pickerOptionIndex(event.key, current, options.length);
  if (index === null) return;
  event.preventDefault(); options[index]?.focus();
}

function pickerOptionIndex(key: string, current: number, count: number): number | null {
  switch (key) {
    case "ArrowDown": return (current + 1) % count;
    case "ArrowUp": return current < 0 ? count - 1 : (current - 1 + count) % count;
    case "Home": return 0;
    case "End": return count - 1;
    default: return null;
  }
}
function movePickerTab(event: KeyboardEvent<HTMLDivElement>, anchor: HTMLButtonElement, onClose: (restore: boolean) => void): void {
  const controls = Array.from(event.currentTarget.querySelectorAll<HTMLElement>('input:not(:disabled),button:not(:disabled):not([tabindex="-1"])'));
  const current = controls.indexOf(event.target as HTMLElement);
  const next = current < 0 ? controls[event.shiftKey ? 0 : 1] : controls[current + (event.shiftKey ? -1 : 1)];
  event.preventDefault();
  if (next) { next.focus(); return; }
  focusNextToAnchor(anchor, event.shiftKey); onClose(false);
}
