import type { WorkspaceStatePort } from "../../features/settings/device-state";
import { normalizeConnectionState } from "../../features/settings/device-state";

export function memoryWorkspaceState(): WorkspaceStatePort {
  const value = normalizeConnectionState(null);
  const listeners = new Set<() => void>();
  return { read: () => Promise.resolve(structuredClone(value)), update: (change) => { change(value); for (const listener of listeners) listener(); return Promise.resolve(); }, subscribe: (listener) => { listeners.add(listener); return () => { listeners.delete(listener); }; } };
}
