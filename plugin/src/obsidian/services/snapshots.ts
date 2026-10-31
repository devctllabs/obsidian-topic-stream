import { normalizePath, TFolder, type FileManager, type TFile, type Vault } from "obsidian";
import type { SnapshotPlan, SnapshotVault } from "../../features/snapshots/snapshot";

export type SnapshotHostVault = Pick<Vault, "getAbstractFileByPath" | "createFolder" | "create" | "createBinary">;
const pending = new WeakMap<SnapshotHostVault, Promise<void>>();
export function obsidianSnapshotVault(vault: SnapshotHostVault, files: Pick<FileManager, "trashFile">): SnapshotVault {
  return { write: (plan, signal) => {
    const operation = (pending.get(vault) ?? Promise.resolve()).then(() => commit(vault, files, plan, signal));
    pending.set(vault, operation.catch(() => undefined));
    return operation;
  } };
}

async function commit(vault: SnapshotHostVault, files: Pick<FileManager, "trashFile">, plan: SnapshotPlan, signal: AbortSignal): Promise<void> {
  signal.throwIfAborted();
  const paths = [plan.path, ...plan.attachments.map((asset) => asset.path)].map(normalizePath);
  for (const path of paths) if (vault.getAbstractFileByPath(path)) throw new Error(`A file already exists at ${path}. Choose a different filename.`);
  const created: TFile[] = [];
  try {
    for (const path of paths) await ensureFolders(vault, path);
    for (const attachment of plan.attachments) {
      signal.throwIfAborted();
      created.push(await vault.createBinary(normalizePath(attachment.path), attachment.bytes));
    }
    signal.throwIfAborted();
    await vault.create(normalizePath(plan.path), plan.markdown);
  } catch (error) {
    const remaining: string[] = [];
    for (const file of created.reverse()) {
      try { await files.trashFile(file); }
      catch { remaining.push(file.path); }
    }
    if (remaining.length) throw new Error(`The note could not be created. New attachments could not be removed: ${remaining.join(", ")}. Remove them before trying again.`);
    throw error;
  }
}
async function ensureFolders(vault: SnapshotHostVault, path: string): Promise<void> {
  const segments = path.split("/").slice(0, -1);
  let current = "";
  for (const segment of segments) {
    current = current ? `${current}/${segment}` : segment;
    const existing = vault.getAbstractFileByPath(current);
    if (existing && !(existing instanceof TFolder)) throw new Error(`A file already exists at the folder path ${current}.`);
    if (!existing) await vault.createFolder(current);
  }
}
