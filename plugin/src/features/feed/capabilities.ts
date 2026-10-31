import type { ReactNode } from "react";
import type { Item, Schema } from "../../api/contracts";

/** Feed owns invocation; Reader (07) and Vault (09) supply the concrete capabilities. */
export interface FeedItemCapabilities {
  renderReader?: (item: Item) => ReactNode;
  saveToVault?: (item: Item, anchor: HTMLElement) => Promise<{ path: string; localImages?: number; linkedImages?: number } | null>;
  summary?: ItemSummaryCapability;
}

/** UI 07 owns the shared state, work and body renderer; Feed owns placement and invocation. */
export interface ItemSummaryCapability {
  subscribe: (item: Item, listener: () => void) => () => void;
  getSnapshot: (item: Item) => Schema<"ItemSummary"> | null;
  request: (item: Item) => Promise<void>;
  render: (item: Item, expanded: boolean) => ReactNode;
}
