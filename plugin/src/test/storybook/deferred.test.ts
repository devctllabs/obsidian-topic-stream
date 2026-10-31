import { describe, expect, it, vi } from "vitest";

import { deferred } from "./deferred";

describe("deterministic asynchronous fixture boundary", () => {
  it("stays pending until explicitly settled and delivers only one result", async () => {
    const pending = deferred<string>();
    const received = vi.fn();
    void pending.promise.then(received);
    await Promise.resolve();
    expect(received).not.toHaveBeenCalled();
    pending.resolve("ready");
    pending.resolve("late duplicate");
    await expect(pending.promise).resolves.toBe("ready");
    expect(received).toHaveBeenCalledExactlyOnceWith("ready");
  });

  it("can fail without timers or automatic retry", async () => {
    const pending = deferred<string>();
    const failure = new Error("Controlled fixture failure");
    const result = expect(pending.promise).rejects.toBe(failure);
    pending.reject(failure);
    await result;
  });
});
