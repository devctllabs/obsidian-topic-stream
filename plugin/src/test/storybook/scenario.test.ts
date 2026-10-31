import { describe, expect, it } from "vitest";

import { FixtureScenario } from "./scenario";
import { makeItem, makeJob, makeSource, makeSummary } from "./fixtures";

describe("feature story service capabilities", () => {
  it("keeps a created Source when its separate initial Sync fails", async () => {
    const scenario = new FixtureScenario();
    const source = makeSource(25);
    const created = scenario.createSource({ name: source.name, config: source.config, enabled: true, pollIntervalMinutes: 60 });
    scenario.sourceCreation.calls[0]?.response.resolve(source);
    await created;
    const sync = scenario.syncAllSources();
    const failed = expect(sync).rejects.toThrow("Synthetic sync failure");
    scenario.sourceSync.calls[0]?.response.reject(new Error("Synthetic sync failure"));
    await failed;
    expect(await scenario.listSources()).toContainEqual(source);
  });

  it("reads disabled ready summaries independently of generating or downloading images", () => {
    const scenario = new FixtureScenario();
    const item = makeItem();
    const summary = makeSummary({ state: "ready", canGenerate: false, unavailableReason: "generation_disabled" });
    scenario.summaries.set(`${item.id}:${item.summarySourceVersion}`, summary);
    expect(scenario.readSummary(item).result?.text).toContain("Synthetic AI summary");
    expect(scenario.summaryGeneration.calls).toEqual([]);
    expect(scenario.imageDownload.calls).toEqual([]);
  });

  it("withholds generation for unsupported abstract, description and missing input", () => {
    const scenario = new FixtureScenario();
    for (const contentOrigin of ["paper_abstract", "article_description", "empty"] as const) {
      const summary = scenario.readSummary(makeItem(0, { contentOrigin, summarySupported: false }));
      expect(summary).toMatchObject({ supported: false, canGenerate: false, unavailableReason: "unsupported_input" });
    }
    expect(scenario.summaryGeneration.calls).toEqual([]);
  });

  it("lets counters, terminal status, retained cohorts and advisory delivery vary independently", () => {
    const scenario = new FixtureScenario();
    const current = makeJob({ progress: { phase: "indexing", processedItems: 10, totalItems: 10, updatedAt: "2026-10-27T09:00:00Z" } });
    scenario.jobs.set(current.id, current);
    expect(scenario.jobs.get(current.id)?.status).toBe("running");
    scenario.jobs.set("new-recovery-job", makeJob({ id: "new-recovery-job", progress: { phase: "preparing", processedItems: 0, totalItems: null, updatedAt: "2026-10-27T09:00:00Z" } }));
    expect(scenario.jobs.get(current.id)?.progress?.processedItems).toBe(10);
    expect(scenario.jobAction.calls).toEqual([]);
  });
});
