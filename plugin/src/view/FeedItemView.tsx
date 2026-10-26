import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ItemView, type WorkspaceLeaf } from "obsidian";
import { createRoot, type Root } from "react-dom/client";

import { TopicStreamClient } from "../api/client";
import { TopicStreamView } from "./TopicStreamView";

export const TOPIC_STREAM_VIEW = "topic-stream-view";

export class FeedItemView extends ItemView {
  private root: Root | null = null;
  private readonly queryClient = new QueryClient({
    defaultOptions: { queries: { retry: 1, staleTime: 30_000 } }
  });

  public constructor(leaf: WorkspaceLeaf, private readonly serverUrl: string) {
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
    const container = this.containerEl.children[1];
    if (!(container instanceof HTMLElement)) {
      return;
    }
    container.empty();
    this.root = createRoot(container);
    this.root.render(
      <QueryClientProvider client={this.queryClient}>
        <TopicStreamView client={new TopicStreamClient(this.serverUrl)} />
      </QueryClientProvider>
    );
  }

  public override async onClose(): Promise<void> {
    this.root?.unmount();
    this.root = null;
    this.queryClient.clear();
  }
}
