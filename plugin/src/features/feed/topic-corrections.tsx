import { useMutation, useQuery, useQueryClient, type UseQueryResult } from "@tanstack/react-query";
import { useState } from "react";
import type { RequestBody, Topic } from "../../api/contracts";
import type { ActionMenuItem } from "../../shared/components/ActionMenu";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import { ApiError } from "../../api/errors";
import { TopicPicker } from "./TopicSwitcher";
import { reconcileFeedTraversals } from "./reconcile-feed";
import type { FeedService } from "./services";

type Correction = RequestBody<"setItemTopicCorrection">;
interface Picker { anchor: HTMLButtonElement; membership: Correction["membership"]; title: string; }

export function useTopicCorrections(client: FeedService, itemId: string, topicId: string | null, onResult: (message: string | null) => void) {
  const queries = useQueryClient();
  const topics = useQuery({ queryKey: ["topics"], queryFn: () => client.listTopics() });
  const [picker, setPicker] = useState<Picker | null>(null);
  const close = (restore: boolean) => { if (restore) picker?.anchor.focus({ preventScroll: true }); setPicker(null); };
  const mutation = useMutation({ mutationKey: ["item-correction-write", itemId], retry: false,
    mutationFn: async (input: Correction) => {
      try { await client.setTopicCorrection(itemId, input); return null; }
      catch (error) { if (error instanceof ApiError && error.canonicalCommitted) return error; throw error; }
    },
    onSuccess: async (pending, input) => {
      queries.setQueryData(["item-correction", itemId, input.topicId], { ...input, projectionRevision: pending?.projectionRevision ?? null });
      onResult(pending ? "Topic correction saved. Retrieval update pending." : null);
      close(true);
      try { await Promise.all([reconcileFeedTraversals(queries, client), queries.refetchQueries({ queryKey: ["search"], type: "all" }, { throwOnError: true, cancelRefetch: false })]); }
      catch { onResult("Topic correction saved. Could not update loaded results. Refresh to try again."); }
    }
  });
  const save = (input: Correction) => {
    if (queries.isMutating({ mutationKey: ["item-correction-write", itemId] })) return;
    onResult(null); mutation.mutate(input);
  };
  const name = topics.data?.find((topic) => topic.id === topicId)?.name ?? "this Topic";
  const items: ActionMenuItem[] = [
    { id: "add-topic", label: "Add to Topic…", separator: true, onSelect: (anchor) => setPicker({ anchor, membership: "include", title: "Add to Topic" }) },
    { id: "wrong-topic", label: topicId ? `Wrong topic: ${name}` : "Wrong topic…", onSelect: (anchor) => topicId ? save({ topicId, membership: "exclude" }) : setPicker({ anchor, membership: "exclude", title: "Wrong topic" }) },
    { id: "automatic-topic", label: topicId ? `Use automatic matching: ${name}` : "Reset Topic matching…", onSelect: (anchor) => topicId ? save({ topicId, membership: "automatic" }) : setPicker({ anchor, membership: "automatic", title: "Use automatic matching" }) }
  ];
  return { items, pending: mutation.isPending, error: picker ? null : mutation.error,
    panel: <CorrectionPicker picker={picker} topics={topics} action={mutation} onClose={close} onSave={save} /> };
}

function CorrectionPicker({ picker, topics, action, onClose, onSave }: { picker: Picker | null; topics: UseQueryResult<Topic[], Error>; action: { error: Error | null; isPending: boolean }; onClose: (restore: boolean) => void; onSave: (input: Correction) => void }) {
  if (!picker) return null;
  return <TopicPicker anchor={picker.anchor} title={picker.title} allowAll={false} topics={topics.data ?? []} value={null} pending={topics.isPending || action.isPending} error={action.error ?? topics.error}
    errorTitle={action.error ? "Could not change Topic membership" : "Could not load Topics"} onRetry={action.error ? undefined : () => { void topics.refetch(); }} onClose={onClose} onChange={(id) => { if (id) onSave({ topicId: id, membership: picker.membership }); }} />;
}

export function TopicCorrectionError({ error }: { error: Error | null }) {
  return error ? <ApiErrorMessage title="Could not change Topic membership" error={error} /> : null;
}
