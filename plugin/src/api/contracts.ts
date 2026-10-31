import type { components, operations } from "./generated/openapi";

export type Schema<Name extends keyof components["schemas"]> = components["schemas"][Name];
export type ResponseBody<Operation extends keyof operations, Status extends keyof operations[Operation]["responses"]> =
  operations[Operation]["responses"][Status] extends { content: { "application/json": infer Body } } ? Body : never;
export type RequestBody<Operation extends keyof operations> =
  operations[Operation] extends { requestBody: { content: { "application/json": infer Body } } } ? Body : never;
export type PathParameters<Operation extends keyof operations> = NonNullable<operations[Operation]["parameters"]["path"]>;
export type QueryParameters<Operation extends keyof operations> = NonNullable<operations[Operation]["parameters"]["query"]>;

export type Feedback = ResponseBody<"setItemFeedback", 200>;
export type FeedPage = ResponseBody<"getFeed", 200>;
export type FeedEntry = FeedPage["items"][number];
export type Item = Schema<"Item">;
export type ItemKind = Schema<"ItemKind">;
export type TopicMode = Schema<"TopicMode">;
export type Source = ResponseBody<"createSource", 201>;
export type SourceInput = RequestBody<"createSource">;
export type Topic = ResponseBody<"createTopic", 201>;
export type TopicInput = RequestBody<"createTopic">;
export type JobAccepted = ResponseBody<"syncAllSources", 202>;
export type PublicProblem = Schema<"Problem"> | Schema<"ProjectionPendingProblem">;

/** Binary image response plus its validated media type, outside the API's JSON schemas. */
export interface ItemImageDownload { bytes: ArrayBuffer; contentType: string; }
