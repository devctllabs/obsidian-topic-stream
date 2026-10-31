import { z } from "zod";
import type { Schema } from "../../api/contracts";
import { normalizeServerUrl } from "../../api/url";

export type WorkspaceDraft = Required<Schema<"WorkspaceSettingsPatch">>;
const instruction = z.string().trim().min(1).max(12000);
export const workspaceSchema = z.object({
  embeddingModelId: z.uuid(),
  generation: z.object({ enabled: z.boolean(), endpoint: z.string().nullable(), summaryModel: z.string().nullable(), rerankModel: z.string().nullable() }).superRefine((value, ctx) => {
    if (!value.enabled) return;
    try { normalizeServerUrl(value.endpoint ?? ""); } catch { ctx.addIssue({ code: "custom", path: ["endpoint"], message: "Enter an HTTP(S) endpoint without credentials, query or fragment." }); }
    for (const field of ["summaryModel", "rerankModel"] as const) if (!value[field]?.trim()) ctx.addIssue({ code: "custom", path: [field], message: "Enter a configured model name." });
  }),
  summary: z.object({ language: z.string().trim().min(1), maxInputChars: z.number().int().min(1000).max(1000000), maxSummaryCharacters: z.number().int().min(200).max(4000) }),
  instructions: z.object({ summaryChunk: instruction, summaryReduce: instruction, summaryFinal: instruction, digestRerank: instruction }),
  retention: z.object({ fullContentDays: z.number().int().min(1).nullable() }),
  retrieval: z.object({ candidateLimit: z.number().int().min(1).max(100), rerankLimit: z.number().int().min(1).max(2000), rrfK: z.number().int().min(1), recencyHalfLifeDays: z.number().positive(), recencyMaxBoost: z.number().min(0).max(1), feedSourceCap: z.number().int().min(1), digestSourceCap: z.number().int().min(1), digestMmrLambda: z.number().min(0).max(1), llmRerankEnabled: z.boolean(), digestExplorationEnabled: z.boolean() })
});
export function workspaceDraft(value: Schema<"WorkspaceSettings">): WorkspaceDraft {
  return structuredClone({ embeddingModelId: value.embeddingModelId, generation: value.generation, summary: value.summary, instructions: value.instructions, retention: value.retention, retrieval: value.retrieval });
}
