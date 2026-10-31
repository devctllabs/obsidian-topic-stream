import { useQuery } from "@tanstack/react-query";
import type { Schema } from "../../api/contracts";
import { IconButton } from "../../shared/components/IconButton";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
export function BackendStatus({ service }: { service: { getSystemStatus: () => Promise<Schema<"SystemStatus">> } }) {
  const query = useQuery({ queryKey: ["system-status"], queryFn: () => service.getSystemStatus() });
  return <div><div className="topic-stream-section-header"><h2>Backend status</h2><IconButton icon="refresh" label="Refresh backend status" disabled={query.isFetching} onClick={() => { void query.refetch(); }} /></div>
    {query.isPending && <p role="status">Checking backend…</p>}{query.isError && <ApiErrorMessage title="Could not read backend status" error={query.error} />}
    {query.data && <StatusDetails value={query.data} />}
  </div>;
}
function StatusDetails({ value }: { value: Schema<"SystemStatus"> }) {
  return <><p role="status">{labels[value.status]} · Topic Stream {value.version}</p><p className="topic-stream-metadata">Updated {new Date(value.now).toLocaleString("en")}</p>
    <ul className="topic-stream-source-rows">{value.components.map((component) => <li key={component.name}><div><h3>{component.name}</h3><p className="topic-stream-metadata">{labels[component.status]}{component.detail ? ` · ${component.detail}` : ""}</p></div></li>)}</ul>
    <p className="topic-stream-metadata">Optional AI can be unavailable while saved Feed history, Settings and Jobs remain usable. Connection preferences are in Obsidian’s plugin settings.</p>
  </>;
}
const labels = { ready: "Ready", degraded: "Degraded", unavailable: "Unavailable" };
