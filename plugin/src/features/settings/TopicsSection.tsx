import { EditorPane } from "./EditorPane";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useLayoutEffect, useRef, useState } from "react";
import type { Topic, TopicInput } from "../../api/contracts";
import { ActionMenu } from "../../shared/components/ActionMenu";
import { ApiErrorMessage } from "../../shared/components/ApiErrorMessage";
import type { ShellNavigation } from "../shell/navigation";
import type { TopicsWorkspaceService } from "./services";
import type { SourceActions } from "./source-actions";
import { TopicEditor } from "./TopicEditor";
import { TopicRow } from "./TopicRow";
import { TopicDeleteDialog } from "./TopicDeleteDialog";
import { cacheTopic } from "./topic-cache";

export function TopicsSection({ client, navigation, sourceActions }: { client: TopicsWorkspaceService; navigation: ShellNavigation; sourceActions: SourceActions }) {
  const queries = useQueryClient();
  const topics = useQuery({ queryKey: ["topics"], queryFn: () => client.listTopics() });
  const [editing, setEditing] = useState<Topic | null | undefined>();
  const [deleting, setDeleting] = useState<Topic | null>(null);
  const add = useRef<HTMLButtonElement>(null);
  const list = useRef<HTMLUListElement>(null);
  const restore = useRef<string | null>(null);
  useLayoutEffect(() => {
    if (editing !== undefined || deleting || !restore.current) return;
    const trigger = Array.from(list.current?.querySelectorAll<HTMLButtonElement>("[data-topic-actions]") ?? []).find((button) => button.dataset.topicActions === restore.current);
    (trigger ?? add.current)?.focus({ preventScroll: true });
    restore.current = null;
  }, [editing, deleting]);
  const remove = useMutation({
    mutationFn: (topic: Topic) => client.deleteTopic(topic.id),
    onSuccess: async (_value, topic) => {
      await queries.cancelQueries({ queryKey: ["topics"] });
      queries.setQueryData<Topic[]>(["topics"], (current = []) => current.filter((value) => value.id !== topic.id));
      void queries.invalidateQueries({ queryKey: ["digests"] });
      restore.current = "add";
      setDeleting(null);
    }
  });
  const save = async (input: TopicInput) => {
    const topic = editing ? await client.updateTopic(editing.id, input) : await client.createTopic(input);
    await cacheTopic(queries, topic);
    restore.current = topic.id;
    setEditing(undefined);
  };
  return <>
    <div className="topic-stream-resource-list" inert={editing !== undefined} aria-hidden={editing !== undefined}>
    <div className="topic-stream-resource-header"><h2>Topics</h2><div className="topic-stream-form-actions"><button ref={add} type="button" className="topic-stream-primary-action" onClick={() => { restore.current = "add"; setEditing(null); }}>Add topic</button>
      <ActionMenu label="More topic actions" menuLabel="Topic collection actions" items={[{ id: "refresh", label: "Refresh topics", disabled: topics.isFetching, onSelect: () => { void topics.refetch(); } }]} />
    </div></div>
    {topics.isPending && <p role="status" className="topic-stream-metadata">Loading topics…</p>}
    {topics.isError && <ApiErrorMessage title="Could not load topics" error={topics.error} />}
    {topics.data?.length === 0 && <p className="topic-stream-metadata">Add a Topic to define what you want to read.</p>}
    <ul className="topic-stream-source-rows" aria-label="Topics" ref={list}>{topics.data?.map((topic) => <TopicRow key={topic.id} topic={topic} service={client}
      onEdit={(value) => { restore.current = value.id; setEditing(value); }}
      onDelete={(value) => { restore.current = value.id; remove.reset(); setDeleting(value); }} />)}</ul>
    </div>
    {editing !== undefined && <EditorPane label="Topic editor"><TopicEditor key={editing?.id ?? "new"} topic={editing} client={client} navigation={navigation} sourceActions={sourceActions} onSave={save} onCancel={() => setEditing(undefined)} /></EditorPane>}
    {deleting && <TopicDeleteDialog topic={deleting} pending={remove.isPending} error={remove.error} onConfirm={() => remove.mutate(deleting)} onCancel={() => setDeleting(null)} />}
  </>;
}
