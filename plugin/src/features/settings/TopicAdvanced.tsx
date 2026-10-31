import type { UseFormReturn } from "react-hook-form";
import type { ItemKind, TopicInput } from "../../api/contracts";

export function TopicAdvanced({ form, selectedKinds, mode, exclude, enabled }: {
  form: UseFormReturn<TopicInput>; selectedKinds: ItemKind[]; mode: TopicInput["mode"]; exclude: string; enabled: boolean;
}) {
  const details = [exclude ? "Exclusions" : "", mode === "strict" ? "Strict matching" : "", selectedKinds.length ? `${selectedKinds.length} kinds` : "", !enabled ? "Paused" : ""].filter(Boolean);
  return <details><summary>Advanced{details.length > 0 && <span className="topic-stream-metadata"> · {details.join(", ")}</span>}</summary>
    <div className="topic-stream-advanced-fields">
      <label>Exclude<textarea rows={3} maxLength={4000} {...form.register("exclude")} /></label>
      <label>Matching<select {...form.register("mode")}><option value="ordinary">Ordinary</option><option value="strict">Strict</option></select></label>
      <div><span className="topic-stream-metadata">Item kinds · None selected includes all kinds</span>
        {kinds.map(([kind, label]) => <label className="topic-stream-checkbox" key={kind}><input type="checkbox" checked={selectedKinds.includes(kind)} onChange={(event) => form.setValue("itemKinds", event.target.checked ? [...selectedKinds, kind] : selectedKinds.filter((value) => value !== kind), { shouldDirty: true })} />{label}</label>)}
      </div>
      <label className="topic-stream-checkbox"><input type="checkbox" {...form.register("enabled")} />Enabled</label>
    </div>
  </details>;
}
const kinds: Array<readonly [ItemKind, string]> = [["article", "Articles"], ["paper", "Papers"], ["video", "Videos"], ["repository", "Repositories"]];
