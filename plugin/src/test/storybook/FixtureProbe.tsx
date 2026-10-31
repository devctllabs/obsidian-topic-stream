import { useEffect, useId, useRef, useState } from "react";

import { FixtureScenario } from "./scenario";
import { fixtureId, makeCollections, makeIssue } from "./fixtures";

export interface FixtureProbeProps {
  mode?: "feed" | "reader" | "digest" | "contents";
  state?: "loaded" | "loading" | "error";
  itemCount?: number;
  longTitle?: boolean;
  disabled?: boolean;
}

/** Harness contract probe only; actual product screens belong to their feature owners. */
export function FixtureProbe({ mode = "feed", state = "loaded", itemCount = 30, longTitle = false, disabled = false }: FixtureProbeProps) {
  const [scenario] = useState(() => new FixtureScenario());
  const [open, setOpen] = useState(false);
  const collections = makeCollections({ sources: 20, topics: 10, items: itemCount });
  const issue = makeIssue(collections.entries);
  const heading = fixtureHeading(mode, longTitle, collections, issue);

  return <section className="topic-stream-harness" aria-label="Harness contract fixture">
    <h1 data-type-role="edition-title">{heading}</h1>
    <FixtureBody mode={mode} state={state} collections={collections} issue={issue} onOpen={() => setOpen(true)} />
    <details className="topic-stream-harness-tools">
      <summary>Test controls</summary>
      <FixtureControls scenario={scenario} disabled={disabled} onOpen={() => setOpen(true)} />
    </details>
    {open && <FixtureDialog onClose={() => setOpen(false)} />}
  </section>;
}

function FixtureDialog({ onClose }: { onClose: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  useEffect(() => { dialog.current?.showModal(); }, []);
  return <dialog ref={dialog} aria-labelledby={titleId} onClose={onClose} onKeyDown={(event) => {
    if (event.key === "Escape") { event.preventDefault(); dialog.current?.close(); }
  }}>
    <h2 id={titleId}>Fixture details</h2>
    <p>Checks keyboard, Escape, touch targets and focus restoration in the host frame.</p>
    <button type="button" onClick={() => dialog.current?.close()}>Back to fixture</button>
  </dialog>;
}

function fixtureHeading(mode: NonNullable<FixtureProbeProps["mode"]>, longTitle: boolean, collections: ReturnType<typeof makeCollections>, issue: ReturnType<typeof makeIssue>): string {
  if (longTitle) return "Engineering notes · Sistemas de búsqueda y contexto de lectura ".repeat(3);
  if (mode === "reader") return collections.entries[0]?.item.title ?? "Empty Reader fixture";
  if (mode === "digest" || mode === "contents") return issue.title;
  return "Reading workspace fixture";
}

function FixtureControls({ scenario, disabled, onOpen }: { scenario: FixtureScenario; disabled: boolean; onOpen: () => void }) {
  const [action, setAction] = useState("Not requested");
  const [events, setEvents] = useState(0);
  useEffect(() => scenario.events.subscribe(() => setEvents((count) => count + 1)), [scenario]);
  const pending = scenario.digestBuild.calls.at(-1)?.response;
  const request = () => {
    setAction("Pending");
    void scenario.digestBuild.invoke({ digestId: fixtureId(501), requestId: "fixture-request" }).then(
      () => setAction("Ready"), () => setAction("Failed — retry is available")
    );
  };
  return <>
    <div className="topic-stream-harness-toolbar">
      <button type="button" disabled={disabled} onClick={onOpen}>Open fixture details</button>
      <button type="button" disabled={disabled || action === "Pending"} onClick={request}>Request fixture work</button>
      <button type="button" disabled={action !== "Pending"} onClick={() => pending?.resolve({ jobId: fixtureId(502) })}>Complete fixture work</button>
      <button type="button" disabled={action !== "Pending"} onClick={() => pending?.reject(new Error("Synthetic failure"))}>Fail fixture work</button>
      <button type="button" onClick={() => scenario.events.emit({ type: "jobChanged", jobId: fixtureId(502) })}>Emit fixture advisory</button>
    </div>
    <p role="status" className="topic-stream-harness-status">{action} · {events} advisories</p>
  </>;
}

function FixtureBody({ mode, state, collections, issue, onOpen }: {
  mode: NonNullable<FixtureProbeProps["mode"]>; state: NonNullable<FixtureProbeProps["state"]>;
  collections: ReturnType<typeof makeCollections>; issue: ReturnType<typeof makeIssue>; onOpen: () => void;
}) {
  if (state === "loading") return <p role="status">Loading fixture content…</p>;
  if (state === "error") return <p role="alert">Fixture content could not be loaded. Existing controls remain available.</p>;
  if (collections.entries.length === 0) return <p>No Items in this fixture.</p>;
  return <div className="topic-stream-harness-grid">
      <nav aria-label={mode === "digest" || mode === "contents" ? "Issue contents" : "Fixture list"}>
        <ul className="topic-stream-harness-list">{collections.entries.map(({ item }) => <li key={item.id}>
          <button type="button" onClick={() => onOpen()}>{item.title}</button>
        </li>)}</ul>
      </nav>
      <article aria-label="Fixture reading pane">
        {issue.sections.map((section) => <section key={section.topicId}>
          <h2 data-type-role="topic-label">{section.topicName}</h2>
          {section.items.map(({ item }) => <article className="topic-stream-harness-item" key={item.id}>
            <h3 data-type-role="item-title">{item.title}</h3>
            <p className="topic-stream-harness-meta" data-type-role="metadata">Example Author · 27 October 2026</p>
            <p>{mode === "reader" ? item.contentMarkdown : item.excerpt}</p>
          </article>)}
        </section>)}
      </article>
    </div>;
}
