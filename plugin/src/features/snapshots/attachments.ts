import type { Item, ItemImageDownload } from "../../api/contracts";
import type { SnapshotAttachment, SnapshotOptions, SnapshotServices, SnapshotSource } from "./snapshot";
import { hasFullText, snapshotMarkdown, type SnapshotImage } from "./markdown";

const MB = 1024 * 1024;
const extensions: Record<string, string> = { "image/png": "png", "image/jpeg": "jpg", "image/gif": "gif", "image/webp": "webp", "image/avif": "avif", "image/svg+xml": "svg" };
interface Preparation { attachments: SnapshotAttachment[]; total: number; linked: number; saved: Map<string, string | null>; services: SnapshotServices; signal: AbortSignal; directory: string; relative: string; }

export async function prepareSnapshotContent(source: SnapshotSource, options: SnapshotOptions, context: { path: string; services: SnapshotServices; signal: AbortSignal }) {
  const relative = `${context.path.split("/").at(-1)!.slice(0, -3)}.assets`;
  const state: Preparation = { attachments: [], total: 0, linked: 0, saved: new Map(), ...context, directory: context.path.replace(/\.md$/, ".assets"), relative };
  const content = new Map<Item, string>();
  const items = source.kind === "item" ? [source.item] : source.issue.sections.flatMap((section) => section.items.map((saved) => saved.item));
  if (options.content !== "full") return { content, attachments: [], linked: 0 };
  for (const item of items.filter(hasFullText)) {
    const prepared = snapshotMarkdown(item);
    let markdown = prepared.markdown;
    for (const [index, image] of prepared.images.entries()) {
      state.signal.throwIfAborted();
      const local = options.images === "local" ? await localImage(item, { image, ordinal: index + 1 }, state) : null;
      markdown = markdown.replace(`[Image](${image.token})`, imageMarkup(image, local, options.images));
      if (options.images === "local" && !local) state.linked++;
    }
    content.set(item, markdown);
  }
  return { content, attachments: state.attachments, linked: state.linked };
}
function imageMarkup(image: SnapshotImage, local: string | null, mode: SnapshotOptions["images"]): string {
  const embed = mode === "remote" || Boolean(local);
  const url = local ?? image.url;
  const alt = image.alt.replace(/[\\[\]]/g, "\\$&");
  const destination = local ? encodeURI(local) : url;
  return destination ? `${embed ? "!" : ""}[${alt}](<${destination.replaceAll(">", "%3E").replaceAll("<", "%3C")}>)` : alt;
}

async function localImage(item: Item, { image, ordinal }: { image: SnapshotImage; ordinal: number }, state: Preparation): Promise<string | null> {
  if (!image.descriptor || !image.url) return null;
  const key = `${item.id}:${image.descriptor.id}`;
  if (state.saved.has(key)) return state.saved.get(key)!;
  if (state.attachments.length >= 30) return null;
  let path: string | null = null;
  try {
    const download = await state.services.images.downloadItemImage(item.id, image.descriptor.id);
    state.signal.throwIfAborted();
    const checked = checkedImage(download, state);
    const hash = Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256", checked.bytes))).slice(0, 4).map((value) => value.toString(16).padStart(2, "0")).join("");
    const filename = `${item.id.replaceAll("-", "").slice(-8)}-${ordinal}-${hash}.${checked.extension}`;
    state.attachments.push({ path: `${state.directory}/${filename}`, bytes: checked.bytes });
    state.total += checked.bytes.byteLength;
    path = `${state.relative}/${filename}`;
  } catch { state.signal.throwIfAborted(); }
  state.saved.set(key, path);
  return path;
}
function checkedImage(download: ItemImageDownload, state: Preparation): { bytes: ArrayBuffer; extension: string } {
  const extension = extensions[download.contentType];
  if (!extension || !download.bytes.byteLength || download.bytes.byteLength > 10 * MB) throw new Error("Image unavailable or too large.");
  const bytes = extension === "svg" ? state.services.sanitizeSvg(download.bytes) : download.bytes;
  if (!bytes.byteLength || bytes.byteLength > 10 * MB || state.total + bytes.byteLength > 100 * MB) throw new Error("Image exceeds this Save's attachment budget.");
  return { bytes, extension };
}
