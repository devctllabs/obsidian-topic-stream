import type { SnapshotPlan, SnapshotVault } from "../../features/snapshots/snapshot";
import type { SnapshotPreferences } from "../../features/snapshots/SnapshotForm";

/** External vault/persistence ports only; composition and Save controls remain production code. */
export function memorySnapshotVault() {
  const notes = new Map<string, SnapshotPlan>();
  let choice: ReturnType<SnapshotPreferences["read"]> = { content: "compact", images: "links" };
  const preferences: SnapshotPreferences = { read: () => choice, save: async (value) => { choice = value; } };
  const vault: SnapshotVault = { write: async (plan, signal) => {
    signal.throwIfAborted();
    if (notes.has(plan.path)) throw new Error(`A file already exists at ${plan.path}. Choose a different filename.`);
    notes.set(plan.path, structuredClone(plan));
  } };
  return { vault, preferences, notes };
}
