import { useEffect, useRef, useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { missingFullText, saveSnapshot, snapshotPath, type SnapshotResult, type SnapshotServices, type SnapshotSource } from "./snapshot";
import type { ClientPreferences } from "../settings/preferences";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";

export interface SnapshotPreferences { read: () => ClientPreferences["snapshot"]; save: (value: ClientPreferences["snapshot"]) => Promise<void>; }
export interface SnapshotFormProps { source: SnapshotSource; services: SnapshotServices; preferences: SnapshotPreferences; readSource?: () => SnapshotSource; onSaved: (result: SnapshotResult) => void; onCancel: () => void; }
const schema = z.object({ folder: z.string(), filename: z.string(), content: z.enum(["compact", "full"]), images: z.enum(["links", "remote", "local"]), confirmMixed: z.boolean() }).superRefine((value, context) => {
  try { snapshotPath(value.folder, value.filename); }
  catch (error) { context.addIssue({ code: "custom", path: ["filename"], message: error instanceof Error ? error.message : "Choose a valid vault path." }); }
});
type Values = z.infer<typeof schema>;

export function SnapshotForm({ source, services, preferences, readSource, onSaved, onCancel }: SnapshotFormProps) {
  const identity = source.kind === "item" ? source.item : source.issue;
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { folder: "", filename: defaultFilename(identity.title), ...preferences.read(), confirmMixed: false } });
  const [content, images] = useWatch({ control: form.control, name: ["content", "images"] });
  const [error, setError] = useState<unknown>(null);
  const pending = form.formState.isSubmitting;
  const dialog = useRef<HTMLDialogElement>(null);
  const [controller] = useState(() => new AbortController());
  useEffect(() => { dialog.current?.showModal(); return () => controller.abort(); }, [controller]);
  const missing = content === "full" ? missingFullText(source) : 0;
  const submit = async (values: Values) => {
    setError(null);
    if (missing && source.kind === "digest_issue" && !values.confirmMixed) { form.setError("confirmMixed", { message: "Confirm the mixed result or choose Compact." }); return; }
    try {
      const captured = structuredClone(readSource?.() ?? source);
      await preferences.save({ content: values.content, images: values.images });
      const result = await saveSnapshot(captured, values, services, controller.signal);
      if (!controller.signal.aborted) onSaved(result);
    } catch (cause) { if (!controller.signal.aborted) setError(cause); }
  };
  return <div className="topic-stream-root topic-stream-snapshot-layer"><dialog ref={dialog} className="topic-stream-dialog topic-stream-snapshot-dialog" aria-label="Save to vault" onCancel={(event) => { event.preventDefault(); if (!pending) onCancel(); }} onKeyDown={(event) => { if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); if (!pending) onCancel(); } }}>
    <h2>Save to vault</h2><p className="topic-stream-snapshot-title">{identity.title}</p>
    <form className="topic-stream-settings-form" onSubmit={(event) => { void form.handleSubmit(submit)(event); }}>
      <fieldset disabled={pending}>
        <label>Folder<input {...form.register("folder")} placeholder="Vault root" autoComplete="off" /></label>
        <label>Filename<input {...form.register("filename")} aria-invalid={Boolean(form.formState.errors.filename)} autoComplete="off" /></label>
      </fieldset>
      {form.formState.errors.filename && <p role="alert" className="topic-stream-inline-error">{form.formState.errors.filename.message}</p>}
      <ContentChoices form={form} content={content} images={images} missing={missing} individual={source.kind === "item"} pending={pending} />
      <p className="topic-stream-metadata">An independent Markdown note. Existing files are never replaced.</p>
      {error != null && <ApiErrorMessage title="Could not save snapshot" error={error} />}
      {pending && <p role="status" className="topic-stream-metadata">Preparing snapshot and saving to vault…</p>}
      <div className="topic-stream-form-actions"><button type="button" className="topic-stream-refresh" disabled={pending} onClick={onCancel}>Cancel</button><button type="submit" className="topic-stream-primary-action" disabled={pending || (source.kind === "item" && missing > 0)}>{pending ? "Saving…" : "Save"}</button></div>
    </form>
  </dialog></div>;
}
const imageHint = { links: "Clickable image links. No image downloads.", remote: "External image embeds. Opening the note may load images from their original sites.", local: "Portable attachments beside the note. Unavailable images stay links; up to 30 files, 10 MB each and 100 MB total." };
function defaultFilename(title: string): string { return title.replace(/[\p{Cc}/\\<>:"|?*#[\]^]/gu, " ").replace(/\s+/g, " ").trim().slice(0, 150) || "Snapshot"; }
function ContentChoices({ form, content, images, missing, individual, pending }: { form: import("react-hook-form").UseFormReturn<Values>; content: Values["content"]; images: Values["images"]; missing: number; individual: boolean; pending: boolean }) {
  return <><fieldset disabled={pending} className="topic-stream-snapshot-choice"><legend>Content</legend><div>
    {(["compact", "full"] as const).map((value) => <label key={value}><input type="radio" value={value} {...form.register("content")} />{value === "compact" ? "Compact" : "Full"}</label>)}
  </div></fieldset>
    <p className="topic-stream-metadata">{content === "compact" ? "Excerpts, source links and ready AI summaries." : "Available article text and READMEs, with ready AI summaries."}</p>
    {content === "full" && !(individual && missing > 0) && <><fieldset disabled={pending} className="topic-stream-snapshot-choice"><legend>Images</legend><div>
      {(["links", "remote", "local"] as const).map((value) => <label key={value}><input type="radio" value={value} {...form.register("images")} />{value[0]!.toUpperCase() + value.slice(1)}</label>)}
    </div></fieldset><p className="topic-stream-metadata">{imageHint[images]}</p></>}
    {missing > 0 && <FullTextConsent count={missing} individual={individual} pending={pending} field={form.register("confirmMixed")} onCompact={() => form.setValue("content", "compact")} />}
    {form.formState.errors.confirmMixed && <p role="alert" className="topic-stream-inline-error">{form.formState.errors.confirmMixed.message}</p>}
  </>;
}
function FullTextConsent({ count, individual, pending, field, onCompact }: { count: number; individual: boolean; pending: boolean; field: import("react-hook-form").UseFormRegisterReturn; onCompact: () => void }) {
  return <div className="topic-stream-snapshot-consent"><p>{individual ? "This Item has no article body or README. Its excerpt is available in Compact." : `${count} Item${count === 1 ? " has" : "s have"} no full text. Each will be labeled “Excerpt only”.`}</p>
    {!individual && <label className="topic-stream-checkbox-label"><input type="checkbox" disabled={pending} {...field} />Include these excerpts in the Full snapshot</label>}
    <button type="button" className="topic-stream-refresh" disabled={pending} onClick={onCompact}>Use Compact</button></div>;
}
