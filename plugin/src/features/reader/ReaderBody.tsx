import { useCallback, useEffect, useId, useMemo, useRef, useState, useSyncExternalStore, type MouseEvent } from "react";
import { createPortal } from "react-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { Item } from "../../api/contracts";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import { openExternalUrl } from "../../obsidian/services/external-links";
import type { MarkdownRendererPort } from "./markdown";
import { prepareMarkdown, type ReaderImageReference } from "./prepare-markdown";
import type { ReaderImages } from "./images";
import { ReaderImage } from "./ReaderImage";
import { patchItemState } from "../feed/cache";

interface ImageSlot { reference: ReaderImageReference; element: HTMLElement; }
interface RenderState { pending: boolean; error: Error | null; images: ImageSlot[]; }

/** Source rendering is view-local; the host port owns and releases its Markdown child Component. */
export function ReaderBody({ item, renderer, images, reloadContent }: { item: Item; renderer: MarkdownRendererPort; images: ReaderImages; reloadContent?: () => Promise<Item> }) {
  const scope = useId();
  const target = useRef<HTMLDivElement>(null);
  const source = useMemo(() => ({ contentMarkdown: item.contentMarkdown, canonicalUrl: item.canonicalUrl, images: item.images }), [item.contentMarkdown, item.canonicalUrl, item.images]);
  const [revision, setRevision] = useState(0);
  const [state, setState] = useState<RenderState>({ pending: true, error: null, images: [] });
  const [linkError, setLinkError] = useState<string | null>(null);
  const subscribe = useCallback((listener: () => void) => images.subscribe({ id: item.id, images: item.images }, listener), [images, item.id, item.images]);
  const snapshot = useCallback(() => images.getSnapshot(item.id), [images, item.id]);
  const imageState = useSyncExternalStore(subscribe, snapshot);
  useEffect(() => {
    const element = target.current;
    if (!element || !source.contentMarkdown.trim()) return;
    let active = true;
    let dispose: (() => void) | undefined;
    void Promise.resolve().then(async () => {
      if (!active) return;
      const prepared = prepareMarkdown(source, scope);
      const rendering = renderer.render(prepared.markdown, element);
      dispose = rendering.dispose;
      await rendering.ready;
      if (active) {
        element.querySelectorAll<HTMLElement>("pre,table").forEach((region) => { region.tabIndex = 0; });
        setState({ pending: false, error: null, images: imageSlots(element, prepared.images) });
      }
    }).catch((error: unknown) => {
      dispose?.(); dispose = undefined;
      if (active) setState({ pending: false, error: error instanceof Error ? error : new Error("Could not render the source text."), images: [] });
    });
    return () => { active = false; dispose?.(); };
  }, [renderer, source, scope, revision]);
  const retry = () => { setState({ pending: true, error: null, images: [] }); setRevision((value) => value + 1); };
  return <div className="topic-stream-reader-body">
    <ContentAvailability item={item} />
    {!item.contentMarkdown.trim() && reloadContent && <ContentRecovery item={item} reload={reloadContent} />}
    <div ref={target} className="topic-stream-prose markdown-rendered" onClickCapture={(event) => readerLink(event, setLinkError)} />
    <ReaderFeedback item={item} state={state} onRetry={retry} />
    {linkError && <p role="alert" className="topic-stream-inline-error">{linkError}</p>}
    {state.images.map(({ reference, element }) => createPortal(<ReaderImage item={item} reference={reference} state={reference.image ? imageState.images.get(reference.image.id) : undefined} session={images} />, element, reference.token))}
  </div>;
}

function ContentRecovery({ item, reload }: { item: Item; reload: () => Promise<Item> }) {
  const queries = useQueryClient();
  const action = useMutation({ mutationFn: reload, retry: false, onSuccess: (fresh) => { patchItemState(queries, item.id, fresh); } });
  return <><button type="button" className="topic-stream-refresh" disabled={action.isPending} onClick={() => action.mutate()}>{action.isPending ? "Checking content…" : "Check for full content"}</button>
    {action.error && <ApiErrorMessage title="Could not reload this Item" error={action.error} />}
    {action.isSuccess && !action.data.contentMarkdown.trim() && <p role="status" className="topic-stream-metadata">Full content is still unavailable.</p>}</>;
}

function ContentAvailability({ item }: { item: Item }) {
  if (!item.contentMarkdown.trim()) return <><p>{item.excerpt}</p><p className="topic-stream-metadata">Full content is unavailable for this Item.</p></>;
  if (item.contentOrigin === "paper_abstract") return <p className="topic-stream-metadata">Only the source abstract is available.</p>;
  if (["article_description", "video_description", "repository_description"].includes(item.contentOrigin)) return <p className="topic-stream-metadata">Only the source description is available.</p>;
  return null;
}

function ReaderFeedback({ item, state, onRetry }: { item: Item; state: RenderState; onRetry: () => void }) {
  if (!item.contentMarkdown.trim()) return null;
  if (state.error) return <><p>{item.excerpt}</p><ApiErrorMessage title="Could not display this Item" error={state.error} /><button type="button" className="topic-stream-refresh" onClick={onRetry}>Try again</button></>;
  return state.pending ? <p role="status" className="topic-stream-metadata">Loading article…</p> : null;
}

function imageSlots(target: HTMLElement, references: ReaderImageReference[]): ImageSlot[] {
  const anchors = Array.from(target.querySelectorAll<HTMLAnchorElement>("a"));
  return references.map((reference) => {
    const element = target.ownerDocument.createElement("span");
    const anchor = anchors.find((value) => value.getAttribute("href") === reference.token);
    if (anchor) anchor.replaceWith(element); else target.append(element);
    return { reference, element };
  });
}

function readerLink(event: MouseEvent<HTMLElement>, onError: (message: string | null) => void): void {
  const anchor = (event.target as Element).closest<HTMLAnchorElement>("a");
  if (!anchor) return;
  event.preventDefault(); event.stopPropagation();
  const href = anchor.getAttribute("href") ?? "";
  if (href.startsWith("https://topic-stream.invalid/reader-image/")) return;
  try {
    if (href.startsWith("#")) { const id = decodeURIComponent(href.slice(1)); Array.from(event.currentTarget.querySelectorAll<HTMLElement>("[id],[data-heading]")).find((element) => element.id === id || element.dataset.heading === id)?.scrollIntoView({ block: "start" }); }
    else openExternalUrl(anchor.ownerDocument.defaultView, href);
    onError(null);
  } catch { onError("Could not open this link."); }
}
