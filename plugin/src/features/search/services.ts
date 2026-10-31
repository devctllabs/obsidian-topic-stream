import type { QueryParameters, Schema, Source } from "../../api/contracts";
import type { FeedService } from "../feed/services";

export type SearchRequest = QueryParameters<"searchItems">;
export interface SearchService extends FeedService {
  searchItems(request: SearchRequest): Promise<Schema<"SearchPage">>;
  listSources(): Promise<Source[]>;
}
