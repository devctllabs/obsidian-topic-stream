import { useQuery } from "@tanstack/react-query";
import { findSavedIssue } from "../digests/saved-result";
import type { DigestsService } from "../digests/services";
import type { ShellNavigation } from "../shell/navigation";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";

export function TaskSavedIssue({ client, jobId, navigation }: { client: DigestsService; jobId: string; navigation: ShellNavigation }) {
  const result = useQuery({ queryKey: ["digest-result", jobId], queryFn: ({ signal }) => findSavedIssue(client, jobId, signal), retry: false, staleTime: Infinity, refetchOnWindowFocus: false, refetchOnReconnect: false });
  if (result.data) return <button type="button" className="topic-stream-refresh" onClick={() => navigation.openIssue(result.data!.id)}>Open saved issue</button>;
  return <>{result.isPending ? <p role="status" className="topic-stream-metadata">Looking for the saved issue…</p> : <><p className="topic-stream-metadata">The saved issue is not available in the archive.</p><button type="button" className="topic-stream-refresh" onClick={() => { void result.refetch(); }}>Check saved issue</button></>}{result.error && <ApiErrorMessage title="Could not check the saved issue" error={result.error} />}</>;
}
