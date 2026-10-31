import { useCallback, useEffect, useRef, useState } from "react";
import type { Item, Schema } from "../../api/contracts";
import type { ItemSummaries } from "../reader/item-summaries";
import { SnapshotForm, type SnapshotPreferences } from "./SnapshotForm";
import type { SnapshotResult, SnapshotServices, SnapshotSource, SnapshotVault } from "./snapshot";
import { sanitizeSvg } from "./svg";

export interface SnapshotHost { vault: SnapshotVault; preferences: SnapshotPreferences; }
export type SaveIssue = (issue: Schema<"DigestIssue">, anchor: HTMLElement) => Promise<SnapshotResult | null>;
interface Session { source: SnapshotSource; readSource: () => SnapshotSource; resolve: (value: SnapshotResult | null) => void; }

export function useSnapshotWorkflow(host: SnapshotHost | undefined, images: SnapshotServices["images"], summaries: ItemSummaries) {
  const [session, setSession] = useState<Session | null>(null);
  const active = useRef<Session | null>(null);
  useEffect(() => () => { active.current?.resolve(null); active.current = null; }, []);
  const close = useCallback((result: SnapshotResult | null) => {
    const previous = active.current; active.current = null; setSession(null);
    previous?.resolve(result);
  }, []);
  const open = useCallback((source: SnapshotSource, readSource: () => SnapshotSource) => {
    if (!host || active.current) return Promise.resolve(null);
    return new Promise<SnapshotResult | null>((resolve) => { const next = { source, readSource, resolve }; active.current = next; setSession(next); });
  }, [host]);
  const annotation = useCallback((issueId: string, saved: Schema<"DigestItem">) => {
    const resource = summaries.getResource({ issueId, captured: saved });
    return resource?.status === "success" ? resource.data ?? null : saved.aiSummary;
  }, [summaries]);
  const saveItem = useCallback((item: Item) => {
    const read = (): SnapshotSource => ({ kind: "item", item, aiSummary: summaries.getSnapshot(item) });
    return open(read(), read);
  }, [summaries, open]);
  const saveCaptured = useCallback((issueId: string, saved: Schema<"DigestItem">) => {
    const read = (): SnapshotSource => ({ kind: "item", item: saved.item, aiSummary: annotation(issueId, saved) });
    return open(read(), read);
  }, [annotation, open]);
  const saveIssue = useCallback<SaveIssue>((issue) => {
    const read = (): SnapshotSource => ({ kind: "digest_issue", issue: { ...issue, sections: issue.sections.map((section) => ({ ...section, items: section.items.map((saved) => ({ ...saved, aiSummary: annotation(issue.id, saved) })) })) } });
    return open(read(), read);
  }, [annotation, open]);
  return { enabled: Boolean(host), saveItem, saveCaptured, saveIssue, dialog: session && host ? <SnapshotForm source={session.source} readSource={session.readSource} services={{ vault: host.vault, images, sanitizeSvg }} preferences={host.preferences} onSaved={close} onCancel={() => close(null)} /> : null };
}
