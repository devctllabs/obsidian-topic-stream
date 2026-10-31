import { Plugin } from "obsidian";

import { SettingsStore } from "./features/settings/persistence";
import { TopicStreamSettingTab } from "./settings";
import { TOPIC_STREAM_VIEW, FeedItemView } from "./view/FeedItemView";

export default class TopicStreamPlugin extends Plugin {
  public settings!: SettingsStore;

  public override async onload(): Promise<void> {
    this.settings = await SettingsStore.load({
      loadPreferences: () => this.loadData() as Promise<unknown>,
      savePreferences: (value) => this.saveData(value),
      loadDevice: () => this.app.loadLocalStorage("topic-stream:device:v1") as unknown,
      saveDevice: (value) => this.app.saveLocalStorage("topic-stream:device:v1", value)
    });

    this.registerView(TOPIC_STREAM_VIEW, (leaf) => new FeedItemView(leaf, this.settings));
    this.addRibbonIcon("newspaper", "Open Topic Stream", () => void this.activateView());
    this.addCommand({
      id: "open-topic-stream",
      name: "Open feed",
      callback: () => void this.activateView()
    });
    this.addSettingTab(new TopicStreamSettingTab(this.app, this));
  }

  public override onunload(): void {
    for (const leaf of this.app.workspace.getLeavesOfType(TOPIC_STREAM_VIEW)) {
      if (leaf.view instanceof FeedItemView) leaf.view.dispose();
    }
    this.settings.dispose();
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
