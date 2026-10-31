import { useQuery } from "@tanstack/react-query";
import type { Schema } from "../../api/contracts";
import type { TasksService } from "./services";

export function TaskModelContext({ job, client }: { job: Schema<"Job">; client: TasksService }) {
  const options = useQuery({ queryKey: ["embedding-model-options"], queryFn: () => client.listEmbeddingModelOptions(), staleTime: Infinity, retry: false, refetchOnWindowFocus: false, refetchOnReconnect: false });
  const status = useQuery({ queryKey: ["retrieval-status"], queryFn: () => client.getRetrievalStatus(), enabled: !job.modelTransition, staleTime: Infinity, retry: false, refetchOnWindowFocus: false, refetchOnReconnect: false });
  const modelId = targetModelId(job, status.data);
  if (!modelId) return null;
  const model = options.data?.find(({ model }) => model.id === modelId)?.model;
  return <p className="topic-stream-metadata">Target model: {model?.name ?? modelId}</p>;
}
function targetModelId(job: Schema<"Job">, status: Schema<"RetrievalStatus"> | undefined): string | null {
  if (job.modelTransition) return job.modelTransition.modelId;
  const active = ["queued", "running", "retry_wait"].includes(job.status);
  return active && status?.transitionJobId === job.id ? status.desiredModelId : null;
}
