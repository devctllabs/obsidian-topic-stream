import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import { AdvisoryConnection } from "../advisory-connection";
import type { EventStreamRequest } from "../event-stream";
import { connectJobResources } from "./resources";
import { ItemSummaries } from "../../features/reader/item-summaries";
import { JobActions } from "./job-actions";
import { deferred } from "../../test/storybook/deferred";
import { FixtureScenario } from "../../test/storybook/scenario";
import { fixtureId, makeItem, makeJob, makeSummary } from "../../test/storybook/fixtures";
import type { Schema } from "../../api/contracts";

describe("authoritative resources behind the actual advisory connection", () => {
  it("shares in-flight Job/retrieval reads across owners and recovers dropped changes without generating work", async () => {
    const client = new FixtureScenario();
    const initial = deferred<Schema<"Job">>();
    const getJob = vi.spyOn(client, "getJob").mockImplementationOnce(() => initial.promise);
    const retrieval = vi.spyOn(client, "getRetrievalStatus");
    const summariesRead = vi.spyOn(client, "getItemSummary");
    const queries = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity, staleTime: Infinity } } });
    const streams: EventStreamRequest[] = [];
    const events = new AdvisoryConnection("https://backend.example", (request) => new Promise<void>((_resolve, reject) => {
      streams.push(request);
      request.signal.addEventListener("abort", () => reject(new DOMException("Closed", "AbortError")), { once: true });
    }));
    const stops: (() => void)[] = [];
    const observe = <T,>(queryKey: string[], queryFn: () => Promise<T>) => {
      const observer = new QueryObserver(queries, { queryKey, queryFn });
      stops.push(observer.subscribe(() => undefined));
      return observer;
    };
    // Settings and Task Center observe identical keys, including while the initial read is pending.
    const settings = observe(["job", fixtureId(400)], () => client.getJob(fixtureId(400)));
    const tasks = observe(["job", fixtureId(400)], () => client.getJob(fixtureId(400)));
    observe(["retrieval-status"], () => client.getRetrievalStatus());
    observe(["retrieval-status"], () => client.getRetrievalStatus());
    stops.push(connectJobResources(queries, events));
    const summaries = new ItemSummaries(queries, client, new JobActions(client));
    stops.push(summaries.connect(events));
    const item = makeItem();
    client.summaries.set(`${item.id}:${item.summarySourceVersion}`, makeSummary({ state: "pending", jobId: fixtureId(400) }));
    stops.push(summaries.subscribe(item, () => undefined));
    const send = (text: string) => streams.at(-1)!.receive(new TextEncoder().encode(text));
    const changed = `event: jobChanged\ndata: {"jobId":"${fixtureId(400)}"}\n\n`;
    send(": connected\n\n" + changed.repeat(20));
    await vi.waitFor(() => expect(summaries.getSnapshot(item)?.state).toBe("pending"));
    expect(getJob).toHaveBeenCalledTimes(1);
    initial.resolve(makeJob());
    await vi.waitFor(() => expect(tasks.getCurrentResult().isSuccess).toBe(true));
    const readCount = retrieval.mock.calls.length;
    const summaryCount = summariesRead.mock.calls.length;
    client.jobs.set(fixtureId(400), makeJob({ attempt: 2 }));
    send(changed.repeat(20));
    await vi.waitFor(() => expect(settings.getCurrentResult().data?.attempt).toBe(2));
    expect(getJob).toHaveBeenCalledTimes(2);
    expect(retrieval).toHaveBeenCalledTimes(readCount + 1);
    expect(summariesRead).toHaveBeenCalledTimes(summaryCount + 1);
    expect(tasks.getCurrentResult().data?.attempt).toBe(2);
    // The next phase is visible immediately; coalescing does not impose a one-second client throttle.
    client.jobs.set(fixtureId(400), makeJob({ attempt: 3 })); send(changed);
    await vi.waitFor(() => expect(tasks.getCurrentResult().data?.attempt).toBe(3));
    expect(getJob).toHaveBeenCalledTimes(3);
    client.jobs.set(fixtureId(400), makeJob({ status: "succeeded" }));
    events.reconnect(); send(": connected\n\n");
    await vi.waitFor(() => expect(tasks.getCurrentResult().data?.status).toBe("succeeded"));
    expect(getJob).toHaveBeenCalledTimes(4);
    expect(streams).toHaveLength(2);
    expect(client.summaryGeneration.calls).toEqual([]);
    expect(client.digestBuild.calls).toEqual([]);
    stops.reverse().forEach((stop) => stop()); events.dispose(); queries.clear();
    expect(streams.every((request) => request.signal.aborted)).toBe(true);
  });
});
