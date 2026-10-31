import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { Topic } from "../../api/contracts";
import { ActionMenu } from "../../shared/components/ActionMenu";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import type { TopicEditingService } from "./services";
import { cacheTopic } from "./topic-cache";

export function TopicRow({ topic, service, onEdit, onDelete }: { topic: Topic; service: TopicEditingService; onEdit: (value: Topic) => void; onDelete: (value: Topic) => void }) {
  const queries = useQueryClient();
  const toggle = useMutation({ mutationFn: () => service.updateTopic(topic.id, { enabled: !topic.enabled }), onSuccess: (value) => cacheTopic(queries, value) });
  return <li><div className="topic-stream-source-row-content"><h3><button type="button" className="topic-stream-row-title-button" disabled={toggle.isPending} onClick={() => onEdit(topic)}>{topic.name}</button></h3>
    <p className="topic-stream-metadata">Revision {topic.revision} · {topic.enabled ? topic.mode : "Paused"}</p>
    <p className="topic-stream-metadata">{topic.include}</p>
    {toggle.isError && <ApiErrorMessage title="Could not update topic" error={toggle.error} />}
  </div><ActionMenu label={`More actions for ${topic.name}`} menuLabel={`Actions for ${topic.name}`} disabled={toggle.isPending} triggerProps={{ "data-topic-actions": topic.id }} items={[
    { id: "edit", label: "Edit", onSelect: () => onEdit(topic) },
    { id: "toggle", label: topic.enabled ? "Pause" : "Enable", onSelect: () => toggle.mutate() },
    { id: "delete", label: "Delete", destructive: true, separator: true, onSelect: () => onDelete(topic) }
  ]} /></li>;
}
