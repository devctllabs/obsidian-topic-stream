import type { QueryClient } from "@tanstack/react-query";
import type { Schema } from "../../api/contracts";
import type { ViewEvents } from "../../shared/events";
import type { WorkspaceStatePort, ConnectionLocalState } from "../settings/device-state";
import type { DigestsService } from "./services";

type Baseline = NonNullable<ConnectionLocalState["digest"]["baseline"]>;
interface ReadinessSnapshot { unseen: string[]; error: Error | null; }
const boundary = (issue: Schema<"DigestIssueSummary">): Baseline => ({ generatedAt: issue.generatedAt, issueId: issue.id });
function isNew(issue: Schema<"DigestIssueSummary">, baseline: Baseline): boolean {
  return Date.parse(issue.generatedAt) > Date.parse(baseline.generatedAt) || (Date.parse(issue.generatedAt) === Date.parse(baseline.generatedAt) && issue.id > baseline.issueId);
}

/** Device-scoped observation, independent of edition content and server reading state. */
export class DigestReadiness {
  private snapshot: ReadinessSnapshot = { unseen: [], error: null };
  private baseline: Baseline | null = null;
  private listeners = new Set<() => void>();
  private closed = false;
  private reading: Promise<void> | null = null;
  private live = new Map<string, Promise<void>>();
  public constructor(private readonly client: DigestsService, private readonly storage: WorkspaceStatePort, private readonly queries: QueryClient, private readonly notify: (message: string) => void) {}
  public getSnapshot = (): ReadinessSnapshot => this.snapshot;
  public subscribe = (listener: () => void): (() => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  public connect(events?: ViewEvents): () => void {
    const unsubscribe = events?.subscribe((event) => {
      if (event.type === "digestReady") void this.receive(event.issueId).catch((error: unknown) => this.fail(error));
      if (event.type === "reconnected") void this.refresh();
    });
    const unsubscribeStorage = this.storage.subscribe(() => { void this.reconcileSeen().catch((error: unknown) => this.fail(error)); });
    void this.refresh();
    return () => { this.closed = true; unsubscribe?.(); unsubscribeStorage(); this.listeners.clear(); };
  }
  private async reconcileSeen(): Promise<void> {
    const local = await this.storage.read();
    this.update({ ...this.snapshot, unseen: this.snapshot.unseen.filter((id) => !local.digest.seenIssueIds.includes(id)) });
  }
  public refresh = (): Promise<void> => {
    if (this.closed) return Promise.resolve();
    this.reading ??= this.read().catch((error: unknown) => this.fail(error)).finally(() => { this.reading = null; });
    return this.reading;
  };
  private async read(): Promise<void> {
    await this.storage.read();
    const page = await this.client.listDigestIssues({ limit: 20 });
    if (this.closed) return;
    let established = false;
    await this.storage.update((value) => {
      if (value.digest.baseline) return;
      value.digest.baseline = page.items[0] ? boundary(page.items[0]) : { generatedAt: "0001-01-01T00:00:00Z", issueId: "" };
      established = true;
    });
    if (this.closed) return;
    this.baseline = (await this.storage.read()).digest.baseline;
    // The initial head defines all older history, including pages not downloaded on this device.
    if (established) { this.update({ ...this.snapshot, error: null }); return; }
    const discovered = await this.readSinceBaseline(page);
    if (this.closed) return;
    let unseen: string[] = [];
    await this.storage.update((value) => {
      unseen = [...new Set([...discovered, ...this.snapshot.unseen])].filter((id) => !value.digest.seenIssueIds.includes(id));
      // Catch-up suppresses later duplicates; an already observed live event keeps its Notice.
      value.digest.notifiedIssueIds = [...new Set([...value.digest.notifiedIssueIds, ...discovered.filter((id) => !this.live.has(id))])];
    });
    this.update({ unseen, error: null });
    void this.queries.invalidateQueries({ queryKey: ["digest-archive"] });
  }
  private async readSinceBaseline(first: Schema<"DigestIssueList">): Promise<string[]> {
    const ids: string[] = [];
    const cursors = new Set<string>();
    let page = first;
    while (!this.closed) {
      const recent = page.items.filter((issue) => isNew(issue, this.baseline!));
      ids.push(...recent.map((issue) => issue.id));
      if (recent.length < page.items.length || !page.nextCursor) break;
      if (cursors.has(page.nextCursor)) throw new Error("The Digest archive returned a repeated page cursor. Refresh to try again.");
      cursors.add(page.nextCursor);
      page = await this.client.listDigestIssues({ limit: 20, cursor: page.nextCursor });
    }
    return ids;
  }
  public open = async (issueId: string): Promise<void> => {
    if (this.closed) return;
    try {
      await this.storage.update((value) => { if (!value.digest.seenIssueIds.includes(issueId)) value.digest.seenIssueIds.push(issueId); });
      this.update({ ...this.snapshot, unseen: this.snapshot.unseen.filter((id) => id !== issueId) });
    } catch (error) { this.fail(error); }
  };
  private receive(id: string): Promise<void> {
    const existing = this.live.get(id);
    if (existing) return existing;
    const action = this.readLive(id).finally(() => { this.live.delete(id); });
    this.live.set(id, action); return action;
  }
  private async readLive(id: string): Promise<void> {
    if (!this.baseline) await this.refresh();
    if (this.closed || !this.baseline) return;
    const issue = await this.client.getDigestIssue(id);
    if (this.closed || issue.id !== id) return;
    let unseen = false;
    let notice = false;
    await this.storage.update((value) => {
      if (!value.digest.baseline || !isNew(issue, value.digest.baseline) || value.digest.seenIssueIds.includes(id)) return;
      unseen = true;
      if (!value.digest.notifiedIssueIds.includes(id)) { value.digest.notifiedIssueIds.push(id); notice = true; }
    });
    if (this.closed) return;
    if (unseen) this.update({ ...this.snapshot, unseen: [...new Set([...this.snapshot.unseen, id])] });
    if (notice) this.notify(`Digest ready: ${issue.title}`);
    void this.queries.invalidateQueries({ queryKey: ["digest-archive"] });
  }
  private fail(error: unknown): void { this.update({ ...this.snapshot, error: error instanceof Error ? error : new Error("Could not refresh new Digest Issues.") }); }
  private update(snapshot: ReadinessSnapshot): void {
    if (this.closed) return;
    this.snapshot = snapshot; for (const listener of this.listeners) listener();
  }
}
