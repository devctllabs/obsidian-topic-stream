import type { Item, ItemImageDownload } from "../../api/contracts";

export interface ReaderImageService { downloadItemImage: (itemId: string, imageId: string) => Promise<ItemImageDownload>; }
export interface ImageObjectUrls { create: (download: ItemImageDownload) => string; revoke: (url: string) => void; }
export type ReaderImageState = { kind: "blocked" | "loading" } | { kind: "ready"; url: string } | { kind: "failed"; error: Error };
export interface ReaderImageSnapshot { consented: boolean; images: ReadonlyMap<string, ReaderImageState>; }
type ImageInput = Pick<Item, "id" | "images">;
interface ImageRecord { id: string; snapshot: ReaderImageSnapshot; descriptors: Map<string, Item["images"][number]>; listeners: Set<() => void>; }
interface ImageWork { record: ImageRecord; imageId: string; }
const empty: ReaderImageSnapshot = { consented: false, images: new Map() };
const browserUrls: ImageObjectUrls = { create: ({ bytes, contentType }) => URL.createObjectURL(new Blob([bytes], { type: contentType })), revoke: (url) => URL.revokeObjectURL(url) };

/** The opened view owns consent, cached image bytes/URLs, bounded downloads and final cleanup. */
export class ReaderImages {
  private records = new Map<string, ImageRecord>();
  private urls = new Set<string>();
  private queue: ImageWork[] = [];
  private active = 0;
  private disposed = false;
  public constructor(private readonly client: ReaderImageService, private readonly objectUrls: ImageObjectUrls = browserUrls) {}
  public getSnapshot = (itemId: string): ReaderImageSnapshot => this.records.get(itemId)?.snapshot ?? empty;
  public subscribe(input: ImageInput, listener: () => void): () => void {
    if (this.disposed) return () => undefined;
    const record = this.ensure(input);
    record.listeners.add(listener);
    if (record.snapshot.consented) this.queueMissing(record);
    return () => { record.listeners.delete(listener); };
  }
  public allow(input: ImageInput): void {
    if (this.disposed) return;
    const record = this.ensure(input);
    this.publish(record, { ...record.snapshot, consented: true });
    this.queueMissing(record);
  }
  public retry(itemId: string, imageId: string): void {
    const record = this.records.get(itemId);
    if (record?.snapshot.consented) this.enqueue(record, imageId, true);
  }
  public failedToDisplay(itemId: string, imageId: string, url: string): void {
    const record = this.records.get(itemId);
    const state = record?.snapshot.images.get(imageId);
    if (!record || state?.kind !== "ready" || state.url !== url) return;
    this.objectUrls.revoke(url); this.urls.delete(url);
    this.phase(record, imageId, { kind: "failed", error: new Error("Could not display this image.") });
  }
  public dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    this.queue = [];
    this.records.forEach((record) => record.listeners.clear()); this.records.clear();
    this.urls.forEach((url) => this.objectUrls.revoke(url)); this.urls.clear();
  }
  private ensure(input: ImageInput): ImageRecord {
    let record = this.records.get(input.id);
    if (!record) { record = { id: input.id, snapshot: empty, descriptors: new Map(), listeners: new Set() }; this.records.set(input.id, record); }
    for (const image of input.images) if (!record.descriptors.has(image.id)) { record.descriptors.set(image.id, image); this.phase(record, image.id, { kind: "blocked" }); }
    return record;
  }
  private queueMissing(record: ImageRecord): void { record.descriptors.forEach((_image, id) => this.enqueue(record, id)); }
  private enqueue(record: ImageRecord, imageId: string, retry = false): void {
    const current = record.snapshot.images.get(imageId);
    if (this.disposed || !record.descriptors.has(imageId)) return;
    if (current?.kind !== "blocked" && !(retry && current?.kind === "failed")) return;
    this.phase(record, imageId, { kind: "loading" });
    this.queue.push({ record, imageId }); this.pump();
  }
  private pump(): void {
    while (!this.disposed && this.active < 4 && this.queue.length > 0) {
      const work = this.queue.shift()!;
      this.active++;
      void this.load(work).finally(() => { this.active--; this.pump(); });
    }
  }
  private async load({ record, imageId }: ImageWork): Promise<void> {
    try {
      const download = await this.client.downloadItemImage(record.id, imageId);
      if (this.disposed) return;
      const url = this.objectUrls.create(download); this.urls.add(url);
      this.phase(record, imageId, { kind: "ready", url });
    } catch (error) { if (!this.disposed) this.phase(record, imageId, { kind: "failed", error: error instanceof Error ? error : new Error("Could not load this image.") }); }
  }
  private phase(record: ImageRecord, imageId: string, state: ReaderImageState): void {
    this.publish(record, { ...record.snapshot, images: new Map(record.snapshot.images).set(imageId, state) });
  }
  private publish(record: ImageRecord, value: ReaderImageSnapshot): void { record.snapshot = value; record.listeners.forEach((listener) => listener()); }
}
