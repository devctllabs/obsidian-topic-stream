import { App, PluginSettingTab, Setting, type ButtonComponent } from "obsidian";
import { DEFAULT_SERVER_URL } from "./features/settings/device-state";
import { TopicStreamClient } from "./api/client";
import { normalizeServerUrl } from "./api/url";
import { obsidianHttp } from "./obsidian/services/http";
import type TopicStreamPlugin from "./main";

export class TopicStreamSettingTab extends PluginSettingTab {
  private test: AbortController | null = null;
  public constructor(app: App, private readonly plugin: TopicStreamPlugin) { super(app, plugin); }

  public display(): void {
    this.stopTest();
    this.containerEl.empty();
    let draft = this.plugin.settings.serverUrl;
    const status = this.containerEl.createEl("p", { attr: { role: "status", "aria-live": "polite" } });
    new Setting(this.containerEl)
      .setName("Server URL")
      .setDesc("Device-local address. Apply to replace the connection in open views.")
      .addText((text) => text.setPlaceholder(DEFAULT_SERVER_URL).setValue(draft).onChange((value) => { draft = value; this.stopTest(); status.setText(""); }))
      .addButton((button) => button.setButtonText("Test connection").onClick(() => { void this.testConnection(draft, status, button); }))
      .addButton((button) => button.setButtonText("Apply").setCta().onClick(() => {
        this.stopTest();
        try {
          this.plugin.settings.applyServerUrl(draft);
          status.setText("Connection address applied.");
        } catch (error) {
          status.setText(error instanceof Error ? error.message : "Could not save the connection address.");
        }
      }));
    this.clientPreferences(status);
  }
  public override hide(): void { this.stopTest(); super.hide(); }
  private stopTest(): void { this.test?.abort(); this.test = null; }
  private async testConnection(draft: string, status: HTMLElement, button: ButtonComponent): Promise<void> {
    this.stopTest();
    const controller = new AbortController();
    this.test = controller;
    button.setDisabled(true); status.setText("Testing connection…");
    try {
      const client = new TopicStreamClient(normalizeServerUrl(draft), obsidianHttp, controller.signal);
      const value = await client.getSystemStatus();
      if (!controller.signal.aborted) status.setText(`Connected · Topic Stream ${value.version}`);
    } catch (error) {
      if (!controller.signal.aborted) status.setText(error instanceof Error ? error.message : "Could not test connection.");
    } finally {
      button.setDisabled(false);
      if (this.test === controller) this.test = null;
    }
  }
  private clientPreferences(status: HTMLElement): void {
    const settings = this.plugin.settings;
    preferenceSelect(this.containerEl, {
      name: "Default feed filter", description: "Initial reading filter for this vault.", options: { all: "All", unread: "Unread" }, value: () => settings.preferences.feedFilter,
      save: (value) => settings.savePreferences((current) => ({ ...current, feedFilter: value as "all" | "unread" }))
    }, status);
    preferenceSelect(this.containerEl, {
      name: "Snapshot content", description: "Default for future saves to the vault.", options: { compact: "Compact", full: "Full content" }, value: () => settings.preferences.snapshot.content,
      save: (value) => settings.savePreferences((current) => ({ ...current, snapshot: { ...current.snapshot, content: value as "compact" | "full" } }))
    }, status);
    preferenceSelect(this.containerEl, {
      name: "Snapshot images", description: "Image fetching still requires confirmation when saving a Snapshot.", options: { links: "Links only", remote: "Remote images", local: "Local copies" }, value: () => settings.preferences.snapshot.images,
      save: (value) => settings.savePreferences((current) => ({ ...current, snapshot: { ...current.snapshot, images: value as "links" | "remote" | "local" } }))
    }, status);
  }
}
function preferenceSelect(container: HTMLElement, config: { name: string; description: string; options: Record<string, string>; value: () => string; save: (value: string) => Promise<void> }, status: HTMLElement): void {
  new Setting(container).setName(config.name).setDesc(config.description).addDropdown((dropdown) => dropdown.addOptions(config.options).setValue(config.value()).onChange((value) => {
    dropdown.setDisabled(true);
    // Obsidian components are thenable; Promise callbacks must return void.
    void config.save(value).then(() => { status.setText("Preference saved."); }, () => { dropdown.setValue(config.value()); status.setText("Could not save preference. Your previous value is kept."); }).finally(() => { dropdown.setDisabled(false); });
  }));
}
