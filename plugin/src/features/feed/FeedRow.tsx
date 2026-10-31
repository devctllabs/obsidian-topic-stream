import type { FeedEntry } from "../../api/contracts";
import { FeedbackMenu, FeedbackStatus, useItemFeedback } from "./item-feedback";
import { useFeedItemActions, type ItemActionContext } from "./item-actions";

export function FeedRow({ entry, selected, onOpen, ...context }: ItemActionContext & {
  entry: FeedEntry; selected: boolean; onOpen: () => void;
}) {
  const feedback = useItemFeedback(context.client, entry.item);
  const actions = useFeedItemActions({ ...context, item: entry.item });
  const date = entry.item.publishedAt ?? entry.item.discoveredAt;
  const openReading = () => {
    if (entry.item.readingState === "unread") actions.reading.mark("read");
    onOpen();
  };
  return <li className="topic-stream-feed-row"><button type="button" className="topic-stream-row" data-item-id={entry.item.id} data-reading-state={entry.item.readingState} aria-current={selected} onClick={openReading}>
    <span className="topic-stream-row-title">{entry.item.title}</span><span className="topic-stream-row-excerpt">{entry.item.excerpt}</span>
    <span className="topic-stream-row-meta"><span>{kindLabels[entry.item.kind]}</span><time dateTime={date}>{new Intl.DateTimeFormat(undefined, { dateStyle: "medium" }).format(new Date(date))}</time>{entry.item.visibility === "hidden" && <span>Hidden</span>}</span>
  </button><div className="topic-stream-feed-row-actions"><FeedbackMenu title={entry.item.title} control={feedback} disabled={actions.pending} items={actions.items} /></div>
    <FeedbackStatus control={feedback} showRating={false} />{actions.summary}{actions.status}
  </li>;
}

const kindLabels = { article: "Article", paper: "Paper", video: "Video", repository: "Repository" } as const;
