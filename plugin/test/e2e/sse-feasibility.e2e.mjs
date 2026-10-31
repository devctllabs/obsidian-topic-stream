import assert from "node:assert/strict";
import { browser } from "@wdio/globals";
import { describe, it } from "mocha";

// Optional real-host check against production CORS/SSE; no diagnostic permission grant.
const endpoint = process.env.SSE_PROBE_URL;
describe("bounded real-host SSE feasibility", () => {
  it("records Fetch/EventSource in main and pop-out realms and compares buffered requestUrl", async function () {
    if (!endpoint) this.skip();
    const result = await browser.executeObsidian(async ({ app, obsidian }, baseUrl) => {
      async function fetchProbe(realm, tag) {
        const controller = new realm.AbortController();
        const began = performance.now();
        const timeout = setTimeout(() => controller.abort(), 2500);
        const reads = [];
        try {
          const response = await realm.fetch(`${baseUrl}/api/v1/events?probe=fetch-${tag}`, { signal: controller.signal, cache: "no-store" });
          const reader = response.body.getReader();
          let text = "";
          while (!text.includes("event: jobChanged")) {
            const value = await reader.read();
            if (value.done) break;
            text += new TextDecoder().decode(value.value);
            reads.push({ elapsedMs: Math.round(performance.now() - began), text: new TextDecoder().decode(value.value) });
          }
          return { status: "streaming", origin: realm.location.origin, reads };
        } catch (error) { return { status: "failed", origin: realm.location.origin, elapsedMs: Math.round(performance.now() - began), error: String(error), reads }; }
        finally { clearTimeout(timeout); controller.abort(); }
      }
      async function eventSourceProbe(realm, tag) {
        const began = performance.now();
        return new Promise((resolve) => {
          const stream = new realm.EventSource(`${baseUrl}/api/v1/events?probe=eventsource-${tag}`);
          const finish = (status) => { clearTimeout(timeout); stream.close(); resolve({ status, origin: realm.location.origin, elapsedMs: Math.round(performance.now() - began) }); };
          const timeout = setTimeout(() => finish("timeout"), 2500);
          stream.addEventListener("jobChanged", () => finish("streaming"));
          stream.onerror = () => finish("failed");
        });
      }
      const main = { fetch: await fetchProbe(window, "main"), eventSource: await eventSourceProbe(window, "main") };
      const popout = app.workspace.openPopoutLeaf({ width: 500, height: 600 });
      let popup;
      try {
        await new Promise((resolve) => setTimeout(resolve, 200));
        const realm = popout.containerEl.ownerDocument.defaultView;
        popup = { distinctRealm: realm !== window, fetch: await fetchProbe(realm, "popout"), eventSource: await eventSourceProbe(realm, "popout") };
      } finally { popout.detach(); }
      const began = performance.now();
      const response = await obsidian.requestUrl({ url: `${baseUrl}/api/v1/events?probe=requestUrl`, throw: false });
      const buffered = { elapsedMs: Math.round(performance.now() - began), status: response.status, contentType: response.headers["content-type"], hasReadableBody: Boolean(response.body), containsConnected: response.text.includes(": connected"), containsJob: response.text.includes("event: jobChanged") };
      return { obsidianVersion: obsidian.apiVersion, main, popup, buffered };
    }, endpoint);
    console.log("SSE_NATIVE_PROBE " + JSON.stringify(result));
    assert.equal(result.buffered.status, 200);
    assert.equal(result.buffered.containsConnected, true);
    assert.equal(result.buffered.containsJob, true);
    assert.equal(result.popup.distinctRealm, true);
    for (const realm of [result.main, result.popup]) {
      assert.equal(realm.fetch.status, "streaming");
      assert.equal(realm.eventSource.status, "streaming");
      assert.ok(realm.fetch.reads.length >= 2);
      assert.ok(realm.fetch.reads[0].text.includes(": connected"));
      assert.ok(realm.fetch.reads.at(-1).text.includes("event: jobChanged"));
    }
  });
});
