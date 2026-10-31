import { useState } from "react";
import type { Source } from "../../api/contracts";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import { IconButton } from "../../shared/components/IconButton";

export function SourcePicker({ sources, selected, loading, error, onRetry, onChange, onBack, onAddSource }: {
  sources: Source[]; selected: string[]; loading: boolean; error: Error | null; onRetry: () => void; onChange: (value: string[]) => void; onBack: () => void; onAddSource: () => void;
}) {
  const [search, setSearch] = useState("");
  const filtered = sources.filter((source) => `${source.name} ${source.config.kind}`.toLowerCase().includes(search.toLowerCase()));
  return <section className="topic-stream-settings-form">
    <div className="topic-stream-section-header"><h2>Sources</h2><IconButton icon="back" label="Back to topic" onClick={onBack} /></div>
    <button type="button" className="topic-stream-refresh" aria-pressed={selected.length === 0} onClick={() => onChange([])}>All sources</button>
    <p className="topic-stream-metadata">Choose specific Sources to limit this Topic.</p>
    <input type="search" aria-label="Search sources" placeholder="Search sources" autoFocus value={search} onChange={(event) => setSearch(event.target.value)} />
    <PickerStatus loading={loading} error={error} empty={filtered.length === 0} onRetry={onRetry} />
    <div className="topic-stream-source-picker-list">{filtered.map((source) => <label key={source.id} className="topic-stream-checkbox">
      <input type="checkbox" checked={selected.includes(source.id)} onChange={(event) => onChange(event.target.checked ? [...selected, source.id] : selected.filter((id) => id !== source.id))} />{source.name}
    </label>)}</div>
    {!loading && !error && <UnavailableSources sources={sources} selected={selected} onChange={onChange} />}
    <button type="button" className="topic-stream-refresh" onClick={onAddSource}>Add source</button>
  </section>;
}

function PickerStatus({ loading, error, empty, onRetry }: { loading: boolean; error: Error | null; empty: boolean; onRetry: () => void }) {
  if (loading) return <p role="status">Loading sources…</p>;
  if (error) return <div><ApiErrorMessage title="Could not load sources" error={error} /><button type="button" className="topic-stream-refresh" onClick={onRetry}>Try again</button></div>;
  return empty ? <p className="topic-stream-metadata">No matching sources.</p> : null;
}
function UnavailableSources({ sources, selected, onChange }: { sources: Source[]; selected: string[]; onChange: (value: string[]) => void }) {
  const unknown = selected.filter((id) => !sources.some((source) => source.id === id));
  return <>{unknown.map((id, index) => <label className="topic-stream-checkbox" key={id}><input type="checkbox" checked onChange={() => onChange(selected.filter((selectedId) => selectedId !== id))} />Unavailable source {index + 1}</label>)}</>;
}
