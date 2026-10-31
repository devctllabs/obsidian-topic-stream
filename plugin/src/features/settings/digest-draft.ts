import { z } from "zod";
import type { Schema } from "../../api/contracts";

const time = { hour: z.number().int().min(0).max(23), minute: z.number().int().min(0).max(59) };
const monthlyRule = z.discriminatedUnion("type", [
  z.object({ type: z.literal("dayOfMonth"), day: z.number().int().min(1).max(31) }),
  z.object({ type: z.literal("nthWeekday"), weekday: z.number().int().min(1).max(7), occurrence: z.number().int().min(1).max(5) }),
  z.object({ type: z.literal("lastDay") })
]);
export const digestSchema = z.object({
  name: z.string().trim().min(1, "Enter a name.").max(120),
  topics: z.array(z.object({ topicId: z.uuid(), itemLimit: z.number().int().min(1).max(15) })).min(1, "Choose at least one Topic."),
  enabled: z.boolean(),
  timezone: z.string().refine((value) => { try { new Intl.DateTimeFormat("en", { timeZone: value }); return true; } catch { return false; } }, "Enter a valid IANA timezone."),
  recurrence: z.discriminatedUnion("frequency", [
    z.object({ frequency: z.literal("daily"), ...time }),
    z.object({ frequency: z.literal("weekly"), weekdays: z.array(z.number().int().min(1).max(7)).min(1), ...time }),
    z.object({ frequency: z.literal("monthly"), rule: monthlyRule, ...time })
  ])
});
export function digestDraft(value: Schema<"DigestDefinition"> | null): Schema<"DigestInput"> {
  if (value) return { name: value.name, topics: structuredClone(value.topics), enabled: value.enabled, timezone: value.timezone, recurrence: structuredClone(value.recurrence) };
  return { name: "", topics: [], enabled: false, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone, recurrence: { frequency: "weekly", weekdays: [1], hour: 12, minute: 0 } };
}
