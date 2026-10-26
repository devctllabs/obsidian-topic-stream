import { Plugin } from "obsidian";

import { TopicStreamSettingTab, DEFAULT_SETTINGS, type TopicStreamSettings } from "./settings";
import { TOPIC_STREAM_VIEW, FeedItemView } from "./view/FeedItemView";

export default class TopicStreamPlugin extends Plugin {
  public settings: TopicStreamSettings = DEFAULT_SETTINGS;

  public override async onload(): Promise<void> {
    this.settings = { ...DEFAULT_SETTINGS, ...await this.loadData() as Partial<TopicStreamSettings> };

    this.registerView(TOPIC_STREAM_VIEW, (leaf) => new FeedItemView(leaf, this.settings.serverUrl));
    this.addRibbonIcon("newspaper", "Open Topic Stream", () => void this.activateView());
    this.addCommand({
      id: "open-topic-stream",
      name: "Open feed",
      callback: () => void this.activateView()
    });
    this.addSettingTab(new TopicStreamSettingTab(this.app, this));
  }

  public override onunload(): void {
    this.app.workspace.detachLeavesOfType(TOPIC_STREAM_VIEW);
  }

  public async saveSettings(): Promise<void> {
    await this.saveData(this.settings);
  }

  private async activateView(): Promise<void> {
    let leaf = this.app.workspace.getLeavesOfType(TOPIC_STREAM_VIEW)[0];
    if (leaf === undefined) {
      leaf = this.app.workspace.getLeaf("tab");
      await leaf.setViewState({ type: TOPIC_STREAM_VIEW, active: true });
    }
    await this.app.workspace.revealLeaf(leaf);
  }
}
