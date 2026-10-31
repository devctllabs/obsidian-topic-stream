import { useIsMutating, useMutation, useMutationState, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import type { Item } from "../../api/contracts";
import { openExternalUrl } from "../../obsidian/services/external-links";
import type { FeedService } from "./services";

export function useItemReading(client: Pick<FeedService, "setReadingState">, item: Item, onReadingState: (id: string, state: Item["readingState"]) => void) {
  const queries = useQueryClient();
  const key = ["item-reading", item.id];
  const [openingError, setOpeningError] = useState<Error | null>(null);
  const mutation = useMutation({ mutationKey: key, retry: false,
    mutationFn: (state: Item["readingState"]) => client.setReadingState(item.id, state),
    onSuccess: (_value, state) => onReadingState(item.id, state) });
  const pending = useIsMutating({ mutationKey: key }) > 0;
  const errors = useMutationState({ filters: { mutationKey: key }, select: (value) => value.state.error });
  const mark = (state: Item["readingState"]) => {
    if (queries.isMutating({ mutationKey: key })) return;
    setOpeningError(null); mutation.mutate(state);
  };
  const openOriginal = (anchor: HTMLElement) => {
    try {
      openExternalUrl(anchor.ownerDocument.defaultView, item.url);
      if (item.readingState === "unread") mark("read");
    } catch (error) { setOpeningError(error instanceof Error ? error : new Error("Could not open the original.")); }
  };
  return { mark, openOriginal, isPending: pending, error: openingError ?? errors.at(-1) ?? null };
}
