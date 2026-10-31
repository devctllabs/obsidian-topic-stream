import { useId, useLayoutEffect, useRef, useSyncExternalStore, type ComponentType, type KeyboardEvent } from "react";
import { IconButton } from "../../shared/components/IconButton";
import { ShellNavigation, isPrimary, type PrimarySection, type ShellScreen } from "./navigation";

export interface SectionProps {
  navigation: ShellNavigation;
  section: PrimarySection;
  active: boolean;
}
export interface TopicStreamShellProps {
  navigation: ShellNavigation;
  screens: Record<ShellScreen, ComponentType<SectionProps>>;
  unseenIssues?: number;
}

export function TopicStreamShell({ navigation, screens, unseenIssues = 0 }: TopicStreamShellProps) {
  const state = useSyncExternalStore(navigation.subscribe, navigation.getSnapshot);
  const id = useId();
  const header = useRef<HTMLElement>(null);
  const previousScreen = useRef(state.screen);
  useLayoutEffect(() => {
    if (!isPrimary(previousScreen.current) && isPrimary(state.screen)) header.current?.querySelector<HTMLButtonElement>(`[data-section="${state.primary}"]`)?.focus();
    previousScreen.current = state.screen;
  }, [state.screen, state.primary]);
  return <main className="topic-stream-root topic-stream-shell">
    <header className="topic-stream-shell-header" ref={header}>
      <span className="topic-stream-brand">Topic Stream</span>
      <nav aria-label="Reading sections" role="tablist" className="topic-stream-primary-nav" onKeyDown={(event) => moveTab(event, navigation)}>
        {primarySections.map((section) => <button key={section} type="button" role="tab" data-section={section}
          id={`${id}-${section}-tab`} aria-controls={`${id}-${section}-panel`} aria-selected={state.primary === section}
          tabIndex={state.primary === section ? 0 : -1} onClick={() => navigation.open(section)}>{labels[section]}{section === "digests" && unseenIssues > 0 && <span className="topic-stream-unseen-count" aria-label={`${unseenIssues} unseen ${unseenIssues === 1 ? "issue" : "issues"}`}>{unseenIssues}</span>}</button>)}
      </nav>
      <div className="topic-stream-utilities">
        <IconButton icon="tasks" label="Tasks" aria-pressed={state.screen === "tasks"} onClick={() => navigation.open("tasks")} />
        <IconButton icon="settings" label="Settings" aria-pressed={state.screen === "settings"} onClick={() => navigation.open("settings")} />
      </div>
    </header>
    {!isPrimary(state.screen) && <div className="topic-stream-management-back"><IconButton icon="back" label="Back to reading" onClick={() => navigation.back()} /></div>}
    {allScreens.map((screen) => <ScreenSlot key={screen} screen={screen} active={state.screen === screen} id={id}
      component={screens[screen]} navigation={navigation} section={isPrimary(screen) ? screen : state.primary} />)}
  </main>;
}

const primarySections: PrimarySection[] = ["feed", "digests", "search"];
const allScreens: ShellScreen[] = [...primarySections, "settings", "tasks"];
const labels: Record<ShellScreen, string> = { feed: "Feed", digests: "Digests", search: "Search", settings: "Settings", tasks: "Tasks" };

function moveTab(event: KeyboardEvent<HTMLElement>, navigation: ShellNavigation): void {
  const keys: Record<string, number> = { ArrowRight: 1, ArrowLeft: -1, Home: 0, End: 2 };
  const direction = keys[event.key];
  if (direction === undefined) return;
  event.preventDefault();
  const current = primarySections.indexOf(navigation.getSnapshot().primary);
  const index = event.key === "Home" || event.key === "End" ? direction : (current + direction + 3) % 3;
  const section = primarySections[index];
  if (section === undefined) return;
  navigation.open(section);
  event.currentTarget.querySelector<HTMLButtonElement>(`[data-section="${section}"]`)?.focus();
}

function ScreenSlot({ screen, active, id, component: Screen, ...props }: SectionProps & {
  screen: ShellScreen; id: string; component: ComponentType<SectionProps>;
}) {
  const primary = isPrimary(screen);
  return <section className="topic-stream-screen" hidden={!active} role={primary ? "tabpanel" : "region"}
    id={`${id}-${screen}-panel`} aria-labelledby={primary ? `${id}-${screen}-tab` : undefined}
    aria-label={primary ? undefined : labels[screen]}>
    {props.navigation.hasVisited(screen) && <Screen {...props} active={active} />}
  </section>;
}
