import { QueryClientProvider } from "@tanstack/react-query";
import { useEffect, useMemo, useSyncExternalStore } from "react";

import { ConnectedShell } from "../shell/ConnectedShell";
import { ViewErrorBoundary } from "../shell/ViewErrorBoundary";
import type { ViewConnection } from "./session";

import type { SettingsStore } from "../settings/persistence";
import { WorkspaceState } from "./workspace-state";
import type { FeedPreferences } from "../feed/preferences";
import type { MarkdownRendererPort } from "../reader/markdown";
import type { SnapshotVault } from "../snapshots/snapshot";
import type { SnapshotHost } from "../snapshots/use-snapshot-workflow";

export function ConnectedView({ connection, settings, notify, onStateChange, markdown, vault }: { connection: ViewConnection; settings: SettingsStore; notify: (message: string) => void; onStateChange?: () => void; markdown: MarkdownRendererPort; vault: SnapshotVault }) {
  const context = useSyncExternalStore(connection.subscribe, connection.getSnapshot);
  const localState = useMemo(() => new WorkspaceState(settings, context.serverUrl, context.client), [settings, context]);
  const feedPreferences = useMemo<FeedPreferences>(() => ({
    getSnapshot: () => settings.preferences.feedFilter,
    subscribe: (listener) => settings.subscribe(listener),
    save: (feedFilter) => settings.savePreferences((current) => ({ ...current, feedFilter }))
  }), [settings]);
  const snapshots = useMemo<SnapshotHost>(() => ({ vault, preferences: { read: () => settings.preferences.snapshot, save: (snapshot) => settings.savePreferences((current) => ({ ...current, snapshot })) } }), [vault, settings]);
  useEffect(() => context.navigation.subscribe(() => onStateChange?.()), [context, onStateChange]);
  return <QueryClientProvider key={context.id} client={context.queryClient}>
    <ViewErrorBoundary><ConnectedShell client={context.client} navigation={context.navigation} localState={localState} notify={notify} feedPreferences={feedPreferences} events={context.events} markdown={markdown} snapshots={snapshots} /></ViewErrorBoundary>
  </QueryClientProvider>;
}
