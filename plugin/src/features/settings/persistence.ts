import { normalizeServerUrl } from "../../api/url";
import { connectionScope, normalizeConnectionState, normalizeDeviceState, type ConnectionLocalState, type DeviceState } from "./device-state";
import { normalizePreferences, type ClientPreferences } from "./preferences";

export interface SettingsStorage {
  loadPreferences(): Promise<unknown>;
  savePreferences(value: ClientPreferences): Promise<void>;
  loadDevice(): unknown;
  saveDevice(value: DeviceState): void;
}

export class SettingsStore {
  private pendingSave = Promise.resolve();
  private readonly listeners = new Set<() => void>();

  private constructor(private readonly storage: SettingsStorage, private device: DeviceState, private current: ClientPreferences) {}

  public static async load(storage: SettingsStorage): Promise<SettingsStore> {
    const legacy = await storage.loadPreferences();
    const device = normalizeDeviceState(storage.loadDevice(), legacy);
    const preferences = normalizePreferences(legacy);
    storage.saveDevice(device);
    await storage.savePreferences(preferences);
    return new SettingsStore(storage, device, preferences);
  }

  public get serverUrl(): string { return this.device.serverUrl; }
  public get preferences(): ClientPreferences { return this.current; }

  public applyServerUrl(value: string): void {
    const serverUrl = normalizeServerUrl(value);
    if (serverUrl === this.device.serverUrl) return;
    this.persistDevice({ ...this.device, serverUrl });
  }

  public savePreferences(update: (current: ClientPreferences) => ClientPreferences): Promise<void> {
    const write = this.pendingSave.then(async () => {
      const preferences = normalizePreferences(update(this.current));
      await this.storage.savePreferences(preferences);
      this.current = preferences;
      this.emit();
    });
    this.pendingSave = write.catch(() => undefined);
    return write;
  }

  public readConnection(serverUrl: string, workspaceId: string | null): ConnectionLocalState {
    return normalizeConnectionState(this.device.connections[connectionScope(serverUrl, workspaceId)]);
  }

  public saveConnection(serverUrl: string, workspaceId: string | null, value: ConnectionLocalState): void {
    const key = connectionScope(serverUrl, workspaceId);
    this.persistDevice({ ...this.device, connections: { ...this.device.connections, [key]: normalizeConnectionState(value) } });
  }

  public subscribe(listener: () => void): () => void {
    this.listeners.add(listener);
    return () => { this.listeners.delete(listener); };
  }

  public dispose(): void { this.listeners.clear(); }

  private persistDevice(value: DeviceState): void {
    this.storage.saveDevice(value);
    this.device = value;
    this.emit();
  }

  private emit(): void { for (const listener of this.listeners) listener(); }
}
