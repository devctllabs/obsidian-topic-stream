import { useLayoutEffect, useRef, useState } from "react";
import type { Schema } from "../../api/contracts";
import type { FeedService } from "../feed/services";
import { useFeedItemActions, type ItemActionContext } from "../feed/item-actions";
import { FeedbackMenu, FeedbackStatus, useItemFeedback } from "../feed/item-feedback";
import { ItemDetails } from "../feed/FeedCard";
import { useItemReading } from "../feed/item-reading";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import type { CapturedCapabilities } from "./DigestReader";
import { IssueSnapshotAction } from "../snapshots/IssueSnapshotAction";
import type { SaveIssue } from "../snapshots/use-snapshot-workflow";

export interface EditionSelection { section: number; itemId: string; }
export function editionItemKey(value: EditionSelection): string { return `${value.section}:${value.itemId}`; }
interface EditionProps { issue: Schema<"DigestIssue">; client: FeedService; onOpen: (value: EditionSelection) => void; onReadingState: ItemActionContext["onReadingState"]; returnItem: EditionSelection | null; capabilitiesFor: CapturedCapabilities; saveIssue?: SaveIssue | undefined; }

export function DigestEdition({ issue, client, onOpen, onReadingState, returnItem, capabilitiesFor, saveIssue }: EditionProps) {
  const article = useRef<HTMLElement>(null);
  useLayoutEffect(() => {
    if (returnItem) Array.from(article.current?.querySelectorAll<HTMLButtonElement>("[data-edition-item]") ?? []).find((element) => element.dataset.editionItem === editionItemKey(returnItem))?.focus({ preventScroll: true });
  }, [returnItem]);
  return <article ref={article} className="topic-stream-edition">
    <p className="topic-stream-metadata"><time dateTime={issue.generatedAt}>{new Date(issue.generatedAt).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" })}</time>{issue.status === "degraded" && " · Partial issue"}</p>
    <div className="topic-stream-edition-title"><h1>{issue.title}</h1>{saveIssue && <IssueSnapshotAction key={issue.id} issue={issue} save={saveIssue} />}</div>
    <nav className="topic-stream-edition-index" aria-label="Issue sections">{issue.sections.map((section, index) => <a key={`${section.topicId}:${index}`} href={`#edition-section-${index}`} onClick={(event) => { event.preventDefault(); article.current?.querySelector(`#edition-section-${index}`)?.scrollIntoView({ block: "start" }); }}>{section.topicName}</a>)}</nav>
    {issue.sections.map((section, sectionIndex) => <section key={`${section.topicId}:${sectionIndex}`} id={`edition-section-${sectionIndex}`} className="topic-stream-edition-section" aria-label={section.topicName}>
      <h2 className="topic-stream-edition-topic">{section.topicName}</h2>
      {section.failure && <p className="topic-stream-metadata">This section is incomplete: {section.failure}</p>}
      {section.items.map((saved) => <EditionItem key={saved.item.id} saved={saved} issueId={issue.id} topicId={section.topicId} capabilitiesFor={capabilitiesFor} selection={{ section: sectionIndex, itemId: saved.item.id }} client={client} onOpen={onOpen} onReadingState={onReadingState} />)}
      {section.items.length === 0 && <p className="topic-stream-metadata">No Items were captured in this section.</p>}
    </section>)}
  </article>;
}

function EditionItem({ saved, selection, client, onOpen, onReadingState, issueId, topicId, capabilitiesFor }: { saved: Schema<"DigestItem">; selection: EditionSelection; client: FeedService; onOpen: EditionProps["onOpen"]; onReadingState: EditionProps["onReadingState"]; issueId: string; topicId: string; capabilitiesFor: CapturedCapabilities }) {
  const [details, setDetails] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const feedback = useItemFeedback(client, saved.item);
  const actions = useFeedItemActions({ client, item: saved.item, topicId, onReadingState, onDetails: () => setDetails((value) => !value), onResult: setMessage, capabilities: capabilitiesFor(issueId, saved), summaryHeadingLevel: 4 });
  return <article className="topic-stream-edition-item"><div className="topic-stream-edition-item-header"><h3><button type="button" data-edition-item={editionItemKey(selection)} onClick={() => { if (saved.item.readingState === "unread") actions.reading.mark("read"); onOpen(selection); }}>{saved.item.title}</button></h3><FeedbackMenu title={saved.item.title} control={feedback} items={actions.items} disabled={actions.pending} /></div>
    <p>{saved.summary || saved.item.excerpt || "No excerpt was captured for this Item."}</p>
    <FeedbackStatus control={feedback} />{message && <p role="status" className="topic-stream-metadata">{message}</p>}{actions.status}{actions.summary}
    {details && <ItemDetails entry={{ item: saved.item, explanation: saved.explanation }} headingLevel={4} />}
  </article>;
}

export function DigestContents({ issue, selected, onOpen, client, onReadingState }: { issue: Schema<"DigestIssue">; selected: EditionSelection; onOpen: EditionProps["onOpen"]; client: FeedService; onReadingState: EditionProps["onReadingState"] }) {
  return <><div className="topic-stream-section-header"><h2>Issue contents</h2></div><nav className="topic-stream-issue-contents" aria-label="Issue contents">{issue.sections.map((section, index) => <section key={`${section.topicId}:${index}`}><h3>{section.topicName}</h3>{section.items.map(({ item }) => <ContentsItem key={item.id} item={item} current={selected.section === index && selected.itemId === item.id} section={index} client={client} onOpen={onOpen} onReadingState={onReadingState} />)}</section>)}</nav></>;
}
function ContentsItem({ item, current, section, client, onOpen, onReadingState }: { item: Schema<"Item">; current: boolean; section: number; client: FeedService; onOpen: EditionProps["onOpen"]; onReadingState: EditionProps["onReadingState"] }) {
  const reading = useItemReading(client, item, onReadingState);
  return <><button type="button" className="topic-stream-contents-item" data-item-id={`${section}:${item.id}`} aria-current={current} onClick={() => { if (item.readingState === "unread") reading.mark("read"); onOpen({ section, itemId: item.id }); }}>{item.title}</button>{reading.error && <ApiErrorMessage title="Could not change reading state" error={reading.error} />}</>;
}
