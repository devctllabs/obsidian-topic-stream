import { useIsMutating, useMutation, useQueryClient } from "@tanstack/react-query";
import type { Item } from "../../api/contracts";
import type { ActionMenuItem } from "../../shared/components/ActionMenu";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import type { FeedService } from "./services";
import { useItemReading } from "./item-reading";
import { TopicCorrectionError, useTopicCorrections } from "./topic-corrections";
import { patchItemState } from "./cache";
import { useVaultAction } from "./vault-action";
import type { FeedItemCapabilities } from "./capabilities";
import { useSummaryAction } from "./summary-action";

export interface ItemActionContext {
  client: FeedService; topicId: string | null; onDetails: () => void;
  onReadingState: (id: string, state: Item["readingState"]) => void;
  onResult: (message: string | null) => void;
  capabilities?: FeedItemCapabilities | undefined;
  summaryHeadingLevel?: 2 | 4;
}
export interface ItemActionResult { itemId: string; message: string; }
export function ItemActionListNotice({ result, selectedId }: { result: ItemActionResult | null; selectedId: string | undefined }) {
  if (!result || result.itemId === selectedId) return null;
  return <p role="status" className="topic-stream-metadata">{result.message}</p>;
}
export function useFeedItemActions({ client, item, topicId, onDetails, onReadingState, onResult, capabilities, summaryHeadingLevel }: ItemActionContext & { item: Item }) {
  const queries = useQueryClient();
  const reading = useItemReading(client, item, onReadingState);
  const correction = useTopicCorrections(client, item.id, topicId, onResult);
  const vault = useVaultAction(item, capabilities?.saveToVault);
  const summary = useSummaryAction(item, capabilities?.summary, summaryHeadingLevel);
  const hide = useMutation({ mutationKey: ["item-hide", item.id], mutationFn: () => client.setVisibility(item.id, "hidden"), retry: false,
    onSuccess: () => patchItemState(queries, item.id, { visibility: "hidden" }) });
  const hiding = useIsMutating({ mutationKey: ["item-hide", item.id] }) > 0;
  const items: ActionMenuItem[] = [
    { id: "details", label: "Why this item", separator: true, onSelect: onDetails },
    { id: "original", label: "Open original", onSelect: reading.openOriginal },
    ...vault.items,
    ...summary.items,
    { id: "reading", label: item.readingState === "read" ? "Mark as unread" : "Mark as read", onSelect: () => reading.mark(item.readingState === "read" ? "unread" : "read") },
    ...correction.items,
    { id: "hide", label: "Hide from all Feeds", disabled: item.visibility === "hidden", destructive: true,
      onSelect: () => { if (!queries.isMutating({ mutationKey: ["item-hide", item.id] })) hide.mutate(); } }
  ];
  return { items, reading, summary: summary.content, summaryTrigger: summary.trigger, pending: reading.isPending || hiding || correction.pending || vault.pending,
    status: <>{reading.error && <ApiErrorMessage title="Could not change reading state" error={reading.error} />}
      {hide.error && <ApiErrorMessage title="Could not hide Item" error={hide.error} />}<TopicCorrectionError error={correction.error} />{correction.panel}{vault.status}</> };
}
