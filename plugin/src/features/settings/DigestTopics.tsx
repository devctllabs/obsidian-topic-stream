import type { Schema } from "../../api/contracts";
import { IconButton } from "../../shared/components/IconButton";

export function DigestTopics({ value, topics, onChange }: { value: Schema<"DigestTopicInput">[]; topics: Schema<"Topic">[]; onChange: (value: Schema<"DigestTopicInput">[]) => void }) {
  const move = (index: number, offset: number) => {
    const result = [...value];
    const target = index + offset;
    const entry = result[index];
    const other = result[target];
    if (!entry || !other) return;
    result[target] = entry; result[index] = other;
    onChange(result);
  };
  return <div><label>Add topic<select value="" onChange={(event) => { if (event.target.value) onChange([...value, { topicId: event.target.value, itemLimit: 5 }]); }}>
    <option value="">Choose a Topic</option>{topics.filter((topic) => !value.some((entry) => entry.topicId === topic.id)).map((topic) => <option value={topic.id} key={topic.id}>{topic.name}</option>)}
  </select></label>
  <ol className="topic-stream-digest-topics">{value.map((entry, index) => {
    const name = topics.find((topic) => topic.id === entry.topicId)?.name ?? "Unavailable topic";
    return <li key={entry.topicId}><span>{name}</span><label>Items<input aria-label={`Item limit for ${name}`} type="number" min={1} max={15} value={entry.itemLimit} onChange={(event) => onChange(value.map((item) => item.topicId === entry.topicId ? { ...item, itemLimit: Number(event.target.value) } : item))} /></label>
      <div className="topic-stream-row-tools"><IconButton icon="up" label={`Move ${name} up`} disabled={index === 0} onClick={() => move(index, -1)} /><IconButton icon="down" label={`Move ${name} down`} disabled={index === value.length - 1} onClick={() => move(index, 1)} /><IconButton icon="close" label={`Remove ${name}`} onClick={() => onChange(value.filter((item) => item.topicId !== entry.topicId))} /></div>
    </li>;
  })}</ol></div>;
}
