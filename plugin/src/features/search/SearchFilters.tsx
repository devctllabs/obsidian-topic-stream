import { useQuery } from "@tanstack/react-query";
import { zodResolver } from "@hookform/resolvers/zod";
import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type KeyboardEvent } from "react";
import { createPortal } from "react-dom";
import { useForm, useWatch, type FieldErrors, type UseFormRegisterReturn } from "react-hook-form";
import { z } from "zod";
import type { SearchRequest, SearchService } from "./services";
import { IconButton } from "../../shared/components/IconButton";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import { focusNextToAnchor, placeAnchoredSurface } from "../../shared/components/anchored-surface";

export type SearchFilterValues = Pick<SearchRequest, "topicId" | "sourceId" | "publishedAfter" | "publishedBefore">;
const date = z.iso.date().or(z.literal(""));
const schema = z.object({ topicId: z.string(), sourceId: z.string(), from: date, through: date })
  .refine((value) => !value.from || !value.through || value.from <= value.through, { path: ["through"], message: "Choose an end date on or after the start date." });
type FilterForm = z.infer<typeof schema>;
const empty: FilterForm = { topicId: "", sourceId: "", from: "", through: "" };

export function SearchFilters({ client, value, onApply }: { client: Pick<SearchService, "listTopics" | "listSources">; value: SearchFilterValues; onApply: (value: SearchFilterValues) => void }) {
  const [anchor, setAnchor] = useState<HTMLButtonElement | null>(null);
  const close = (restore: boolean) => { if (restore) anchor?.focus({ preventScroll: true }); setAnchor(null); };
  const active = Object.values(value).some(Boolean);
  return <><button type="button" className="topic-stream-refresh" aria-haspopup="dialog" aria-expanded={Boolean(anchor)} aria-pressed={active} onClick={(event) => setAnchor(anchor ? null : event.currentTarget)}>Filters</button>
    {anchor && <FilterPanel client={client} value={value} anchor={anchor} onClose={close} onApply={(next) => { close(true); onApply(next); }} />}</>;
}

function FilterPanel({ client, value, anchor, onClose, onApply }: { client: Pick<SearchService, "listTopics" | "listSources">; value: SearchFilterValues; anchor: HTMLButtonElement; onClose: (restore: boolean) => void; onApply: (value: SearchFilterValues) => void }) {
  const topics = useQuery({ queryKey: ["topics"], queryFn: () => client.listTopics() });
  const sources = useQuery({ queryKey: ["sources"], queryFn: () => client.listSources() });
  const form = useForm<FilterForm>({ resolver: zodResolver(schema), defaultValues: formValues(value) });
  const [topicId, sourceId] = useWatch({ control: form.control, name: ["topicId", "sourceId"] });
  const panel = useRef<HTMLDivElement>(null);
  const [position, setPosition] = useState<CSSProperties>({ visibility: "hidden" });
  const errors = Object.keys(form.formState.errors).length;
  const error = topics.error ?? sources.error;
  useLayoutEffect(() => { if (panel.current) setPosition(placeAnchoredSurface(anchor, panel.current)); }, [anchor, errors, error]);
  useLayoutEffect(() => { if (position.visibility === "visible") panel.current?.querySelector("select")?.focus({ preventScroll: true }); }, [position.visibility]);
  useEffect(() => {
    const document = anchor.ownerDocument;
    const bounds = anchor.getBoundingClientRect();
    const dismiss = (event: Event) => { if (!event.composedPath().includes(panel.current!) && !event.composedPath().includes(anchor)) onClose(false); };
    const scroll = () => {
      const current = anchor.getBoundingClientRect();
      if (current.top !== bounds.top || current.left !== bounds.left) onClose(false);
    };
    const resize = () => onClose(true);
    document.addEventListener("pointerdown", dismiss); document.addEventListener("scroll", scroll, true);
    document.defaultView?.addEventListener("resize", resize);
    return () => { document.removeEventListener("pointerdown", dismiss); document.removeEventListener("scroll", scroll, true); document.defaultView?.removeEventListener("resize", resize); };
  }, [anchor, onClose]);
  return createPortal(<div className={anchor.closest(".theme-dark") ? "theme-dark" : "theme-light"}><div ref={panel} role="dialog" aria-label="Search filters" className="topic-stream-root topic-stream-search-filter-panel" style={position} onKeyDown={(event) => panelKeys(event, anchor, onClose)}>
    <div className="topic-stream-section-header"><h2>Search filters</h2><IconButton icon="close" label="Close filters" onClick={() => onClose(true)} /></div>
    <form className="topic-stream-settings-form" onSubmit={(event) => { void form.handleSubmit((input) => onApply(filterRequest(input)))(event); }}>
      <fieldset><FilterSelect label="Topic" field={form.register("topicId")} value={topicId} options={topics.data ?? []} pending={topics.isPending} />
        <FilterSelect label="Source" field={form.register("sourceId")} value={sourceId} options={sources.data ?? []} pending={sources.isPending} />
        <label>Published from<input type="date" {...form.register("from")} /></label>
        <label>Published through<input type="date" aria-invalid={Boolean(form.formState.errors.through)} {...form.register("through")} /></label>
      </fieldset>
      <p className="topic-stream-metadata">Publication dates use UTC.</p>
      <FilterFeedback errors={form.formState.errors} error={error} onRetry={() => { void topics.refetch(); void sources.refetch(); }} />
      <div className="topic-stream-form-actions"><button type="button" className="topic-stream-refresh" onClick={() => form.reset(empty)}>Clear filters</button><button type="submit" className="topic-stream-primary-action" disabled={topics.isPending || sources.isPending}>Apply filters</button></div>
    </form>
  </div></div>, anchor.ownerDocument.body);
}

