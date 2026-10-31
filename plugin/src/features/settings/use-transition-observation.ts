import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import type { WorkspaceStatePort } from "./device-state";
import type { WorkspaceSettingsService } from "./services";
import type { TransitionNotifications } from "./transition-notifications";

/** A persisted deliberate transition remains observable when Settings has never been mounted. */
export function useTransitionObservation(client: WorkspaceSettingsService, storage: WorkspaceStatePort, notifications: TransitionNotifications): Error | null {
  const [watching, setWatching] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  useEffect(() => {
    let active = true;
    const read = () => { void storage.read().then((value) => { if (active) setWatching(Boolean(value.pendingTransition || Object.keys(value.watchedTransitions).length)); }, () => { if (active) setError(new Error("Could not read saved model completion watch.")); }); };
    const unsubscribe = storage.subscribe(read); read();
    return () => { active = false; unsubscribe(); };
  }, [storage]);
  const status = useQuery({ queryKey: ["retrieval-status"], queryFn: () => client.getRetrievalStatus(), enabled: watching, staleTime: Infinity, retry: false, refetchOnWindowFocus: false, refetchOnReconnect: false });
  const jobId = status.data?.latestTransition?.jobId ?? status.data?.transitionJobId;
  const job = useQuery({ queryKey: ["job", jobId], queryFn: () => client.getJob(jobId!), enabled: watching && Boolean(jobId), staleTime: Infinity, retry: false, refetchOnWindowFocus: false, refetchOnReconnect: false });
  useEffect(() => {
    if (!watching || !status.data) return;
    let active = true;
    void notifications.observe(status.data, job.data).then(() => { if (active) setError(null); }, () => { if (active) setError(new Error("Could not reconcile the model completion notice.")); });
    return () => { active = false; };
  }, [watching, status.data, job.data, notifications]);
  return error;
}
