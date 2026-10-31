export type PrimarySection = "feed" | "digests" | "search";
export type ShellScreen = PrimarySection | "settings" | "tasks";
export type SelectionOwner = PrimarySection | "tasks";
export type SettingsSection = "sources" | "topics" | "digests" | "workspace" | "preferences" | "status";
export interface NavigationState {
  primary: PrimarySection;
  screen: ShellScreen;
  selections: Partial<Record<SelectionOwner, string>>;
  settingsSection?: SettingsSection;
}

export class ShellNavigation {
  private state: NavigationState = { primary: "feed", screen: "feed", selections: {} };
  private readonly listeners = new Set<() => void>();
  private positions: Record<string, number> = {};
  private visitedScreens = new Set<ShellScreen>();
  private readonly leaveGuards = new Set<(proceed: () => void) => void>();

  public constructor(value?: unknown) { this.restore(value); }

  public getSnapshot = (): NavigationState => this.state;
  public subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => { this.listeners.delete(listener); };
  };

  public open(screen: ShellScreen): void {
    if (screen === this.state.screen) return;
    this.attempt(() => {
      this.visitedScreens.add(screen);
      this.update({ ...this.state, screen, primary: isPrimary(screen) ? screen : this.state.primary });
    });
  }

  public select(section: SelectionOwner, id: string | null): void {
    const selections = { ...this.state.selections };
    if (id === null) delete selections[section];
    else selections[section] = id;
    this.update({ ...this.state, selections });
  }

  public back(): void { this.open(this.state.primary); }
  public openSettings(section: SettingsSection): void {
    this.attempt(() => {
      this.visitedScreens.add("settings");
      this.update({ ...this.state, screen: "settings", settingsSection: section });
    });
  }
  public openTask(id: string): void {
    this.attempt(() => {
      this.visitedScreens.add("tasks");
      this.update({ ...this.state, screen: "tasks", selections: { ...this.state.selections, tasks: id } });
    });
  }
  public openIssue(id: string): void {
    this.attempt(() => {
      this.visitedScreens.add("digests");
      this.update({ ...this.state, screen: "digests", primary: "digests", selections: { ...this.state.selections, digests: id } });
    });
  }
  public hasVisited(screen: ShellScreen): boolean { return this.visitedScreens.has(screen); }
  public setLeaveGuard(guard: (proceed: () => void) => void): () => void {
    this.leaveGuards.add(guard);
    return () => { this.leaveGuards.delete(guard); };
  }
  public attempt(proceed: () => void): void {
    const guards = [...this.leaveGuards].reverse();
    const advance = (index: number): void => {
      const guard = guards[index];
      if (!guard) { proceed(); return; }
      if (!this.leaveGuards.has(guard)) { advance(index + 1); return; }
      guard(() => advance(index + 1));
    };
    advance(0);
  }

  public position(key: string): number { return this.positions[key] ?? 0; }
  public rememberPosition(key: string, value: number): void {
    this.positions[key] = value;
  }
  public exportState(): NavigationState & { positions: Record<string, number> } {
    return { ...this.state, positions: { ...this.positions } };
  }
  public restore(value: unknown): void {
    const input = record(value);
    const primary = isPrimary(input.primary) ? input.primary : "feed";
    const screen = isPrimary(input.screen) || input.screen === "settings" || input.screen === "tasks" ? input.screen : primary;
    this.visitedScreens = new Set([screen]);
    const selections = Object.fromEntries(Object.entries(record(input.selections)).filter(([key, id]) => (isPrimary(key) || key === "tasks") && typeof id === "string"));
    this.positions = Object.fromEntries(Object.entries(record(input.positions)).filter((entry): entry is [string, number] => typeof entry[1] === "number" && Number.isFinite(entry[1]) && entry[1] >= 0));
    this.update({ primary, screen, selections, ...(isSettingsSection(input.settingsSection) ? { settingsSection: input.settingsSection } : {}) });
  }

  private update(state: NavigationState): void {
    this.state = state;
    for (const listener of this.listeners) listener();
  }
}

function record(value: unknown): Record<string, unknown> {
  return typeof value === "object" && value !== null ? value as Record<string, unknown> : {};
}

export function isPrimary(value: unknown): value is PrimarySection {
  return value === "feed" || value === "digests" || value === "search";
}
function isSettingsSection(value: unknown): value is SettingsSection { return typeof value === "string" && ["sources", "topics", "digests", "workspace", "preferences", "status"].includes(value); }