function FilterSelect({ label, field, value, options, pending }: { label: "Topic" | "Source"; field: UseFormRegisterReturn; value: string; options: { id: string; name: string }[]; pending: boolean }) {
  const missing = value && !options.some((option) => option.id === value);
  return <label>{label}<select {...field} value={value}><option value="">{label === "Topic" ? "All Topics" : "All Sources"}</option>
    {missing && <option value={value}>Selected {label}{pending ? "" : " is unavailable"}</option>}
    {options.map((option) => <option key={option.id} value={option.id}>{option.name}</option>)}
  </select></label>;
}

function formValues(value: SearchFilterValues): FilterForm {
  return { topicId: value.topicId ?? "", sourceId: value.sourceId ?? "", from: value.publishedAfter?.slice(0, 10) ?? "", through: value.publishedBefore?.slice(0, 10) ?? "" };
}

function FilterFeedback({ errors, error, onRetry }: { errors: FieldErrors<FilterForm>; error: Error | null; onRetry: () => void }) {
  return <>{Object.keys(errors).length > 0 && <p role="alert" className="topic-stream-inline-error">{errors.through?.message ?? "Choose valid publication dates."}</p>}
    {error && <><ApiErrorMessage title="Could not load filter options" error={error} /><button type="button" className="topic-stream-refresh" onClick={onRetry}>Try again</button></>}
  </>;
}

function filterRequest(input: FilterForm): SearchFilterValues {
  return { ...(input.topicId ? { topicId: input.topicId } : {}), ...(input.sourceId ? { sourceId: input.sourceId } : {}),
    ...(input.from ? { publishedAfter: `${input.from}T00:00:00Z` } : {}), ...(input.through ? { publishedBefore: `${input.through}T23:59:59.999999999Z` } : {}) };
}

function panelKeys(event: KeyboardEvent<HTMLDivElement>, anchor: HTMLButtonElement, onClose: (restore: boolean) => void): void {
  if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); onClose(true); return; }
  if (event.key !== "Tab") return;
  const controls = Array.from(event.currentTarget.querySelectorAll<HTMLElement>("button:not(:disabled),input:not(:disabled),select:not(:disabled)"));
  const index = controls.indexOf(event.target as HTMLElement);
  if ((event.shiftKey && index === 0) || (!event.shiftKey && index === controls.length - 1)) { event.preventDefault(); focusNextToAnchor(anchor, event.shiftKey); onClose(false); }
}
