import { describe, expect, it, vi } from "vitest";

import { ControlledAction, FixtureEvents } from "./fake-services";

describe("controllable feature service ports", () => {
  it("supports out-of-order old/current responses without starting work on construction", async () => {
    const action = new ControlledAction<{ selection: number }, string>();
    expect(action.calls).toEqual([]);
    const old = action.invoke({ selection: 1 });
    const current = action.invoke({ selection: 2 });
    action.calls[1]?.response.resolve("current");
    await expect(current).resolves.toBe("current");
    action.calls[0]?.response.resolve("old");
    await expect(old).resolves.toBe("old");
    expect(action.calls.map(({ input }) => input.selection)).toEqual([1, 2]);
  });

  it("delivers only explicit advisories and stops after unsubscribe", () => {
    const events = new FixtureEvents();
    const received = vi.fn();
    const unsubscribe = events.subscribe(received);
    expect(received).not.toHaveBeenCalled();
    events.emit({ type: "jobChanged", jobId: "job" });
    expect(received).toHaveBeenCalledExactlyOnceWith({ type: "jobChanged", jobId: "job" });
    unsubscribe();
    events.emit({ type: "digestReady", issueId: "issue" });
    expect(received).toHaveBeenCalledTimes(1);
  });
});
