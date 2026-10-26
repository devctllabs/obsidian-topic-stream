import { App, PluginSettingTab, Setting } from "obsidian";

import type TopicStreamPlugin from "./main";

export interface TopicStreamSettings {
  serverUrl: string;
}

export const DEFAULT_SETTINGS: TopicStreamSettings = {
  serverUrl: "http://127.0.0.1:7331"
};

export class TopicStreamSettingTab extends PluginSettingTab {
  public constructor(app: App, private readonly plugin: TopicStreamPlugin) {
    super(app, plugin);
  }

  public display(): void {
    this.containerEl.empty();
    new Setting(this.containerEl)
      .setName("Server URL")
      .setDesc("Address of the local Topic Stream daemon")
      .addText((text) => text
        .setPlaceholder(DEFAULT_SETTINGS.serverUrl)
        .setValue(this.plugin.settings.serverUrl)
        .onChange(async (value) => {
          this.plugin.settings.serverUrl = value.trim() || DEFAULT_SETTINGS.serverUrl;
          await this.plugin.saveSettings();
        }));
  }
}
