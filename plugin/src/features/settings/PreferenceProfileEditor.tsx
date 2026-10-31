import type { Schema } from "../../api/contracts";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type SyntheticEvent } from "react";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import { ApiError } from "../../api/errors";
import type { ShellNavigation } from "../shell/navigation";
import { EditorExitDialog, useEditorExit } from "./editor-exit";

import type { PreferenceProfileService } from "./services";
export type { PreferenceProfileService } from "./services";

export function PreferenceProfileEditor({ service, navigation }: { service: PreferenceProfileService; navigation?: ShellNavigation | undefined }) {
  const query = useQuery({ queryKey: ["preference-profile"], queryFn: () => service.getPreferenceProfile() });
  if (query.data) return <ProfileForm service={service} initial={query.data} navigation={navigation} />;
  if (query.isError) return <section><ApiErrorMessage title="Could not load preference profile" error={query.error} /><button type="button" className="topic-stream-refresh" onClick={() => { void query.refetch(); }}>Try again</button></section>;
  return <p role="status" className="topic-stream-metadata">Loading preference profile…</p>;
}

function ProfileForm({ service, initial, navigation }: { service: PreferenceProfileService; initial: Schema<"PreferenceProfile">; navigation?: ShellNavigation | undefined }) {
  const queries = useQueryClient();
  const [base, setBase] = useState(initial);
  const [draft, setDraft] = useState({ prefer: initial.prefer, avoid: initial.avoid });
  const dirty = draft.prefer !== base.prefer || draft.avoid !== base.avoid;
  const accept = (value: Schema<"PreferenceProfile">, keepDraft = false) => {
    setBase(value);
    if (!keepDraft) setDraft({ prefer: value.prefer, avoid: value.avoid });
    queries.setQueryData(["preference-profile"], value);
  };
  const save = useMutation({
    mutationFn: () => service.updatePreferenceProfile(draft, base.version),
    onSuccess: (value) => accept(value)
  });
  const submit = (event: SyntheticEvent<HTMLFormElement>) => { event.preventDefault(); if (dirty && !save.isPending) save.mutate(); };
  const exit = useEditorExit({ navigation, dirty,
    save: async () => { try { await save.mutateAsync(); return true; } catch { return false; } },
    discard: () => setDraft({ prefer: base.prefer, avoid: base.avoid })
  });
  const conflict = isProfileConflict(save.error);
  return <><form className="topic-stream-settings-form" onSubmit={submit}>
    <h2>Preference profile</h2>
    <p className="topic-stream-metadata">Guidance for optional Digest reranking. Topic criteria and Feed feedback are managed separately.</p>
    <fieldset disabled={save.isPending}>
      <label>Prefer<textarea rows={4} maxLength={12000} value={draft.prefer} onChange={(event) => setDraft({ ...draft, prefer: event.target.value })} /></label>
      <label>Avoid<textarea rows={4} maxLength={12000} value={draft.avoid} onChange={(event) => setDraft({ ...draft, avoid: event.target.value })} /></label>
    </fieldset>
    <div className="topic-stream-form-actions"><button type="submit" className="topic-stream-primary-action" disabled={!dirty || save.isPending}>Save changes</button>
      <span role="status" className="topic-stream-metadata">{save.isPending ? "Saving…" : save.isSuccess && !dirty ? "Saved" : ""}</span>
    </div>
    {save.isError && <ApiErrorMessage title={conflict ? "Profile changed on the server. Your draft is kept." : "Could not save preference profile"} error={save.error} />}
    {conflict && <ProfileConflict service={service} onKeepDraft={(value) => { accept(value, true); save.reset(); }} onUseSaved={(value) => { accept(value); save.reset(); }} />}
  </form><EditorExitDialog exit={exit} saving={save.isPending} resource="preference profile" /></>;
}

function ProfileConflict({ service, onKeepDraft, onUseSaved }: {
  service: PreferenceProfileService; onKeepDraft: (value: Schema<"PreferenceProfile">) => void; onUseSaved: (value: Schema<"PreferenceProfile">) => void;
}) {
  const latest = useMutation({ mutationFn: () => service.getPreferenceProfile() });
  const value = latest.data;
  if (value) return <section className="topic-stream-profile-review" aria-label="Latest saved profile">
    <h3>Saved version {value.version}</h3>
    <dl><dt>Prefer</dt><dd>{value.prefer || "Empty"}</dd><dt>Avoid</dt><dd>{value.avoid || "Empty"}</dd></dl>
    <div className="topic-stream-form-actions">
      <button type="button" className="topic-stream-refresh" onClick={() => onKeepDraft(value)}>Keep my draft</button>
      <button type="button" className="topic-stream-refresh" onClick={() => onUseSaved(value)}>Use saved values</button>
    </div>
  </section>;
  return <div><button type="button" className="topic-stream-refresh" disabled={latest.isPending} onClick={() => latest.mutate()}>Review latest</button>
    {latest.isError && <ApiErrorMessage title="Could not load latest profile" error={latest.error} />}
  </div>;
}

function isProfileConflict(error: unknown): boolean {
  return error instanceof ApiError && (error.status === 412 || error.status === 409);
}
