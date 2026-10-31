import { describe, expect, it, vi } from "vitest";
import { WorkspaceState } from "./workspace-state";
import { SettingsStore } from "../settings/persistence";
import { FIXTURE_TIME, fixtureId } from "../../test/storybook/fixtures";

async function setup() {
  return SettingsStore.load({ loadPreferences: () => Promise.resolve(null), savePreferences: () => Promise.resolve(), loadDevice: () => null, saveDevice: () => undefined });
}
describe("device-local Workspace action state", () => {
  it("resolves public identity and isolates a new Workspace at the same endpoint", async () => {
    const settings = await setup();
    const status = { status: "ready" as const, version: "test", workspaceId: fixtureId(1), now: FIXTURE_TIME, components: [] };
    const service = { getSystemStatus: vi.fn(async () => status) };
    const first = new WorkspaceState(settings, "http://example.org", service);
    await first.update((value) => { value.unresolvedBuilds.digest = "request"; });
    expect((await first.read()).unresolvedBuilds).toEqual({ digest: "request" });
    const another = new WorkspaceState(settings, "http://example.org", { getSystemStatus: async () => ({ ...status, workspaceId: fixtureId(2) }) });
    expect((await another.read()).unresolvedBuilds).toEqual({});
    const endpoint = new WorkspaceState(settings, "http://other.example.org", service);
    expect((await endpoint.read()).unresolvedBuilds).toEqual({});
  });
  it("does not persist without verified Workspace identity and can retry a failed status request", async () => {
    const settings = await setup();
    const read = vi.spyOn(settings, "readConnection");
    const service = { getSystemStatus: vi.fn<() => Promise<never>>().mockRejectedValue(new Error("Unavailable")) };
    const state = new WorkspaceState(settings, "http://example.org", service);
    await expect(state.update((value) => { value.unresolvedBuilds.digest = "request"; })).rejects.toThrow("Unavailable");
    expect(read).not.toHaveBeenCalled();
    await expect(state.read()).rejects.toThrow("Unavailable");
    expect(service.getSystemStatus).toHaveBeenCalledTimes(2);
  });
});
