import { describe, expect, it, vi } from "vitest";
import { TFile, TFolder } from "obsidian";
import type { SnapshotPlan } from "../../features/snapshots/snapshot";
import { deferred } from "../../test/storybook/deferred";
import { obsidianSnapshotVault, type SnapshotHostVault } from "./snapshots";

vi.mock("obsidian", () => ({ normalizePath: (path: string) => path.replaceAll("\\", "/"), TFile: class { path = ""; }, TFolder: class { path = ""; } }));
const signal = new AbortController().signal;
const plan: SnapshotPlan = { path: "Reading/Note.md", markdown: "# Snapshot", attachments: [{ path: "Reading/Note.assets/image.png", bytes: new ArrayBuffer(4) }] };
function host() {
  const paths = new Map<string, TFile | TFolder>();
  const create = async (path: string) => { if (paths.has(path)) throw new Error("Existing file"); const file = Object.assign(new TFile(), { path }); paths.set(path, file); return file; };
  const vault = { getAbstractFileByPath: (path: string) => paths.get(path) ?? null, create: vi.fn(create), createBinary: vi.fn(create), createFolder: vi.fn(async (path: string) => { const folder = Object.assign(new TFolder(), { path }); paths.set(path, folder); return folder; }) } satisfies SnapshotHostVault;
  const files = { trashFile: vi.fn(async (file: TFile | TFolder) => { paths.delete(file.path); }) };
  return { paths, vault, files };
}
describe("Vault snapshot host boundary", () => {
  it("creates portable folders, attachments and then the note using host APIs", async () => {
    const output = host();
    await obsidianSnapshotVault(output.vault, output.files).write(plan, signal);
    expect(output.vault.create).toHaveBeenCalledWith(plan.path, plan.markdown);
    expect(output.vault.createBinary).toHaveBeenCalledWith(plan.attachments[0]!.path, plan.attachments[0]!.bytes);
    expect(output.vault.createFolder.mock.calls.map(([path]) => path)).toEqual(["Reading", "Reading/Note.assets"]);
    expect(output.files.trashFile).not.toHaveBeenCalled();
  });
  it("refuses existing notes and attachment collisions before any writes", async () => {
    for (const path of [plan.path, plan.attachments[0]!.path]) {
      const output = host(); output.paths.set(path, Object.assign(new TFile(), { path }));
      await expect(obsidianSnapshotVault(output.vault, output.files).write(plan, signal)).rejects.toThrow(/exists|existing/i);
      expect(output.vault.createBinary).not.toHaveBeenCalled();
      expect(output.vault.createFolder).not.toHaveBeenCalled();
      expect(output.files.trashFile).not.toHaveBeenCalled();
    }
  });
  it("rolls back only newly-created attachments when creating the note fails", async () => {
    const output = host();
    output.paths.set("Reading/My picture.png", Object.assign(new TFile(), { path: "Reading/My picture.png" }));
    output.vault.create.mockRejectedValue(new Error("Disk full"));
    await expect(obsidianSnapshotVault(output.vault, output.files).write(plan, signal)).rejects.toThrow("Disk full");
    expect(output.paths.has(plan.attachments[0]!.path)).toBe(false);
    expect(output.paths.has("Reading/My picture.png")).toBe(true);
    expect(output.files.trashFile).toHaveBeenCalledTimes(1);
  });
  it("serializes separate view adapters for one vault and cancels queued writes", async () => {
    const output = host(); const pending = deferred<TFile>();
    output.vault.create.mockImplementationOnce(() => pending.promise);
    const first = obsidianSnapshotVault(output.vault, output.files).write({ ...plan, attachments: [] }, signal);
    await vi.waitFor(() => expect(output.vault.create).toHaveBeenCalledTimes(1));
    const controller = new AbortController();
    const second = obsidianSnapshotVault(output.vault, output.files).write({ ...plan, path: "Reading/Other.md", attachments: [] }, controller.signal);
    const assertion = expect(second).rejects.toThrow();
    controller.abort(); pending.resolve(new TFile());
    await first; await assertion;
    expect(output.vault.create).toHaveBeenCalledTimes(1);
  });
  it("reports attachment cleanup failures and keeps a later save usable", async () => {
    const output = host();
    output.vault.create.mockRejectedValueOnce(new Error("Disk full")); output.files.trashFile.mockRejectedValueOnce(new Error("Read only"));
    const writer = obsidianSnapshotVault(output.vault, output.files);
    await expect(writer.write(plan, signal)).rejects.toThrow(/attachments.*Reading\/Note.assets\/image.png/);
    await writer.write({ ...plan, path: "Reading/Other.md", attachments: [] }, signal);
    expect(output.paths.has("Reading/Other.md")).toBe(true);
  });
});
