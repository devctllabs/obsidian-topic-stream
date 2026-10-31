import { useLayoutEffect, useRef, useState } from "react";
import type { Schema } from "../../api/contracts";
import { ConnectedItemCard } from "../feed/FeedCard";
import type { ItemActionContext, ItemActionResult } from "../feed/item-actions";
import type { FeedItemCapabilities } from "../feed/capabilities";

export type CapturedCapabilities = (issueId: string, saved: Schema<"DigestItem">) => FeedItemCapabilities;
export function DigestReader({ issueId, saved, capabilitiesFor, ...context }: ItemActionContext & { issueId: string; saved: Schema<"DigestItem">; capabilitiesFor: CapturedCapabilities }) {
  const [details, setDetails] = useState(false);
  const [result, setResult] = useState<ItemActionResult | null>(null);
  const article = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => { article.current?.querySelector<HTMLAnchorElement>("h1 a")?.focus({ preventScroll: true }); }, []);
  return <div ref={article}><ConnectedItemCard {...context} entry={{ item: saved.item, explanation: saved.explanation }} details={details} onDetails={() => setDetails(true)} onResult={(message) => setResult(message ? { itemId: saved.item.id, message } : null)} actionResult={result} capabilities={capabilitiesFor(issueId, saved)} /></div>;
}
