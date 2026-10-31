import { FIXTURE_TIME, fixtureId, makeDigestDefinition, makeEntry, makeIssue, makeJob, makeSource, makeSummary, makeTopic } from "./fixtures";
import { interactiveScenario } from "./interactive-scenario";
import type { FixtureScenario } from "./scenario";

/** Authored synthetic catalog content; the screens and their composition are production code. */
export function showcaseScenario() {
  const scenario = interactiveScenario();
  scenario.workspaceSettings.generation = { enabled: true, endpoint: "https://generation.example/v1", summaryModel: "fixture-summary-model", rerankModel: "fixture-rerank-model" };
  scenario.collections.sources = ["Systems journal", "Retrieval notes", "Cuaderno de ingeniería", "Open source dispatch", "Research notebook", "The reading room"]
    .map((name, index) => makeSource(index, { name }));
  scenario.collections.topics = ["Retrieval systems", "Software craft", "Engineering notes", "Open source", "Research", "Reading practice"]
    .map((name, index) => makeTopic(index, { name }));
  scenario.collections.entries = Array.from({ length: 30 }, (_, index) => {
    const [title, excerpt] = reading[index % reading.length]!;
    const diagram = { id: fixtureId(800 + index), url: `https://images.example/reading-${index}.svg`, altText: "From source to useful results" };
    return makeEntry(index, { title, excerpt, authors: [index % 3 === 2 ? "Elena García" : "Alex Morgan"], sourceIds: [fixtureId(100 + index % 6)], topicIds: [fixtureId(200 + index % 6)],
      contentMarkdown: `## A question worth keeping\n\n${excerpt}\n\nUseful reading begins with a small question. Keep the original source close, look for the assumptions behind its argument, and leave room for a different interpretation.\n\n## From reading to practice\n\nA good note preserves the question as well as the answer. Choose one observation to test in your own work, then return to the source when the context changes.\n\n![${diagram.altText}](${diagram.url})\n\n## Para volver a pensar\n\nConserva las preguntas que siguen abiertas. Una colección útil conecta ideas y deja espacio para descubrir algo distinto.`, images: [diagram] });
  });
  scenario.digests = [makeDigestDefinition({ name: "The weekly reading list" }), makeDigestDefinition({ id: fixtureId(901), name: "Notas de ingeniería", topics: [{ topicId: fixtureId(202), itemLimit: 5 }] })];
  scenario.issue = authoredIssue(scenario, fixtureId(900));
  scenario.issues = [scenario.issue];
  const readIssue = scenario.getDigestIssue;
  scenario.getDigestIssue = async (id) => {
    const issue = await readIssue(id);
    for (const section of issue.sections) for (const saved of section.items) if (saved.aiSummary) saved.aiSummary = { ...saved.aiSummary, canGenerate: saved.aiSummary.supported && scenario.workspaceSettings.generation.enabled, unavailableReason: scenario.workspaceSettings.generation.enabled ? null : "generation_disabled" };
    return issue;
  };
  return Object.assign(scenario, { dispose: installBuildDemo(scenario) });
}

function authoredIssue(scenario: FixtureScenario, digestId: string) {
  const issue = { ...makeIssue([]), digestId, title: scenario.digests.find((definition) => definition.id === digestId)!.name };
  issue.sections = scenario.collections.topics.slice(0, 4).map((topic) => ({ topicId: topic.id, topicName: topic.name, topicRevision: topic.revision, failure: null,
    items: scenario.collections.entries.filter(({ item }) => item.topicIds.includes(topic.id)).slice(0, 3).map((entry) => ({ item: structuredClone(entry.item), summary: entry.item.excerpt, explanation: entry.explanation, aiSummary: makeSummary({ sourceVersion: entry.item.summarySourceVersion }) })) }));
  return issue;
}

/** A simulated external worker owns its timers; the primary story tears it down with the view. */
function installBuildDemo(scenario: FixtureScenario): () => void {
  const timers = new Set<number>();
  const accepted = new Map<string, string>();
  const later = (action: () => void, delay: number) => { const timer = window.setTimeout(() => { timers.delete(timer); action(); }, delay); timers.add(timer); };
  scenario.generateDigest = (digestId, requestId) => {
    const previous = accepted.get(requestId);
    if (previous) return Promise.resolve({ jobId: previous });
    if (!scenario.digests.some((definition) => definition.id === digestId)) return Promise.reject(new Error("Saved definition unavailable."));
    const job = makeJob({ id: fixtureId(700 + scenario.jobs.size), kind: "generate-digest", queue: "generation", status: "queued" });
    scenario.jobs.set(job.id, job); accepted.set(requestId, job.id);
    later(() => { scenario.jobs.set(job.id, { ...job, status: "running" }); scenario.events.emit({ type: "jobChanged", jobId: job.id }); }, 300);
    later(() => {
      const issue = { ...authoredIssue(scenario, digestId), id: fixtureId(10000 + scenario.jobs.size), generationJobId: job.id, generatedAt: new Date(Date.parse(FIXTURE_TIME) + 3600000 + scenario.jobs.size * 1000).toISOString() };
      issue.title += ` · ${new Intl.DateTimeFormat("en", { month: "long", day: "numeric", timeZone: "UTC" }).format(Date.parse(issue.generatedAt))}`; scenario.issues.unshift(issue);
      scenario.jobs.set(job.id, { ...job, status: "succeeded", durationMs: 900, finishedAt: issue.generatedAt, updatedAt: issue.generatedAt });
      scenario.events.emit({ type: "jobChanged", jobId: job.id }); scenario.events.emit({ type: "digestReady", issueId: issue.id });
    }, 900);
    return Promise.resolve({ jobId: job.id });
  };
  return () => { for (const timer of timers) window.clearTimeout(timer); timers.clear(); };
}

const reading = [
  ["A closer look at retrieval systems", "A useful search result starts with a clear question. Notes on relevance, evaluation, and the tradeoffs behind a reliable retrieval pipeline."],
  ["Small tools, carefully made", "What changes when a tool does one thing well? A field guide to keeping software useful as its scope grows."],
  ["Notas sobre una búsqueda más útil", "Cómo evaluar la relevancia y construir una colección que invite a volver. Apuntes sobre búsqueda y lectura."],
  ["The quiet work of maintaining software", "Good maintenance makes the next change easier. A practical look at boundaries, tests, and decisions that age well."],
  ["Reading beyond the abstract", "A repeatable way to turn a promising paper into questions, experiments, and notes you can use."],
  ["Designing a durable reading workspace", "From saved links to a working collection: keeping context close without adding more steps to the day."],
  ["Un cuaderno para pensar mejor", "Una práctica sencilla para conectar ideas, conservar preguntas y encontrar de nuevo lo que merece atención."],
  ["Measure the system you actually use", "Evaluation belongs beside the real workflow. Start with observed failures, then choose the measurements that explain them."]
] as const;
