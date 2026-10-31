import type { Schema } from "../../api/contracts";
import type { DigestsService } from "./services";

/** Public result identity is generationJobId; private Job arguments never participate. */
export async function findSavedIssue(client: Pick<DigestsService, "listDigestIssues">, jobId: string, signal: AbortSignal, digestId?: string): Promise<Schema<"DigestIssueSummary"> | null> {
  let cursor: string | undefined;
  const seen = new Set<string>();
  do {
    if (signal.aborted) throw new DOMException("View closed", "AbortError");
    const page = await client.listDigestIssues({ limit: 50, ...(digestId ? { digestId } : {}), ...(cursor ? { cursor } : {}) });
    const issue = page.items.find((issue) => issue.generationJobId === jobId);
    if (issue) return issue;
    if (!page.nextCursor) return null;
    if (seen.has(page.nextCursor)) throw new Error("The archive repeated its page cursor. Check the saved result again.");
    seen.add(page.nextCursor); cursor = page.nextCursor;
  } while (!signal.aborted);
  throw new DOMException("View closed", "AbortError");
}
