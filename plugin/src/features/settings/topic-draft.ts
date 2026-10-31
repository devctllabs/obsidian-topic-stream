import { z } from "zod";
import type { Topic, TopicInput } from "../../api/contracts";

export const topicSchema = z.object({
  name: z.string().trim().min(1, "Enter a name.").max(80),
  include: z.string().max(4000), exclude: z.string().max(4000),
  sourceIds: z.array(z.uuid()), itemKinds: z.array(z.enum(["article", "paper", "video", "repository"])),
  mode: z.enum(["ordinary", "strict"]), enabled: z.boolean()
}) satisfies z.ZodType<TopicInput>;

export function topicDraft(topic: Topic | null): TopicInput {
  if (!topic) return { name: "", include: "", exclude: "", sourceIds: [], itemKinds: [], mode: "ordinary", enabled: true };
  return { name: topic.name, include: topic.include, exclude: topic.exclude, sourceIds: topic.sourceIds, itemKinds: topic.itemKinds, mode: topic.mode, enabled: topic.enabled };
}
