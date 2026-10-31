import { describe, expect, it, vi } from "vitest";
import { ReaderImages, type ImageObjectUrls } from "./images";
import type { ItemImageDownload } from "../../api/contracts";
import { fixtureId, makeItem } from "../../test/storybook/fixtures";
import { fixtureImage } from "../../test/storybook/images";
import { deferred } from "../../test/storybook/deferred";

function objectUrls() {
  let sequence = 0;
  return { create: vi.fn<ImageObjectUrls["create"]>(() => `blob:fixture-${++sequence}`), revoke: vi.fn<ImageObjectUrls["revoke"]>() };
}
function itemWithImages(count: number) {
  return makeItem(0, { images: Array.from({ length: count }, (_, index) => ({ id: fixtureId(800 + index), url: `https://images.example/${index}.svg`, altText: `Diagram ${index + 1}` })) });
}

describe("view-owned Reader images", () => {
  it("waits for consent, deduplicates clicks, bounds active downloads and releases every cached URL", async () => {
    const pending = deferred<ItemImageDownload>();
    const download = vi.fn(async (_itemId: string, _imageId: string) => pending.promise);
    const urls = objectUrls();
    const images = new ReaderImages({ downloadItemImage: download }, urls);
    const item = itemWithImages(5);
    const unsubscribe = images.subscribe(item, () => undefined);
    expect(download).not.toHaveBeenCalled();
    images.allow(item); images.allow(item);
    expect(download).toHaveBeenCalledTimes(4);
    pending.resolve(fixtureImage());
    await vi.waitFor(() => expect(Array.from(images.getSnapshot(item.id).images.values()).filter((value) => value.kind === "ready")).toHaveLength(5));
    expect(download).toHaveBeenCalledTimes(5);
    images.allow(item);
    expect(download).toHaveBeenCalledTimes(5);
    unsubscribe(); images.dispose(); images.dispose();
    expect(urls.create).toHaveBeenCalledTimes(5);
    expect(urls.revoke.mock.calls.map(([url]) => url).sort()).toEqual(Array.from({ length: 5 }, (_, index) => `blob:fixture-${index + 1}`));
  });
  it("does not create URLs or notify subscribers after disposal of pending downloads", async () => {
    const pending = deferred<ItemImageDownload>();
    const urls = objectUrls();
    const images = new ReaderImages({ downloadItemImage: async () => pending.promise }, urls);
    const item = itemWithImages(1);
    const listener = vi.fn();
    images.subscribe(item, listener); images.allow(item);
    const notifications = listener.mock.calls.length;
    images.dispose(); pending.resolve(fixtureImage());
    await pending.promise; await Promise.resolve();
    expect(urls.create).not.toHaveBeenCalled();
    expect(listener).toHaveBeenCalledTimes(notifications);
  });
  it("keeps a retried image ready when an obsolete decode error arrives for the previous URL", async () => {
    const urls = objectUrls();
    const download = vi.fn(async () => fixtureImage());
    const images = new ReaderImages({ downloadItemImage: download }, urls);
    const item = itemWithImages(1);
    const imageId = item.images[0]!.id;
    images.subscribe(item, () => undefined); images.allow(item);
    await vi.waitFor(() => expect(images.getSnapshot(item.id).images.get(imageId)).toEqual({ kind: "ready", url: "blob:fixture-1" }));
    images.failedToDisplay(item.id, imageId, "blob:fixture-1");
    images.retry(item.id, imageId);
    await vi.waitFor(() => expect(images.getSnapshot(item.id).images.get(imageId)).toEqual({ kind: "ready", url: "blob:fixture-2" }));
    images.failedToDisplay(item.id, imageId, "blob:fixture-1");
    expect(images.getSnapshot(item.id).images.get(imageId)).toEqual({ kind: "ready", url: "blob:fixture-2" });
    expect(download).toHaveBeenCalledTimes(2);
    images.dispose();
    expect(urls.revoke.mock.calls).toEqual([["blob:fixture-1"], ["blob:fixture-2"]]);
  });
});
