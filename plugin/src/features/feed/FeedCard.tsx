import type { FeedService } from "./services";
import type { FeedEntry } from "../../api/contracts";
import { FeedbackMenu, FeedbackStatus, useItemFeedback } from "./item-feedback";
import { externalUrl } from "../../obsidian/services/external-links";
import type { ReactNode } from "react";
import type { ActionMenuItem } from "../../shared/components/ActionMenu";
import { useFeedItemActions, type ItemActionContext, type ItemActionResult } from "./item-actions";
export type ItemPresentation = Omit<FeedEntry, "score"> & { score?: number };

export function ConnectedItemCard({ entry, details, actionResult, ...context }: ItemActionContext & { entry: ItemPresentation; details: boolean; actionResult?: ItemActionResult | null }) {
  const actions = useFeedItemActions({ ...context, item: entry.item });
  const content = details ? undefined : context.capabilities?.renderReader?.(entry.item);
  const notice = actionResult?.itemId === entry.item.id ? <p role="status" className="topic-stream-metadata">{actionResult.message}</p> : null;
  return <FeedCard client={context.client} entry={entry} headingLevel={1} details={details} onOpenOriginal={actions.reading.openOriginal} extraMenuItems={details ? actions.items : actions.items.filter((action) => action.id !== "summary")} actionsPending={actions.pending} actionStatus={<>{notice}{actions.status}</>} content={content} annotation={<>{!details && actions.summaryTrigger}{actions.summary}</>} />;
}

export function FeedCard({ client, entry, headingLevel = 2, details = false, onOpenOriginal, extraMenuItems = [], actionsPending = false, actionStatus, content, annotation }: { client: Pick<FeedService, "setFeedback">; entry: ItemPresentation; headingLevel?: 1 | 2; details?: boolean; onOpenOriginal?: (anchor: HTMLAnchorElement) => void; extraMenuItems?: ActionMenuItem[]; actionsPending?: boolean; actionStatus?: ReactNode; content?: ReactNode; annotation?: ReactNode }) {
  const Heading = `h${headingLevel}` as const;
  const feedback = useItemFeedback(client, entry.item);
  const date = entry.item.publishedAt === null
    ? "Recently discovered"
    : new Intl.DateTimeFormat(undefined, { dateStyle: "medium" }).format(new Date(entry.item.publishedAt));
  return <article className="topic-stream-card">
    <div className="topic-stream-card-header"><div className="topic-stream-card-meta"><span>{date}</span><span>{entry.explanation.summary}</span></div>
      <FeedbackMenu title={entry.item.title} control={feedback} items={extraMenuItems} disabled={actionsPending} /></div>
    <Heading><a href={externalUrl(entry.item.url)} onClick={(event) => { if (onOpenOriginal) { event.preventDefault(); onOpenOriginal(event.currentTarget); } }}>{entry.item.title}</a></Heading>
    <FeedbackStatus control={feedback} />
    {actionStatus}
    {annotation}
    {content === undefined ? <p>{entry.item.excerpt}</p> : content}
    {details && <ItemDetails entry={entry} />}
  </article>;
}

export function ItemDetails({ entry: { item, score, explanation }, headingLevel = 2 }: { entry: ItemPresentation; headingLevel?: 2 | 4 }) {
  const Heading = `h${headingLevel}` as const;
  return <><section className="topic-stream-item-details" aria-label="Why this item"><Heading>Why this item</Heading><p>{explanation.summary}</p>
    <dl>{score !== undefined && <><dt>Recommendation score</dt><dd>{score}</dd></>}
      {Object.entries(explanation.signals).map(([name, value]) => <div key={name}><dt>{name}</dt><dd>{value}</dd></div>)}
      <dt>Pipeline version</dt><dd>{explanation.pipelineVersion}</dd></dl></section>
    <section className="topic-stream-item-details" aria-label="Item details"><Heading>Item details</Heading><dl>
      <dt>Kind</dt><dd>{item.kind}</dd><dt>Authors</dt><dd>{item.authors.join(", ") || "Not provided"}</dd>
      <dt>Discovered</dt><dd><time dateTime={item.discoveredAt}>{new Intl.DateTimeFormat(undefined, { dateStyle: "medium" }).format(new Date(item.discoveredAt))}</time></dd>
      <dt>Canonical URL</dt><dd>{item.canonicalUrl}</dd></dl></section></>;
}
