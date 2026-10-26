export interface FieldIssue {
  field: string;
  message: string;
}

interface PublicProblem {
  type: string;
  title: string;
  detail: string | null;
  code: string | null;
  issues: FieldIssue[];
  canonicalCommitted: boolean;
  projectionRevision: string | null;
}

/** ApiError keeps machine-readable facts separate from the server's approved display text. */
export class ApiError extends Error {
  public readonly type: string | null;
  public readonly code: string | null;
  public readonly issues: readonly FieldIssue[];
  public readonly canonicalCommitted: boolean;
  public readonly projectionRevision: string | null;

  public constructor(public readonly status: number, problem: PublicProblem | null) {
    super(problem?.detail ?? problem?.title ?? `Topic Stream could not complete the request (HTTP ${status}).`);
    this.name = "ApiError";
    this.type = problem?.type ?? null;
    this.code = problem?.code ?? null;
    this.issues = problem?.issues ?? [];
    this.canonicalCommitted = problem?.canonicalCommitted ?? false;
    this.projectionRevision = problem?.projectionRevision ?? null;
  }
}

/** httpError validates Problem Details without exposing unstructured response bodies. */
export function httpError(status: number, text: string): ApiError {
  try {
    return new ApiError(status, parseProblem(JSON.parse(text) as unknown, status));
  } catch {
    return new ApiError(status, null);
  }
}

function parseProblem(value: unknown, status: number): PublicProblem | null {
  if (!isRecord(value) || value.status !== status || typeof value.type !== "string" || value.type.trim() === "" ||
      typeof value.title !== "string" || value.title.trim() === "") {
    return null;
  }
  if ((value.detail !== undefined && typeof value.detail !== "string") ||
      (value.code !== undefined && typeof value.code !== "string")) {
    return null;
  }
  const issues = parseIssues(value.errors);
  if (issues === null) return null;
  return {
    type: value.type,
    title: value.title,
    detail: typeof value.detail === "string" && value.detail.trim() !== "" ? value.detail : null,
    code: typeof value.code === "string" ? value.code : null,
    issues,
    canonicalCommitted: value.code === "projection_pending" && value.canonicalCommitted === true,
    projectionRevision: typeof value.projectionRevision === "string" ? value.projectionRevision : null
  };
}

function parseIssues(value: unknown): FieldIssue[] | null {
  if (value === undefined) return [];
  if (!Array.isArray(value)) return null;
  const issues: FieldIssue[] = [];
  for (const issue of value) {
    if (!isRecord(issue) || typeof issue.field !== "string" || typeof issue.message !== "string") return null;
    issues.push({ field: issue.field, message: issue.message });
  }
  return issues;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
