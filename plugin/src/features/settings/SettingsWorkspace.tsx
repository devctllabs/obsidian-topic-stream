import type { TransitionNotifications } from "./transition-notifications";
import type { JobActions } from "../../shared/jobs/job-actions";
import { useEffect, useState, useSyncExternalStore } from "react";
import type { ShellNavigation, SettingsSection } from "../shell/navigation";
import type { PreferenceProfileService, TopicsWorkspaceService, DigestDefinitionService, WorkspaceSettingsService } from "./services";
import { PreferenceProfileEditor } from "./PreferenceProfileEditor";
import { SourcesSection, type SourcesWorkspaceService } from "./SourcesSection";
import { SourceActions } from "./source-actions";
import { TopicsSection } from "./TopicsSection";

import { BackendStatus } from "./BackendStatus";
import { WorkspaceSettingsEditor } from "./WorkspaceSettingsEditor";
import { DigestDefinitions } from "./DigestDefinitions";
import type { DigestBuilds } from "./digest-builds";

export function SettingsWorkspace({ client, navigation, builds, jobs, transitions }: { client: SourcesWorkspaceService & TopicsWorkspaceService & PreferenceProfileService & DigestDefinitionService & WorkspaceSettingsService & Pick<import("../../api/client").TopicStreamClient, "getSystemStatus">; navigation: ShellNavigation; builds: DigestBuilds; jobs: JobActions; transitions: TransitionNotifications }) {
  const state = useSyncExternalStore(navigation.subscribe, navigation.getSnapshot);
  const section = state.settingsSection ?? "sources";
  const [sourceActions] = useState(() => new SourceActions(client));
  useEffect(() => () => sourceActions.dispose(), [sourceActions]);
  return <div className="topic-stream-page topic-stream-settings-page"><div className="topic-stream-settings-heading"><h1>Settings</h1>
    <label className="topic-stream-section-picker"><span>Settings section</span><select value={section} onChange={(event) => {
      const next = settingsSections.find(([key]) => key === event.target.value)?.[0];
      if (next) navigation.openSettings(next);
    }}>{settingsSections.map(([key, label]) => <option key={key} value={key}>{label}</option>)}</select></label></div>
    <div className="topic-stream-settings-layout"><nav className="topic-stream-settings-nav" aria-label="Settings sections">
      {settingsSections.map(([key, label]) => <button key={key} type="button" aria-current={section === key ? "page" : undefined} onClick={() => navigation.openSettings(key)}>{label}</button>)}
    </nav>
    <section aria-label={sectionLabels[section]}>
      {section === "sources" && <SourcesSection service={client} actions={sourceActions} navigation={navigation} />}
      {section === "topics" && <TopicsSection client={client} navigation={navigation} sourceActions={sourceActions} />}
      {section === "workspace" && <WorkspaceSettingsEditor transitions={transitions} jobs={jobs} service={client} navigation={navigation} />}
      {section === "digests" && <DigestDefinitions service={client} builds={builds} navigation={navigation} />}
      {section === "preferences" && <PreferenceProfileEditor service={client} navigation={navigation} />}
      {section === "status" && <BackendStatus service={client} />}
    </section></div>
  </div>;
}
const settingsSections = [["sources", "Sources"], ["topics", "Topics"], ["digests", "Digest definitions"], ["workspace", "Workspace"], ["preferences", "Preferences"], ["status", "Status"]] as const;
const sectionLabels: Record<SettingsSection, string> = { sources: "Sources", topics: "Topics", digests: "Digest definitions", workspace: "Workspace settings", preferences: "Preference profile", status: "Backend status" };
