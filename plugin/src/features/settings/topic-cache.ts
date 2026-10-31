import type { QueryClient } from "@tanstack/react-query";
import type { Topic } from "../../api/contracts";

export async function cacheTopic(queries: QueryClient, topic: Topic): Promise<void> {
  await queries.cancelQueries({ queryKey: ["topics"] });
  queries.setQueryData<Topic[]>(["topics"], (current = []) => current.some((value) => value.id === topic.id)
    ? current.map((value) => value.id === topic.id ? topic : value) : [...current, topic]);
  void queries.invalidateQueries({ queryKey: ["topics"] });
}
