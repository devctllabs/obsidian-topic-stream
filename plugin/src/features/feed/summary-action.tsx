import { useIsMutating, useMutation, useQueryClient } from "@tanstack/react-query";
import { useCallback, useState, useSyncExternalStore, type ReactNode } from "react";
import type { Item, Schema } from "../../api/contracts";
import type { ActionMenuItem } from "../../shared/components/ActionMenu";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import type { ItemSummaryCapability } from "./capabilities";

/** No Job tracker here: UI 07 supplies the shared model and body through this capability. */
export function useSummaryAction(item: Item, summary: ItemSummaryCapability | undefined, headingLevel: 2 | 4 = 2) {
  const queries = useQueryClient();
  const state = useSummarySnapshot(item, summary);
  const [expandedVersion, setExpandedVersion] = useState<string | null>(null);
  const expanded = expandedVersion === item.summarySourceVersion;
  const key = ["item-summary-request", item.id, item.summarySourceVersion];
  const mutation = useMutation({ mutationKey: key, retry: false, mutationFn: async () => {
    if (!summary) throw new Error("AI summary is unavailable.");
    await summary.request(item);
  } });
  const requesting = useIsMutating({ mutationKey: key }) > 0;
  const choose = (retry = false) => {
    const current = summary?.getSnapshot(item) ?? null;
    if (!current || !summaryAvailable(item, current)) return;
    if (current.state === "ready") { setExpandedVersion((value) => value === item.summarySourceVersion ? null : item.summarySourceVersion); return; }
    setExpandedVersion(item.summarySourceVersion);
    if (shouldRequest(current, retry) && !queries.isMutating({ mutationKey: key })) mutation.mutate();
  };
  const items: ActionMenuItem[] = summaryAvailable(item, state) ? [{ id: "summary", label: "AI summary", onSelect: () => choose() }] : [];
  const presentation = summaryPresentation(item, state, mutation.error);
  const body = summary?.render(item, expanded && presentation.matches);
  return { items, trigger: items.length > 0 ? <button type="button" className="topic-stream-refresh topic-stream-summary-trigger" aria-expanded={expanded} onClick={() => choose()}>AI summary</button> : null, content: <SummaryDisclosure headingLevel={headingLevel} expanded={expanded} busy={requesting || presentation.pending} submitting={requesting} body={body} error={presentation.error} canRetry={presentation.canRetry} onRetry={() => choose(true)} /> };
}

function useSummarySnapshot(item: Item, summary: ItemSummaryCapability | undefined) {
  const subscribe = useCallback((listener: () => void) => summary?.subscribe(item, listener) ?? (() => undefined), [item, summary]);
  const snapshot = useCallback(() => summary?.getSnapshot(item) ?? null, [item, summary]);
  return useSyncExternalStore(subscribe, snapshot);
}
function shouldRequest(state: Schema<"ItemSummary">, retry: boolean): boolean {
  return state.state !== "pending" && (state.state !== "failed" || retry);
}
function summaryPresentation(item: Item, state: Schema<"ItemSummary"> | null, mutationError: Error | null) {
  if (state?.sourceVersion !== item.summarySourceVersion) return { matches: false, pending: false, error: mutationError, canRetry: false };
  const failed = state.state === "failed";
  return { matches: true, pending: state.state === "pending", error: summaryError(state, mutationError),
    canRetry: state.canGenerate && (!failed || (state.retryable && Boolean(state.jobId))) };
}
function summaryError(state: Schema<"ItemSummary">, mutationError: Error | null): Error | null {
  if (state.state === "ready") return null;
  return state.state === "failed" ? new Error(state.failure ?? "Summary generation failed.") : mutationError;
}
function summaryAvailable(item: Item, state: Schema<"ItemSummary"> | null): boolean {
  if (state?.sourceVersion !== item.summarySourceVersion) return false;
  if (state.state === "ready" && state.result?.sourceVersion === item.summarySourceVersion) return true;
  if (state.state === "pending") return true;
  return item.summarySupported && state.supported && state.canGenerate;
}
function SummaryDisclosure({ expanded, busy, submitting, body, error, canRetry, onRetry, headingLevel }: { expanded: boolean; busy: boolean; submitting: boolean; body: ReactNode; error: Error | null; canRetry: boolean; onRetry: () => void; headingLevel: 2 | 4 }) {
  if (!expanded) return body;
  const Heading = `h${headingLevel}` as const;
  return <section className="topic-stream-summary-disclosure" role="group" aria-label="AI summary" aria-busy={busy}>
    <Heading>AI summary</Heading>{submitting && <p role="status" className="topic-stream-metadata">Preparing summary…</p>}{body}
    {error && <><ApiErrorMessage title="Could not request AI summary" error={error} />{canRetry && <button type="button" className="topic-stream-refresh" disabled={busy} onClick={onRetry}>Retry AI summary</button>}</>}
  </section>;
}
