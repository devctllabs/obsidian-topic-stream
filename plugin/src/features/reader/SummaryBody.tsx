import { useCallback, useSyncExternalStore } from "react";
import { useMutation } from "@tanstack/react-query";
import type { ItemSummaryCapability } from "../feed/capabilities";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import type { ItemSummaries, SummaryInput } from "./item-summaries";

export function summaryCapability(model: ItemSummaries, captured?: SummaryInput): ItemSummaryCapability {
  return { subscribe: (item, listener) => model.subscribe(captured ?? item, listener), getSnapshot: (item) => model.getSnapshot(captured ?? item), request: (item) => model.request(captured ?? item),
    render: (item, expanded) => <SummaryBody input={captured ?? item} model={model} expanded={expanded} /> };
}
function SummaryBody({ input, model, expanded }: { input: SummaryInput; model: ItemSummaries; expanded: boolean }) {
  const subscribe = useCallback((listener: () => void) => model.subscribe(input, listener), [model, input]);
  const snapshot = useCallback(() => model.getResource(input), [model, input]);
  const resource = useSyncExternalStore(subscribe, snapshot);
  const refresh = useMutation({ mutationFn: () => model.refresh(input), retry: false });
  if (resource?.error) return <><ApiErrorMessage title="Could not read summary status" error={resource.error} /><button type="button" className="topic-stream-refresh" disabled={refresh.isPending} onClick={() => refresh.mutate()}>Check summary status</button></>;
  if (!expanded) return null;
  const state = resource?.data;
  if (state?.state === "ready") return <p className="topic-stream-summary-text">{state.result?.text}</p>;
  if (state?.state === "pending") return <><p role="status" className="topic-stream-metadata">Summary is being prepared.</p><button type="button" className="topic-stream-refresh" disabled={refresh.isPending} onClick={() => refresh.mutate()}>Check summary status</button></>;
  return null;
}
