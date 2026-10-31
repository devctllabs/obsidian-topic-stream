import { skipToken, useIsMutating, useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useState } from "react";
import type { Feedback, FeedPage, Item } from "../../api/contracts";
import { ApiError } from "../../api/errors";
import { ActionMenu, type ActionMenuItem } from "../../shared/components/ActionMenu";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import { FeedbackNote } from "./FeedbackNote";
import { patchItemState } from "./cache";
import type { FeedService } from "./services";

type FeedbackValue = Pick<Feedback, "sentiment" | "comment">;
interface FeedbackState { saved: FeedbackValue | null; draft: string; draftDirty: boolean; projectionRevision: string | null; }
interface FeedbackRequest extends FeedbackValue { kind: "rating" | "note"; }
interface NoteEditor { anchor: HTMLButtonElement; sentiment: Feedback["sentiment"]; }

export function reconcilePageFeedback(queries: QueryClient, pages: FeedPage[]): void {
  for (const page of pages) for (const { item } of page.items) reconcileItemFeedback(queries, item);
}
function reconcileItemFeedback(queries: QueryClient, item: Item): void {
  if (queries.isMutating({ mutationKey: ["item-feedback-write", item.id] })) return;
  queries.setQueryData<FeedbackState>(["item-feedback", item.id], (previous) => !previous || previous.projectionRevision ? previous : { ...previous, saved: item.feedback, draft: previous.draftDirty ? previous.draft : item.feedback?.comment ?? "" });
}

/** One opened view owns committed intent and the Item's unsent draft across its surfaces. */
export function useItemFeedback(client: Pick<FeedService, "setFeedback">, item: Item) {
  const queries = useQueryClient();
  const key = ["item-feedback", item.id];
  const initial: FeedbackState = { saved: item.feedback, draft: item.feedback?.comment ?? "", draftDirty: false, projectionRevision: null };
  const { data: state = initial } = useQuery<FeedbackState>({ queryKey: key, queryFn: skipToken, gcTime: Infinity, initialData: initial });
  const [editor, setEditor] = useState<NoteEditor | null>(null);
  const closeNote = (restore: boolean) => { if (restore) editor?.anchor.focus({ preventScroll: true }); setEditor(null); };
  const feedback = useMutation({ mutationKey: ["item-feedback-write", item.id], retry: false,
    mutationFn: async (request: FeedbackRequest) => {
      try {
        const value = await client.setFeedback(item.id, request.sentiment, request.comment);
        return { saved: value, value, projectionRevision: null as string | null };
      } catch (error) {
        if (!(error instanceof ApiError) || !error.canonicalCommitted) throw error;
        return { saved: { sentiment: request.sentiment, comment: request.comment }, value: null, projectionRevision: error.projectionRevision };
      }
    },
    onSuccess: (value, request) => {
      queries.setQueryData<FeedbackState>(key, (previous) => ({ ...previous!, saved: value.saved, projectionRevision: value.projectionRevision, draftDirty: request.kind === "note" ? false : previous!.draftDirty }));
      if (value.value) patchItemState(queries, item.id, { feedback: value.value });
      if (request.kind === "note") closeNote(true);
    }
  });
  const pending = useIsMutating({ mutationKey: ["item-feedback-write", item.id] }) > 0;
  const choose = (sentiment: Feedback["sentiment"], anchor: HTMLButtonElement) => {
    if (queries.isMutating({ mutationKey: ["item-feedback-write", item.id] })) return;
    setEditor({ sentiment, anchor });
    feedback.mutate({ sentiment, comment: state.saved?.comment ?? null, kind: "rating" });
  };
  const openNote = (anchor: HTMLButtonElement) => { if (state.saved) setEditor({ anchor, sentiment: state.saved.sentiment }); };
  const setComment = (draft: string) => queries.setQueryData<FeedbackState>(key, (previous) => ({ ...previous!, draft, draftDirty: true }));
  const sendNote = () => { if (editor && !pending) feedback.mutate({ sentiment: editor.sentiment, comment: state.draft.trim(), kind: "note" }); };
  return { feedback, currentFeedback: state.saved, comment: state.draft, projectionRevision: state.projectionRevision, pending, editor, choose, openNote, closeNote, setComment, sendNote };
}
type FeedbackControl = ReturnType<typeof useItemFeedback>;

export function FeedbackMenu({ title, control, items = [], disabled = false }: { title: string; control: FeedbackControl; items?: ActionMenuItem[]; disabled?: boolean }) {
  const { currentFeedback, pending } = control;
  return <><ActionMenu label={`More actions for ${title}`} menuLabel={`Actions for ${title}`} disabled={disabled || pending} items={[
    { id: "like", label: "Useful", checked: currentFeedback?.sentiment === "like", onSelect: (anchor) => control.choose("like", anchor) },
    { id: "dislike", label: "Not for me", checked: currentFeedback?.sentiment === "dislike", onSelect: (anchor) => control.choose("dislike", anchor) },
    ...(currentFeedback ? [{ id: "note", label: currentFeedback.comment ? "Edit note" : "Add note", separator: true, onSelect: control.openNote }] : []),
    ...items
  ]} /><ItemFeedbackNote control={control} items={items} /></>;
}

function ItemFeedbackNote({ control, items }: { control: FeedbackControl; items: ActionMenuItem[] }) {
  const { editor, feedback } = control;
  if (!editor) return null;
  const correction = items.find((item) => item.id === "wrong-topic");
  const topicCorrection = editor.sentiment === "dislike" && correction ? { label: correction.label, onSelect: () => { control.closeNote(false); correction.onSelect(editor.anchor); } } : undefined;
  return <FeedbackNote anchor={editor.anchor} sentiment={editor.sentiment} comment={control.comment} pending={control.pending} sendingNote={feedback.variables?.kind === "note"} error={feedback.error}
    onChange={control.setComment} onSend={control.sendNote} onClose={control.closeNote} topicCorrection={topicCorrection} />;
}

export function FeedbackStatus({ control, showRating = true }: { control: FeedbackControl; showRating?: boolean }) {
  const { currentFeedback, projectionRevision, editor, feedback, pending } = control;
  return <><FeedbackRating value={showRating ? currentFeedback : null} />
    {projectionRevision && <p role="status" className="topic-stream-metadata">Saved. Retrieval update pending.</p>}
    {!editor && <>{pending && <p role="status" className="topic-stream-metadata">Saving feedback…</p>}
      {feedback.isSuccess && feedback.variables.kind === "note" && <p role="status" className="topic-stream-metadata">Note sent</p>}
      {feedback.error && <ApiErrorMessage title="Could not save feedback" error={feedback.error} />}</>}
  </>;
}

function FeedbackRating({ value }: { value: FeedbackValue | null }) {
  return value ? <p className="topic-stream-metadata">Feedback: {value.sentiment === "like" ? "Useful" : "Not for me"}</p> : null;
}
