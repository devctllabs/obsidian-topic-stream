import { useRef, useState } from "react";
import type { Item } from "../../api/contracts";
import type { ReaderImageReference } from "./prepare-markdown";
import type { ReaderImages, ReaderImageState } from "./images";

export function ReaderImage({ item, reference, state, session }: { item: Item; reference: ReaderImageReference; state: ReaderImageState | undefined; session: ReaderImages }) {
  const frame = useRef<HTMLSpanElement>(null);
  const label = reference.alt || "Image";
  const activate = () => { frame.current?.focus({ preventScroll: true }); session.allow(item); };
  const retry = () => { frame.current?.focus({ preventScroll: true }); if (reference.image) session.retry(item.id, reference.image.id); };
  return <span ref={frame} className="topic-stream-reader-image" role="group" aria-label={`Image: ${label}`} tabIndex={-1}>
    <ImageContent reference={reference} label={label} state={state} onAllow={activate} onRetry={retry}
      onDecodeError={(url) => { if (reference.image) session.failedToDisplay(item.id, reference.image.id, url); }} />
  </span>;
}

function ImageContent({ reference, label, state, onAllow, onRetry, onDecodeError }: { reference: ReaderImageReference; label: string; state: ReaderImageState | undefined; onAllow: () => void; onRetry: () => void; onDecodeError: (url: string) => void }) {
  if (!reference.image) return <span className="topic-stream-image-placeholder" role="img" aria-label={`Unavailable image: ${label}`}>{label}<span className="topic-stream-metadata">Image unavailable from this source.</span></span>;
  if (state?.kind === "ready") return <LoadedImage key={state.url} url={state.url} label={label} onError={onDecodeError} />;
  if (state?.kind === "failed") return <span className="topic-stream-image-placeholder"><span role="alert" className="topic-stream-inline-error"><strong>Could not load image</strong><span>{state.error.message}</span></span><button type="button" className="topic-stream-refresh" aria-label={`Retry image: ${label}`} onClick={onRetry}>Retry image</button></span>;
  if (state?.kind === "loading") return <span className="topic-stream-image-placeholder" role="status">Loading image…</span>;
  return <button type="button" className="topic-stream-image-placeholder" onClick={onAllow}><span>{label}</span><span className="topic-stream-metadata">Load all images in this Item</span></button>;
}

function LoadedImage({ url, label, onError }: { url: string; label: string; onError: (url: string) => void }) {
  const [loaded, setLoaded] = useState(false);
  return <><img src={url} alt={label} decoding="async" onLoad={() => setLoaded(true)} onError={() => onError(url)} />{!loaded && <span role="status" className="topic-stream-metadata">Loading image…</span>}</>;
}
