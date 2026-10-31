import { ItemView, Notice, type WorkspaceLeaf } from "obsidian";
import { createRoot, type Root } from "react-dom/client";

import { ConnectedView } from "../features/connection/ConnectedView";
import { ViewConnection } from "../features/connection/session";
import type { SettingsStore } from "../features/settings/persistence";
import { obsidianHttp } from "../obsidian/services/http";
import { browserEventStream } from "../obsidian/services/event-stream";
import { obsidianMarkdown } from "../obsidian/services/markdown";
import { obsidianSnapshotVault } from "../obsidian/services/snapshots";

export const TOPIC_STREAM_VIEW = "topic-stream-view";

export class FeedItemView extends ItemView {
  private root: Root | null = null;
  private connection: ViewConnection | null = null;
  private unsubscribe: (() => void) | null = null;
  private releaseMigration: (() => void) | null = null;

  public constructor(leaf: WorkspaceLeaf, private readonly settings: SettingsStore) {
    super(leaf);
  }

  public getViewType(): string {
    return TOPIC_STREAM_VIEW;
  }

  public getDisplayText(): string {
    return "Topic Stream";
  }

  public getIcon(): string {
    return "newspaper";
  }

  public override async onOpen(): Promise<void> {
    this.dispose();
    this.contentEl.empty();
    const connection = new ViewConnection(this.settings.serverUrl, obsidianHttp, browserEventStream(() => this.contentEl.win));
    connection.getSnapshot().navigation.restore(this.settings.readConnection(this.settings.serverUrl, null).navigation);
    this.connection = connection;
    this.releaseMigration = this.contentEl.onWindowMigrated(() => connection.reconnectEvents());
    this.unsubscribe = this.settings.subscribe(() => connection.replaceServerUrl(this.settings.serverUrl));
    this.root = createRoot(this.contentEl);
    this.root.render(<ConnectedView connection={connection} settings={this.settings} notify={this.notify} onStateChange={this.persistNavigation} markdown={obsidianMarkdown(this.app, this)} vault={obsidianSnapshotVault(this.app.vault, this.app.fileManager)} />);
  }

  public override async onClose(): Promise<void> {
    this.dispose();
  }

  public dispose(): void {
    this.releaseMigration?.();
    this.releaseMigration = null;
    this.unsubscribe?.();
    this.unsubscribe = null;
    this.persistNavigation();
    this.root?.unmount();
    this.root = null;
    this.connection?.dispose();
    this.connection = null;
  }

  private notify = (message: string): void => { new Notice(message); };

  private persistNavigation = (): void => {
    const context = this.connection?.getSnapshot();
    if (!context) return;
    this.settings.saveConnection(context.serverUrl, null, {
      ...this.settings.readConnection(context.serverUrl, null), navigation: context.navigation.exportState()
    });
  };
}
