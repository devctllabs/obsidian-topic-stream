import { describe, expect, it } from "vitest";
import { ShellNavigation } from "./navigation";

describe("reading context navigation", () => {
  it("returns from management to the previous section and keeps each selection", () => {
    const navigation = new ShellNavigation();
    navigation.open("digests");
    navigation.select("digests", "saved-issue");
    navigation.open("settings");
    navigation.open("tasks");
    navigation.back();
    expect(navigation.getSnapshot()).toEqual({ primary: "digests", screen: "digests", selections: { digests: "saved-issue" } });
    navigation.open("feed");
    navigation.select("feed", "reading-item");
    navigation.open("digests");
    expect(navigation.getSnapshot().selections).toEqual({ digests: "saved-issue", feed: "reading-item" });
  });

  it("restores a normalized saved section, selection and independent scroll positions", () => {
    const first = new ShellNavigation();
    first.open("search");
    first.select("search", "an-item");
    first.rememberPosition("search:list", 360);
    first.rememberPosition("search:detail:an-item", 720);
    const restored = new ShellNavigation(first.exportState());
    expect(restored.getSnapshot()).toEqual(first.getSnapshot());
    expect(restored.position("search:list")).toBe(360);
    expect(restored.position("search:detail:an-item")).toBe(720);
    restored.restore({ primary: "unknown", screen: "settings", selections: { search: 4 }, positions: { bad: -1 } });
    expect(restored.getSnapshot()).toEqual({ primary: "feed", screen: "settings", selections: {} });
    expect(restored.position("bad")).toBe(0);
  });

  it("keeps a dirty editor open until its owner approves the requested navigation", () => {
    const navigation = new ShellNavigation();
    navigation.open("settings");
    let leave: (() => void) | undefined;
    const unsubscribe = navigation.setLeaveGuard((proceed) => { leave = proceed; });
    navigation.open("feed");
    expect(navigation.getSnapshot().screen).toBe("settings");
    expect(leave).toBeDefined();
    leave?.();
    expect(navigation.getSnapshot().screen).toBe("feed");
    unsubscribe();
    navigation.open("search");
    expect(navigation.getSnapshot().screen).toBe("search");
  });
  it("opens the exact accepted Job and returns to the previous reading context", () => {
    const navigation = new ShellNavigation();
    navigation.open("digests");
    navigation.select("digests", "issue");
    navigation.openTask("accepted-job");
    expect(navigation.getSnapshot()).toEqual({ primary: "digests", screen: "tasks", selections: { digests: "issue", tasks: "accepted-job" } });
    const restored = new ShellNavigation(navigation.exportState());
    expect(restored.getSnapshot().selections.tasks).toBe("accepted-job");
    restored.back();
    expect(restored.getSnapshot().screen).toBe("digests");
  });
  it("asks the nested editor before its parent and keeps the parent guard after the child closes", () => {
    const navigation = new ShellNavigation({ screen: "settings" });
    const calls: string[] = [];
    let leaveParent: (() => void) | undefined;
    let leaveChild: (() => void) | undefined;
    navigation.setLeaveGuard((next) => { calls.push("parent"); leaveParent = next; });
    const closeChild = navigation.setLeaveGuard((next) => { calls.push("child"); leaveChild = next; });
    navigation.open("feed");
    expect(calls).toEqual(["child"]);
    closeChild();
    leaveChild?.();
    expect(calls).toEqual(["child", "parent"]);
    expect(navigation.getSnapshot().screen).toBe("settings");
    leaveParent?.();
    expect(navigation.getSnapshot().screen).toBe("feed");
  });
});
