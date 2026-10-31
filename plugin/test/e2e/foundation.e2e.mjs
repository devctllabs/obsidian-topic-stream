import assert from "node:assert/strict";
import { createServer } from "node:http";
import { browser } from "@wdio/globals";
import { before, after, describe, it } from "mocha";
import { obsidianPage } from "wdio-obsidian-service";

async function backend(name) {
  const timestamp = "2026-10-27T09:00:00Z";
  const source = {
    id: "11111111-1111-4111-8111-111111111111", name,
    config: { kind: "rss", url: "https://example.org/feed", allowPrivateNetwork: false },
    enabled: true, pollIntervalMinutes: 60, lastSyncedAt: null, createdAt: timestamp, updatedAt: timestamp
  };
  const topic = {
    id: "33333333-3333-4333-8333-333333333333", name: "Systems", sourceIds: [], itemKinds: [],
    include: "Practical systems", exclude: "", mode: "ordinary", enabled: true, revision: 1,
    createdAt: timestamp, updatedAt: timestamp
  };
  const item = {
    id: "22222222-2222-4222-8222-222222222222", sourceIds: [source.id], kind: "article",
    title: name + " article", url: "https://example.org/article", canonicalUrl: "https://example.org/article",
    excerpt: "A production-shell reading fixture.", contentMarkdown: "## Canonical source\n\nFull content rendered by Obsidian, with **native emphasis**.\n\n![First diagram](https://images.example/first.svg)\n\n![Second diagram](https://images.example/second.svg)\n\n![[Welcome]]\n\n<img src=\"https://images.example/blocked.svg\">\n\n```topic-stream-fixture\nThis is source text, not an instruction.\n```",
    details: { type: "article", description: "A test article" }, authors: ["Example Author"],
    publishedAt: timestamp, discoveredAt: timestamp, contentOrigin: "article_body",
    summarySupported: true, summarySourceVersion: "a".repeat(64),
    readingState: "unread", visibility: "visible", topicIds: [topic.id], feedback: null, images: [
      { id: "88888888-8888-4888-8888-888888888881", url: "https://images.example/first.svg", altText: "First diagram" },
      { id: "88888888-8888-4888-8888-888888888882", url: "https://images.example/second.svg", altText: "Second diagram" }
    ]
  };
  const summary = { sourceVersion: item.summarySourceVersion, supported: true, canGenerate: true, unavailableReason: null, state: "not_requested", jobId: null, failure: null, retryable: false, result: null };
  const definition = { id: "44444444-4444-4444-8444-444444444444", name: "Native reading", topics: [{ topicId: topic.id, itemLimit: 5 }], enabled: false, timezone: "Europe/Madrid", recurrence: { frequency: "weekly", weekdays: [1], hour: 12, minute: 0 }, version: 1, nextRunAt: null, lastSuccessfulAt: null, createdAt: timestamp, updatedAt: timestamp };
  const job = { id: "66666666-6666-4666-8666-666666666666", kind: "generate-digest", queue: "generation", status: "running", attempt: 1, maxAttempts: 5, cancellationRequested: false, createdAt: timestamp, updatedAt: timestamp };
  const issue = { id: "55555555-5555-4555-8555-555555555555", digestId: definition.id, generationJobId: job.id, title: "Saved native edition", status: "complete", scheduledFor: null, generatedAt: timestamp,
    sections: [{ topicId: topic.id, topicName: topic.name, topicRevision: 1, failure: null, items: [{ item: structuredClone(item), summary: "A synthetic saved excerpt for the native edition.", explanation: { summary: "Fixture", signals: {}, pipelineVersion: "test" }, aiSummary: null }] }] };
  const requests = [];
  const streams = new Set();
  const streaming = { available: true };
  const issues = [issue];
  const server = createServer(async (request, response) => {
    response.setHeader("Content-Type", "application/json");
    const url = new URL(request.url, "http://fixture");
    const chunks = [];
    for await (const chunk of request) chunks.push(chunk);
    const body = chunks.length ? JSON.parse(Buffer.concat(chunks).toString()) : null;
    requests.push({ method: request.method, path: url.pathname, body, query: Object.fromEntries(url.searchParams), headers: request.headers });
    if (url.pathname === "/api/v1/events") {
      // This fixture models the approved policy; native production Echo policy has its own probe.
      if (request.headers.origin === "app://obsidian.md") response.setHeader("Access-Control-Allow-Origin", request.headers.origin);
      if (!streaming.available) { response.statusCode = 503; response.end("{}"); return; }
      response.setHeader("Content-Type", "text/event-stream");
      streams.add(response); response.on("close", () => streams.delete(response));
      response.write(": connected\n\n"); return;
    }
    if (url.pathname.startsWith("/api/v1/digest-issues/")) {
      const saved = issues.find((value) => value.id === url.pathname.split("/").at(-1));
      response.statusCode = saved ? 200 : 404; response.end(JSON.stringify(saved ?? {})); return;
    }
    if (request.method === "POST" && url.pathname === `/api/v1/digests/${definition.id}/generate`) {
      response.statusCode = 202; response.end(JSON.stringify({ jobId: job.id })); return;
    }
    if (request.method === "POST" && url.pathname === `/api/v1/jobs/${job.id}/cancel`) {
      job.cancellationRequested = true; response.statusCode = 202; response.end(JSON.stringify(job)); return;
    }
    if (url.pathname.startsWith("/api/v1/items/" + item.id + "/images/")) {
      response.setHeader("Content-Type", "image/svg+xml");
      response.end('<svg xmlns="http://www.w3.org/2000/svg" width="300" height="120" viewBox="0 0 300 120"><path d="M30 60h240" stroke="#f2b38a" stroke-width="3"/><circle cx="150" cy="60" r="20" fill="#f2b38a"/></svg>'); return;
    }
    if (request.method === "POST" && url.pathname === "/api/v1/items/" + item.id + "/ai-summary") {
      summary.state = "ready"; summary.jobId = source.id;
      summary.result = { id: topic.id, generationJobId: source.id, sourceVersion: item.summarySourceVersion, text: "An explicit synthetic summary from the native test service.", model: "fixture", language: "en", maxInputChars: 48000, maxSummaryCharacters: 800, instructionsVersion: "test", algorithmVersion: "test", createdAt: timestamp };
      response.statusCode = 202; response.end(JSON.stringify({ jobId: source.id })); return;
    }
    if (request.method === "PUT" && url.pathname === "/api/v1/items/" + item.id + "/reading-state") {
      item.readingState = body.state;
      response.statusCode = 204; response.end(); return;
    }
    const scope = body?.topicId ?? url.searchParams.get("topicId");
    const feed = {
      items: item.visibility === "visible" && (!scope || item.topicIds.includes(scope))
        ? [{ item, score: 0.8, explanation: { summary: "Fixture", signals: {}, pipelineVersion: "test" } }] : [],
      nextCursor: null, snapshot: "test-snapshot", generatedAt: timestamp
    };
    const routes = {
      "/api/v1/system/status": { status: "ready", version: "test", workspaceId: source.id, components: [], now: "2026-10-27T09:00:00Z" },
      "/api/v1/sources": { items: [source] },
      "/api/v1/topics": { items: [topic] },
      "/api/v1/feed": feed, "/api/v1/feed/refresh": feed,
      "/api/v1/search": { items: feed.items, nextCursor: null },
      "/api/v1/digests": { items: [definition] }, "/api/v1/digest-issues": { items: issues, nextCursor: null },
      [`/api/v1/digest-issues/${issue.id}`]: issue,
      "/api/v1/jobs": { items: [job], nextCursor: null }, [`/api/v1/jobs/${job.id}`]: job,
      ["/api/v1/items/" + item.id]: item,
      ["/api/v1/items/" + item.id + "/ai-summary"]: summary
    };
    const data = routes[url.pathname];
    response.statusCode = data === undefined ? 404 : 200;
    response.end(JSON.stringify(data ?? {}));
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  return { server, requests, item, issue, issues, job, summary, streams, streaming, emit: (type, data) => { for (const stream of streams) stream.write(`event: ${type}\ndata: ${JSON.stringify(data)}\n\n`); }, disconnect: () => { for (const stream of streams) stream.end(); }, url: `http://127.0.0.1:${server.address().port}` };
}

describe("production foundation in Obsidian", () => {
  let first;
  let second;

  before(async () => {
    first = await backend("First backend source");
    second = await backend("Second backend source");
  });

  after(async () => {
    await Promise.all([first, second].filter(Boolean).map(({ server }) => new Promise((resolve) => {
      server.closeAllConnections();
      server.close(resolve);
    })));
  });

  it("replaces an open connection and reloads without duplicate registrations or roots", async () => {
    await browser.executeObsidian(({ plugins }) => {
      window.topicStreamTestErrors = [];
      window.topicStreamHostResizeNotifications = [];
      window.addEventListener("error", (event) => {
        // Chromium's deferred-layout notification has no exception. Keep it separately visible;
        // production code has no ResizeObserver. Never suppress application exceptions/rejections.
        if (!event.error && event.message === "ResizeObserver loop completed with undelivered notifications.") window.topicStreamHostResizeNotifications.push(event.message);
        else window.topicStreamTestErrors.push(event.message);
      });
      window.addEventListener("unhandledrejection", (event) => window.topicStreamTestErrors.push(String(event.reason)));
    });
    await browser.executeObsidian(({ plugins }, url) => plugins.topicStream.settings.applyServerUrl(url), first.url);
    await browser.executeObsidianCommand("topic-stream:open-topic-stream");
    const root = browser.$(".topic-stream-root");
    await root.waitForDisplayed();
    await browser.waitUntil(() => first.streams.size === 1);
    const row = browser.$(".topic-stream-row");
    await row.waitForDisplayed();
    await row.click();
    await browser.$(".topic-stream-detail-pane h1").waitForDisplayed();
    assert.equal(await browser.$(".topic-stream-detail-pane h1").getText(), "First backend source article");
    await browser.execute(() => { document.querySelector(".topic-stream-root").style.width = "420px"; });
    assert.equal(await browser.$(".topic-stream-list-pane").isDisplayed(), false);
    await browser.$('button[aria-label="Back to list"]').click();
    await row.waitForDisplayed();
    assert.equal(await row.isFocused(), true);
    await row.click();
    await browser.$('button[aria-label="Settings"]').click();
    await browser.waitUntil(async () => (await browser.$(".topic-stream-source-rows").getHTML()).includes("First backend source"));

    await browser.executeObsidian(({ plugins }, url) => plugins.topicStream.settings.applyServerUrl(url), second.url);
    await browser.waitUntil(() => first.streams.size === 0 && second.streams.size === 1);
    await browser.waitUntil(async () => !(await root.getHTML()).includes("First backend source article"));
    await browser.$('button[aria-label="Settings"]').click();
    await browser.waitUntil(async () => (await browser.$(".topic-stream-source-rows").getHTML()).includes("Second backend source"));
    assert.ok(!(await root.getHTML()).includes("First backend source"));

    await browser.executeObsidian(async ({ plugins }) => {
      await plugins.topicStream.settings.savePreferences((current) => ({ ...current, feedFilter: "unread" }));
    });
    await obsidianPage.disablePlugin("topic-stream");
    await browser.waitUntil(() => second.streams.size === 0);
    assert.equal(await browser.$$(".topic-stream-root").length, 0);
    await obsidianPage.enablePlugin("topic-stream");
    await browser.executeObsidianCommand("topic-stream:open-topic-stream");
    await browser.$(".topic-stream-root").waitForDisplayed();
    await browser.waitUntil(() => second.streams.size === 1);
    await browser.$('button[aria-label="Settings"][aria-pressed="true"]').waitForDisplayed();
    const state = await browser.executeObsidian(async ({ app, plugins }) => ({
      url: plugins.topicStream.settings.serverUrl,
      filter: plugins.topicStream.settings.preferences.feedFilter,
      views: app.workspace.getLeavesOfType("topic-stream-view").length,
      commands: app.commands.listCommands().filter(({ id }) => id === "topic-stream:open-topic-stream").length,
      data: JSON.parse(await app.vault.adapter.read(`${app.vault.configDir}/plugins/topic-stream/data.json`)),
      errors: window.topicStreamTestErrors
    }));
    assert.equal(state.url, second.url);
    assert.equal(state.filter, "unread");
    assert.equal(state.views, 1);
    assert.equal(state.commands, 1);
    assert.equal(await browser.$$(".topic-stream-root").length, 1);
    assert.equal("serverUrl" in state.data, false);
    assert.deepEqual(state.errors, []);
  });
  it("tests a draft connection without applying it and saves native client preferences", async () => {
    await browser.executeObsidian(({ app }) => { app.setting.open(); app.setting.openTabById("topic-stream"); });
    const setting = (name) => browser.$(`//div[contains(concat(" ", normalize-space(@class), " "), " setting-item ")][.//div[@class="setting-item-name" and text()="${name}"]]`);
    const address = setting("Server URL").$("input");
    await address.waitForDisplayed();
    await address.setValue(first.url);
    await browser.$("button=Test connection").click();
    await browser.waitUntil(async () => browser.execute(() => Array.from(document.querySelectorAll("p[role=status]")).some((node) => node.textContent === "Connected · Topic Stream test")), { timeout: 10000, timeoutMsg: "Connection test did not succeed" });
    assert.equal(await browser.executeObsidian(({ plugins }) => plugins.topicStream.settings.serverUrl), second.url);
    await setting("Default feed filter").$("select").selectByAttribute("value", "all");
    await setting("Snapshot content").$("select").selectByAttribute("value", "full");
    await setting("Snapshot images").$("select").selectByAttribute("value", "remote");
    await browser.waitUntil(async () => {
      const prefs = await browser.executeObsidian(({ plugins }) => plugins.topicStream.settings.preferences);
      return prefs.feedFilter === "all" && prefs.snapshot.content === "full" && prefs.snapshot.images === "remote";
    });
    await browser.executeObsidian(({ app }) => { app.setting.close(); });
  });

  it("keeps scoped Feed sessions, persists Unread and starts a new session after closing the view", async () => {
    const publications = () => second.requests.filter((request) => request.method === "POST" && request.path === "/api/v1/feed/refresh");
    const beforeOpen = publications().length;
    await browser.executeObsidian(async ({ app, plugins }) => {
      await plugins.topicStream.settings.savePreferences((current) => ({ ...current, feedFilter: "all" }));
      for (const leaf of app.workspace.getLeavesOfType("topic-stream-view")) leaf.detach();
    });
    await browser.waitUntil(async () => (await browser.$$(".topic-stream-root")).length === 0);
    await browser.executeObsidianCommand("topic-stream:open-topic-stream");
    const back = browser.$('button[aria-label="Back to reading"]');
    if (await back.isDisplayed()) await back.click();
    await browser.$(".topic-stream-row").waitForDisplayed();
    assert.equal(publications().length, beforeOpen + 1);
    await browser.$(".topic-stream-topic-trigger").click();
    await browser.$('[role="option"]=Systems').click();
    await browser.waitUntil(async () => (await browser.$(".topic-stream-topic-trigger").getText()) === "Systems");
    await browser.$(".topic-stream-row").waitForDisplayed();
    assert.deepEqual(publications().at(-1).body, { limit: 25, topicId: "33333333-3333-4333-8333-333333333333" });
    await browser.$(".topic-stream-topic-trigger").click();
    await browser.$('[role="option"]=All Topics').click();
    await browser.$(".topic-stream-row").waitForDisplayed();
    assert.equal(publications().length, beforeOpen + 2);
    const unread = browser.$(".topic-stream-feed-filter").$("button=Unread");
    await unread.click();
    await browser.waitUntil(async () => (await unread.getAttribute("aria-pressed")) === "true");
    const row = browser.$(".topic-stream-row");
    await row.click();
    await browser.$(".topic-stream-detail-pane h1").waitForDisplayed();
    await browser.waitUntil(async () => (await row.getAttribute("data-reading-state")) === "read");
    await browser.$('button[aria-label="Back to list"]').click();
    await row.waitForDisplayed();
    assert.equal(await row.isFocused(), true);
    await browser.$('button[aria-label="Refresh feed"]').click();
    await browser.$("h2=No unread items").waitForDisplayed();
    assert.equal(publications().length, beforeOpen + 3);
    assert.equal(await browser.executeObsidian(({ plugins }) => plugins.topicStream.settings.preferences.feedFilter), "unread");
    await browser.executeObsidian(({ app }) => { for (const leaf of app.workspace.getLeavesOfType("topic-stream-view")) leaf.detach(); });
    await browser.waitUntil(async () => (await browser.$$(".topic-stream-root")).length === 0);
    await browser.executeObsidianCommand("topic-stream:open-topic-stream");
    await browser.$("h2=No unread items").waitForDisplayed();
    assert.equal(publications().length, beforeOpen + 4);
    assert.deepEqual(await browser.execute(() => window.topicStreamTestErrors), []);
  });

  it("renders native Markdown without vault writes, withholds every image and generates Summary only explicitly", async () => {
    await browser.executeObsidian(({ plugins }) => {
      window.topicStreamCodeProcessorCalls = 0;
      plugins.topicStream.registerMarkdownCodeBlockProcessor("topic-stream-fixture", () => { window.topicStreamCodeProcessorCalls++; });
    });
    const filesBefore = await browser.executeObsidian(({ app }) => app.vault.getMarkdownFiles().map((file) => file.path).sort());
    const imageRequests = () => second.requests.filter((request) => request.path.includes("/images/"));
    const summaryRequests = () => second.requests.filter((request) => request.method === "POST" && request.path.endsWith("/ai-summary"));
    assert.equal(imageRequests().length, 0);
    assert.equal(summaryRequests().length, 0);
    await browser.$('[role="tab"][data-section="search"]').click();
    await browser.$(".topic-stream-row").waitForDisplayed();
    await browser.$(".topic-stream-row").click();
    await browser.$(".topic-stream-prose h2").waitForDisplayed();
    assert.equal(await browser.$(".topic-stream-prose h2").getText(), "Canonical source");
    assert.equal(await browser.$(".topic-stream-prose strong").getText(), "native emphasis");
    assert.equal(await browser.execute(() => window.topicStreamCodeProcessorCalls), 0);
    assert.equal(await browser.$$(".topic-stream-prose img,.topic-stream-prose .internal-embed").length, 0);
    const firstImage = browser.$('.topic-stream-detail-pane button.topic-stream-image-placeholder');
    await firstImage.waitForDisplayed();
    assert.equal(imageRequests().length, 0);
    await firstImage.click();
    await browser.$('img[alt="First diagram"]').waitForDisplayed();
    await browser.$('img[alt="Second diagram"]').waitForDisplayed();
    await browser.waitUntil(async () => browser.execute(() => Array.from(document.querySelectorAll('.topic-stream-prose img')).every((image) => image.naturalWidth > 0)));
    assert.equal(imageRequests().length, 2);
    assert.equal(summaryRequests().length, 0);
    await browser.$('button=AI summary').click();
    await browser.$('.topic-stream-summary-text').waitForDisplayed();
    assert.equal(await browser.$('.topic-stream-summary-text').getText(), "An explicit synthetic summary from the native test service.");
    assert.equal(summaryRequests().length, 1);
    await browser.$('button[aria-label="Back to list"]').click();
    await browser.$('.topic-stream-row').click();
    await browser.$('img[alt="First diagram"]').waitForDisplayed();
    assert.equal(imageRequests().length, 2);
    assert.equal(summaryRequests().length, 1);
    assert.deepEqual(await browser.executeObsidian(({ app }) => app.vault.getMarkdownFiles().map((file) => file.path).sort()), filesBefore);
    await browser.executeObsidian(({ app }) => { for (const leaf of app.workspace.getLeavesOfType("topic-stream-view")) leaf.detach(); });
    await browser.waitUntil(async () => (await browser.$$(".topic-stream-root")).length === 0);
    assert.deepEqual(await browser.execute(() => window.topicStreamTestErrors), []);
  });

  it("reads captured editions and builds a Manual-only definition through native Task controls", async () => {
    const files = await browser.executeObsidian(({ app }) => app.vault.getMarkdownFiles().map((file) => file.path).sort());
    await browser.executeObsidianCommand("topic-stream:open-topic-stream");
    await browser.$('[role="tab"][data-section="digests"]').click();
    const digests = browser.$('[id$="-digests-panel"]');
    await digests.$('.topic-stream-row').waitForDisplayed();
    const readsBefore = second.requests.filter((request) => request.path.endsWith("/reading-state")).length;
    await digests.$('.topic-stream-row').click();
    await digests.$('h1=Saved native edition').waitForDisplayed();
    assert.equal(second.requests.filter((request) => request.path.endsWith("/reading-state")).length, readsBefore);
    await digests.$('button=Second backend source article').click();
    await digests.$('.topic-stream-prose strong').waitForDisplayed();
    assert.equal(await digests.$('.topic-stream-prose strong').getText(), "native emphasis");
    await digests.$('button[aria-label="Back to issue"]').click();
    await digests.$('h1=Saved native edition').waitForDisplayed();
    await digests.$('button[aria-label="Back to archive"]').click();
    await digests.$('button[aria-label="Build a Digest Issue"]').click();
    await browser.$('[role="menuitem"]=Build Native reading · Manual only').click();
    await digests.$('p=Building…').waitForDisplayed();
    const build = second.requests.find((request) => request.method === "POST" && request.path.endsWith("/generate"));
    assert.match(build.headers["idempotency-key"], /^[0-9a-f-]{36}$/);
    await digests.$('button[aria-label="View build task for Native reading"]').click();
    const tasks = browser.$('[aria-label="Tasks"][role="region"]');
    await tasks.$('h1=Build Digest Issue').waitForDisplayed();
    await tasks.$('button[aria-label="Cancel task"]').click();
    await browser.$('dialog[aria-label="Cancel this task?"]').$('button=Request cancellation').click();
    await tasks.$('.topic-stream-detail-pane').$('p=Cancellation requested').waitForDisplayed();
    assert.deepEqual(await browser.executeObsidian(({ app }) => app.vault.getMarkdownFiles().map((file) => file.path).sort()), files);
    await browser.executeObsidian(({ app }) => { for (const leaf of app.workspace.getLeavesOfType("topic-stream-view")) leaf.detach(); });
    await browser.waitUntil(async () => (await browser.$$(".topic-stream-root")).length === 0);
    assert.deepEqual(await browser.execute(() => window.topicStreamTestErrors), []);
  });


  it("saves explicit Compact and Full Items with real metadata, attachments and overwrite protection", async () => {
    await browser.executeObsidian(async ({ plugins }) => { await plugins.topicStream.settings.savePreferences((current) => ({ ...current, snapshot: { content: "compact", images: "links" } })); });
    await browser.executeObsidianCommand("topic-stream:open-topic-stream");
    await browser.$('[role="tab"][data-section="search"]').click();
    const search = browser.$('[id$="-search-panel"]');
    const back = search.$('button[aria-label="Back to list"]');
    if (await back.isDisplayed()) await back.click();
    const trigger = search.$('.topic-stream-list-pane button[aria-label^="More actions for"]');
    await trigger.waitForDisplayed();
    const generations = second.requests.filter((request) => request.method === "POST" && request.path.endsWith("/ai-summary")).length;
    const reads = second.requests.filter((request) => request.path.endsWith("/reading-state")).length;
    await trigger.click(); await browser.$('[role="menuitem"]=AI summary').waitForDisplayed();
    await browser.$('[role="menuitem"]=Save to vault').click();
    const dialog = browser.$('dialog[aria-label="Save to vault"]');
    await dialog.waitForDisplayed();
    assert.equal(await browser.executeObsidian(({ app }) => Boolean(app.vault.getAbstractFileByPath("Reading/Snapshots/Native compact.md"))), false);
    await dialog.$('input[name="folder"]').setValue("Reading//Snapshots/");
    await dialog.$('input[name="filename"]').setValue("Native compact");
    await dialog.$('button=Save').click();
    await dialog.waitForDisplayed({ reverse: true });
    const compact = await browser.executeObsidian(async ({ app }) => app.vault.read(app.vault.getFileByPath("Reading/Snapshots/Native compact.md")));
    assert.match(compact, /topic_stream_schema: 1\ntopic_stream_kind: item/);
    assert.match(compact, /A production-shell reading fixture/);
    assert.match(compact, /## AI summary\n\nAn explicit synthetic summary/);
    assert.ok(!compact.includes("Full content rendered"));
    await browser.waitUntil(async () => browser.executeObsidian(({ app }) => app.metadataCache.getFileCache(app.vault.getFileByPath("Reading/Snapshots/Native compact.md"))?.frontmatter?.topic_stream_schema === 1));
    await trigger.click(); await browser.$('[role="menuitem"]=Save to vault').click();
    await dialog.$('input[name="folder"]').setValue("Reading/Snapshots");
    await dialog.$('input[name="filename"]').setValue("Native compact");
    await dialog.$('button=Save').click();
    await dialog.$('[role="alert"]').waitForDisplayed();
    assert.match(await dialog.$('[role="alert"]').getText(), /already exists/);
    assert.equal(await browser.executeObsidian(async ({ app }) => app.vault.read(app.vault.getFileByPath("Reading/Snapshots/Native compact.md"))), compact);
    await dialog.$('input[name="filename"]').setValue("Native full");
    await dialog.$('input[value="full"]').click(); await dialog.$('input[value="local"]').click();
    await dialog.$('button=Save').click(); await dialog.waitForDisplayed({ reverse: true });
    const full = await browser.executeObsidian(async ({ app }) => ({ text: await app.vault.read(app.vault.getFileByPath("Reading/Snapshots/Native full.md")), images: app.vault.getFiles().filter((file) => file.path.startsWith("Reading/Snapshots/Native full.assets/")).map((file) => file.path) }));
    assert.match(full.text, /Full content rendered by Obsidian/);
    assert.match(full.text, /!\[First diagram\]\(<Native%20full.assets\//);
    assert.equal(full.images.length, 2);
    assert.equal(second.requests.filter((request) => request.method === "POST" && request.path.endsWith("/ai-summary")).length, generations);
    assert.equal(second.requests.filter((request) => request.path.endsWith("/reading-state")).length, reads);
    assert.equal(await browser.execute(() => window.topicStreamCodeProcessorCalls), 0);
    assert.deepEqual(await browser.execute(() => window.topicStreamTestErrors), []);
  });

  it("rolls back new native attachments on note failure and retains per-vault choices after reload", async () => {
    const trigger = browser.$('[id$="-search-panel"] .topic-stream-list-pane button[aria-label^="More actions for"]');
    await trigger.click(); await browser.$('[role="menuitem"]=Save to vault').click();
    const dialog = browser.$('dialog[aria-label="Save to vault"]');
    assert.equal(await dialog.$('input[value="full"]').isSelected(), true);
    assert.equal(await dialog.$('input[value="local"]').isSelected(), true);
    await dialog.$('input[name="folder"]').setValue("Reading/Snapshots");
    await dialog.$('input[name="filename"]').setValue("Failing attempt");
    await browser.executeObsidian(({ app }) => {
      window.topicStreamOriginalCreate = app.vault.create;
      app.vault.create = function(path, ...args) { if (path === "Reading/Snapshots/Failing attempt.md") return Promise.reject(new Error("Test note creation failure")); return window.topicStreamOriginalCreate.call(this, path, ...args); };
    });
    try {
      await dialog.$('button=Save').click();
      await dialog.$('[role="alert"]').waitForDisplayed();
      assert.match(await dialog.$('[role="alert"]').getText(), /Test note creation failure/);
      const state = await browser.executeObsidian(({ app }) => ({ note: Boolean(app.vault.getAbstractFileByPath("Reading/Snapshots/Failing attempt.md")), files: app.vault.getFiles().filter((file) => file.path.startsWith("Reading/Snapshots/Failing attempt.assets/")).length, kept: Boolean(app.vault.getFileByPath("Reading/Snapshots/Native full.md")) }));
      assert.deepEqual(state, { note: false, files: 0, kept: true });
    } finally { await browser.executeObsidian(({ app }) => { app.vault.create = window.topicStreamOriginalCreate; delete window.topicStreamOriginalCreate; }); }
    await dialog.$('button=Cancel').click();
    await obsidianPage.disablePlugin("topic-stream"); await obsidianPage.enablePlugin("topic-stream");
    assert.deepEqual(await browser.executeObsidian(({ plugins }) => plugins.topicStream.settings.preferences.snapshot), { content: "full", images: "local" });
    assert.deepEqual(await browser.execute(() => window.topicStreamTestErrors), []);
  });

  it("requires mixed Full consent and preserves captured Digest text and AI annotations in a native snapshot", async () => {
    const saved = second.issue.sections[0].items[0];
    saved.aiSummary = { ...second.summary, sourceVersion: saved.item.summarySourceVersion, canGenerate: false, unavailableReason: "historical_input", result: { ...second.summary.result, sourceVersion: saved.item.summarySourceVersion, text: "A captured historical annotation." } };
    second.item.contentMarkdown = "New canonical content must not replace the saved edition.";
    second.item.summarySourceVersion = "b".repeat(64);
    second.issue.sections[0].items.push({ ...structuredClone(saved), item: { ...structuredClone(saved.item), id: "77777777-7777-4777-8777-777777777777", title: "Description-only saved Item", contentMarkdown: "Only a description", contentOrigin: "article_description", summarySupported: false, images: [] }, summary: "A captured description excerpt.", aiSummary: null });
    await browser.executeObsidianCommand("topic-stream:open-topic-stream");
    await browser.$('[role="tab"][data-section="digests"]').click();
    const digests = browser.$('[id$="-digests-panel"]');
    await digests.$('.topic-stream-row').click();
    await digests.$('button[aria-label="Issue actions"]').click(); await browser.$('[role="menuitem"]=Save to vault').click();
    const dialog = browser.$('dialog[aria-label="Save to vault"]');
    await dialog.$('input[name="folder"]').setValue("Reading/Snapshots");
    await dialog.$('input[name="filename"]').setValue("Native edition");
    await dialog.$('button=Save').click(); await dialog.$('[role="alert"]').waitForDisplayed();
    assert.equal(await browser.executeObsidian(({ app }) => Boolean(app.vault.getAbstractFileByPath("Reading/Snapshots/Native edition.md"))), false);
    await dialog.$('input[name="confirmMixed"]').click(); await dialog.$('button=Save').click();
    await dialog.waitForDisplayed({ reverse: true });
    const text = await browser.executeObsidian(async ({ app }) => app.vault.read(app.vault.getFileByPath("Reading/Snapshots/Native edition.md")));
    assert.match(text, /topic_stream_kind: digest_issue/);
    assert.match(text, /## Systems\n\n### Second backend source article/);
    assert.match(text, /Full content rendered by Obsidian/);
    assert.match(text, /#### AI summary\n\nA captured historical annotation/);
    assert.match(text, /Excerpt only\n\nA captured description excerpt/);
    assert.ok(!text.includes("New canonical content"));
    await browser.executeObsidian(({ app }) => { for (const leaf of app.workspace.getLeavesOfType("topic-stream-view")) leaf.detach(); });
    await browser.waitUntil(async () => (await browser.$$(".topic-stream-root")).length === 0);
    assert.deepEqual(await browser.execute(() => window.topicStreamTestErrors), []);
  });

  it("owns real SSE across views, reconnect, pop-out migration and unload without automatic writes or duplicate Notices", async () => {
    await browser.waitUntil(() => second.streams.size === 0);
    await browser.executeObsidian(async ({ plugins }) => plugins.topicStream.settings.savePreferences((current) => ({ ...current, feedFilter: "all" })));
    await browser.executeObsidianCommand("topic-stream:open-topic-stream");
    await browser.$('.topic-stream-root').waitForDisplayed();
    await browser.$('[role="tab"][data-section="feed"]').click();
    await browser.$('[id$="-feed-panel"] .topic-stream-row').waitForDisplayed();
    await browser.waitUntil(() => second.streams.size === 1);
    const writes = () => second.requests.filter((request) => request.method === "POST").length;
    const beforeWrites = writes();
    const feedReads = () => second.requests.filter((request) => request.method === "GET" && request.path === "/api/v1/feed");
    const beforeFeed = feedReads().length;
    second.emit("feedInvalidated", { itemId: second.item.id });
    await browser.waitUntil(() => feedReads().length > beforeFeed);
    assert.equal(feedReads().at(-1).query.snapshot, "test-snapshot");
    assert.equal(writes(), beforeWrites);
    await browser.$('button[aria-label="Tasks"]').click();
    const backTasks = browser.$('button[aria-label="Back to tasks"]');
    if (await backTasks.isDisplayed()) await backTasks.click();
    await browser.$('[id$="-tasks-panel"] .topic-stream-row').waitForDisplayed();
    await browser.$('[id$="-tasks-panel"] .topic-stream-row').click();
    await browser.$('.topic-stream-task-detail').waitForDisplayed();
    const jobReads = () => second.requests.filter((request) => request.path === `/api/v1/jobs/${second.job.id}`).length;
    const beforeJob = jobReads();
    second.job.attempt = 2;
    for (let index = 0; index < 20; index++) second.emit("jobChanged", { jobId: second.job.id });
    await browser.waitUntil(() => jobReads() > beforeJob);
    await browser.waitUntil(async () => (await browser.$('.topic-stream-task-facts').getText()).includes("2 / 5"));
    // Drop an advisory. Reconnect must recover the authoritative value without replay.
    second.job.attempt = 3; second.disconnect();
    await browser.waitUntil(async () => (await browser.$('.topic-stream-task-facts').getText()).includes("3 / 5"), { timeout: 10000 });
    const beforeMalformed = second.requests.filter((request) => request.path === "/api/v1/events").length;
    for (const stream of second.streams) stream.write('event: jobChanged\ndata: not-json\n\n');
    await browser.waitUntil(() => second.streams.size === 1 && second.requests.filter((request) => request.path === "/api/v1/events").length > beforeMalformed);
    second.streaming.available = false; second.disconnect();
    await browser.waitUntil(() => second.streams.size === 0);
    assert.ok((await browser.$('.topic-stream-task-facts').getText()).includes("3 / 5"));
    second.job.attempt = 4;
    await browser.$('button[aria-label="Refresh selected task"]').click();
    await browser.waitUntil(async () => (await browser.$('.topic-stream-task-facts').getText()).includes("4 / 5"));
    second.streaming.available = true;
    await browser.waitUntil(() => second.streams.size === 1, { timeout: 10000 });
    const archiveReads = () => second.requests.filter((request) => request.method === "GET" && request.path === "/api/v1/digest-issues").length;
    const beforeSecondView = archiveReads();
    await browser.executeObsidian(async ({ app }) => { const leaf = app.workspace.getLeaf("split"); await leaf.setViewState({ type: "topic-stream-view", active: true }); });
    await browser.waitUntil(() => second.streams.size === 2 && archiveReads() > beforeSecondView);
    // Publish only after the new view has read existing history and rendered a frame.
    await browser.execute(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    const live = { ...structuredClone(second.issue), id: "99999999-9999-4999-8999-999999999991", title: "Live streaming edition", generatedAt: "2026-10-29T09:00:00Z" };
    second.issues.unshift(live);
    second.emit("digestReady", { issueId: live.id }); second.emit("digestReady", { issueId: live.id });
    await browser.waitUntil(() => browser.execute(() => Array.from(document.querySelectorAll('.notice')).some((node) => node.textContent.includes("Digest ready: Live streaming edition"))));
    const notices = await browser.execute(() => Array.from(document.querySelectorAll('.notice')).filter((node) => node.textContent.includes("Digest ready: Live streaming edition")).length);
    assert.equal(notices, 1);
    const opens = second.requests.filter((request) => request.path === "/api/v1/events").length;
    const moved = await browser.executeObsidian(({ app }) => {
      const leaf = app.workspace.getLeavesOfType("topic-stream-view")[0];
      const root = leaf.view.contentEl.querySelector('.topic-stream-root');
      app.workspace.moveLeafToPopout(leaf, { width: 520, height: 640 });
      return Boolean(root);
    });
    assert.equal(moved, true);
    await browser.waitUntil(() => second.streams.size === 2 && second.requests.filter((request) => request.path === "/api/v1/events").length > opens);
    const realms = await browser.executeObsidian(({ app }) => app.workspace.getLeavesOfType("topic-stream-view").map((leaf) => ({ popup: leaf.view.contentEl.win !== window, roots: leaf.view.contentEl.querySelectorAll('.topic-stream-root').length })));
    assert.equal(realms.filter((realm) => realm.popup).length, 1);
    assert.ok(realms.every((realm) => realm.roots === 1));
    const layout = await browser.execute(async () => {
      const before = window.topicStreamHostResizeNotifications.length;
      await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));
      return { before, after: window.topicStreamHostResizeNotifications.length };
    });
    assert.equal(layout.after, layout.before, "Host layout must settle after migration");
    assert.ok(layout.after <= 2, "Host resize notifications must stay bounded");
    assert.equal(writes(), beforeWrites);
    await obsidianPage.disablePlugin("topic-stream");
    await browser.waitUntil(() => second.streams.size === 0);
    await obsidianPage.enablePlugin("topic-stream");
    await browser.executeObsidian(({ app }) => { for (const leaf of app.workspace.getLeavesOfType("topic-stream-view")) leaf.detach(); });
    assert.deepEqual(await browser.execute(() => window.topicStreamTestErrors), []);
  });
});
