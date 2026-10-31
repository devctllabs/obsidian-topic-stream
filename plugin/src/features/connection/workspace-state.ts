import type { Schema } from "../../api/contracts";
import type { SettingsStore } from "../settings/persistence";
import type { WorkspaceStatePort } from "../settings/device-state";
import type { ConnectionLocalState } from "../settings/device-state";

/** Resolves the public Workspace identity before touching device-local action records. */
export class WorkspaceState implements WorkspaceStatePort {
  private workspace: Promise<string> | null = null;
  public constructor(private readonly settings: Pick<SettingsStore, "readConnection" | "saveConnection" | "subscribe">, private readonly serverUrl: string, private readonly service: { getSystemStatus: () => Promise<Schema<"SystemStatus">> }) {}
  public subscribe = (listener: () => void): (() => void) => this.settings.subscribe(listener);
  public read = async (): Promise<ConnectionLocalState> => this.settings.readConnection(this.serverUrl, await this.identity());
  public update = async (change: (value: ConnectionLocalState) => void): Promise<void> => {
    const workspaceId = await this.identity();
    const state = this.settings.readConnection(this.serverUrl, workspaceId);
    change(state);
    this.settings.saveConnection(this.serverUrl, workspaceId, state);
  };
  private identity(): Promise<string> {
    this.workspace ??= this.service.getSystemStatus().then((status) => status.workspaceId).catch((error: unknown) => { this.workspace = null; throw error; });
    return this.workspace;
  }
}
