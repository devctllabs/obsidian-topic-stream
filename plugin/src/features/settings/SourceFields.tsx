import type { UseFormReturn } from "react-hook-form";
import type { Schema, SourceInput } from "../../api/contracts";
import { SettingsTextField } from "./SettingsTextField";

type SourceForm = UseFormReturn<SourceInput>;
export function SourceAdapterFields({ config, form }: { config: Schema<"SourceConfig">; form: SourceForm }) {
  if (config.kind === "rss" || config.kind === "atom") return <SettingsTextField label="Feed URL" type="url" placeholder="https://example.org/feed" binding={form.register("config.url")} error={form.getFieldState("config.url", form.formState).error} />;
  if (config.kind === "arxiv") return <SettingsTextField label="arXiv query" placeholder="cat:cs.IR" maxLength={500} binding={form.register("config.query")} error={form.getFieldState("config.query", form.formState).error} />;
  if (config.kind === "youtube") return <SettingsTextField label="Channel ID" placeholder="UC…" binding={form.register("config.channelId")} error={form.getFieldState("config.channelId", form.formState).error} />;
  return <>
    <label>Period<select {...form.register("config.period")}><option value="daily">Daily</option><option value="weekly">Weekly</option><option value="monthly">Monthly</option></select></label>
    <label>Language<select {...form.register("config.language")}>
      <option value="all">All languages</option><option value="python">Python</option><option value="rust">Rust</option><option value="go">Go</option><option value="javascript">JavaScript</option><option value="typescript">TypeScript</option>
    </select></label>
  </>;
}

export function SourceAdvanced({ config, form }: { config: Schema<"SourceConfig">; form: SourceForm }) {
  return <details><summary>Advanced</summary><div className="topic-stream-advanced-fields">
    <SettingsTextField label="Poll interval (minutes)" type="number" min={15} max={10080} binding={form.register("pollIntervalMinutes", { valueAsNumber: true })} error={form.formState.errors.pollIntervalMinutes} />
    <label className="topic-stream-checkbox"><input type="checkbox" {...form.register("enabled")} />Enabled</label>
    {(config.kind === "rss" || config.kind === "atom") && <label className="topic-stream-checkbox"><input type="checkbox" {...form.register("config.allowPrivateNetwork")} />Allow private network fetches</label>}
  </div></details>;
}
