import type { QueryClient } from "@tanstack/react-query";
import type { Source } from "../../api/contracts";

export async function cacheSource(queries: QueryClient, source: Source): Promise<void> {
  await queries.cancelQueries({ queryKey: ["sources"] });
  queries.setQueryData<Source[]>(["sources"], (current = []) => current.some((value) => value.id === source.id)
    ? current.map((value) => value.id === source.id ? source : value) : [...current, source]);
  void queries.invalidateQueries({ queryKey: ["sources"] });
}
