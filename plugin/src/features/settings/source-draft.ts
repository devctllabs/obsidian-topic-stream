import { z } from "zod";
import type { Schema, Source, SourceInput } from "../../api/contracts";

const feedUrl = z.url({ protocol: /^https?$/, error: "Enter an HTTP or HTTPS feed URL." });
export const sourceSchema = z.object({
  name: z.string().trim().min(1, "Enter a name.").max(120),
  enabled: z.boolean(),
  pollIntervalMinutes: z.number().int().min(15).max(10080),
  config: z.discriminatedUnion("kind", [
    z.object({ kind: z.literal("rss"), url: feedUrl, allowPrivateNetwork: z.boolean() }),
    z.object({ kind: z.literal("atom"), url: feedUrl, allowPrivateNetwork: z.boolean() }),
    z.object({ kind: z.literal("arxiv"), query: z.string().trim().min(1, "Enter an arXiv query.").max(500) }),
    z.object({ kind: z.literal("youtube"), channelId: z.string().trim().regex(/^UC[A-Za-z0-9_-]{20,}$/, "Enter a YouTube Channel ID beginning with UC.") }),
    z.object({ kind: z.literal("github"), period: z.enum(["daily", "weekly", "monthly"]), language: z.enum(["all", "python", "rust", "go", "javascript", "typescript"]) })
  ])
}) satisfies z.ZodType<SourceInput>;

export function defaultSourceConfig(kind: Schema<"SourceConfig">["kind"]): Schema<"SourceConfig"> {
  switch (kind) {
    case "rss": case "atom": return { kind, url: "", allowPrivateNetwork: false };
    case "arxiv": return { kind, query: "" };
    case "youtube": return { kind, channelId: "" };
    case "github": return { kind, period: "daily", language: "all" };
  }
}
export function sourceDraft(source: Source | null): SourceInput {
  if (!source) return { name: "", config: defaultSourceConfig("rss"), enabled: true, pollIntervalMinutes: 60 };
  return { name: source.name, config: source.config, enabled: source.enabled, pollIntervalMinutes: source.pollIntervalMinutes };
}
