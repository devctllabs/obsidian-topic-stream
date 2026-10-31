import { useIsMutating, useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import type { Item } from "../../api/contracts";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import type { ActionMenuItem } from "../../shared/components/ActionMenu";
import type { FeedItemCapabilities } from "./capabilities";

export function useVaultAction(item: Item, save: FeedItemCapabilities["saveToVault"]) {
  const queries = useQueryClient();
  const key = ["item-vault-save", item.id];
  const mutation = useMutation({ mutationKey: key, retry: false, mutationFn: async (anchor: HTMLElement) => {
    if (!save) throw new Error("Saving to vault is unavailable.");
    return save(item, anchor);
  } });
  const pending = useIsMutating({ mutationKey: key }) > 0;
  useEffect(() => { if (!pending && (mutation.isSuccess || mutation.isError)) mutation.variables?.focus({ preventScroll: true }); }, [pending, mutation.isSuccess, mutation.isError, mutation.variables]);
  const items: ActionMenuItem[] = save ? [{ id: "save", label: "Save to vault", onSelect: (anchor) => {
    if (!queries.isMutating({ mutationKey: key })) mutation.mutate(anchor);
  } }] : [];
  return { items, pending, status: <>
    {mutation.isSuccess && mutation.data && <p role="status" className="topic-stream-metadata">Saved to vault.</p>}
    {mutation.isSuccess && mutation.data?.linkedImages !== undefined && mutation.data.linkedImages > 0 && <p role="status" className="topic-stream-metadata">{mutation.data.localImages ?? 0} images saved locally; {mutation.data.linkedImages} kept as links.</p>}
    {mutation.error && <ApiErrorMessage title="Could not save to vault" error={mutation.error} />}</> };
}
