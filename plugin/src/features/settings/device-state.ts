export interface WorkspaceStatePort {
  read: () => Promise<ConnectionLocalState>;
  update: (change: (value: ConnectionLocalState) => void) => Promise<void>;
  subscribe: (listener: () => void) => () => void;
}

export interface TransitionWatch {
  modelId: string;
  selectionGeneration: number;
  jobId: string | null;
  notified: boolean;
}

export interface ConnectionLocalState {
  pendingTransition?: { modelId: string; afterGeneration: number };
  navigation?: ReturnType<ShellNavigation["exportState"]>;
  digest: {
    baseline: { generatedAt: string; issueId: string } | null;
    seenIssueIds: string[];
    notifiedIssueIds: string[];
  };
  unresolvedBuilds: Record<string, string>;
  watchedTransitions: Record<string, TransitionWatch>;
}

export interface DeviceState {
  version: 1;
  serverUrl: string;
  connections: Record<string, ConnectionLocalState>;
}

export const DEFAULT_SERVER_URL = "http://127.0.0.1:7331";

export function normalizeDeviceState(input: unknown, legacy?: unknown): DeviceState {
  const value = record(input);
  if (value.version !== 1) {
    return { version: 1, serverUrl: safeServerUrl(input == null ? record(legacy).serverUrl : undefined), connections: {} };
  }
  const connections = Object.entries(record(value.connections)).flatMap(([key, state]) => {
    const normalizedKey = normalizeScopeKey(key);
    return normalizedKey === null ? [] : [[normalizedKey, normalizeConnectionState(state)] as const];
  });
  return { version: 1, serverUrl: safeServerUrl(value.serverUrl), connections: Object.fromEntries(connections) };
}

export function normalizeConnectionState(input: unknown): ConnectionLocalState {
  const value = record(input);
  const digest = record(value.digest);
  const baseline = record(digest.baseline);
  const pendingTransition = normalizePendingTransition(value.pendingTransition);
  return {
    ...(pendingTransition ? { pendingTransition } : {}),
    ...(value.navigation == null ? {} : { navigation: new ShellNavigation(value.navigation).exportState() }),
    digest: {
      baseline: typeof baseline.generatedAt === "string" && typeof baseline.issueId === "string"
        ? { generatedAt: baseline.generatedAt, issueId: baseline.issueId } : null,
      seenIssueIds: stringIds(digest.seenIssueIds),
      notifiedIssueIds: stringIds(digest.notifiedIssueIds)
    },
    unresolvedBuilds: Object.fromEntries(Object.entries(record(value.unresolvedBuilds)).filter((entry): entry is [string, string] => typeof entry[1] === "string")),
    watchedTransitions: Object.fromEntries(Object.entries(record(value.watchedTransitions)).flatMap(([key, entry]) => {
      const watch = normalizeWatch(entry);
      return watch === null ? [] : [[key, watch] as const];
    }))
  };
}

export function connectionScope(serverUrl: string, workspaceId: string | null): string {
  return JSON.stringify([normalizeServerUrl(serverUrl), workspaceId]);
}

function safeServerUrl(value: unknown): string {
  try {
    return typeof value === "string" ? normalizeServerUrl(value) : DEFAULT_SERVER_URL;
  } catch {
    return DEFAULT_SERVER_URL;
  }
}

function normalizeScopeKey(key: string): string | null {
  try {
    const value: unknown = JSON.parse(key);
    if (!Array.isArray(value) || value.length !== 2 || typeof value[0] !== "string") return null;
    if (value[1] !== null && typeof value[1] !== "string") return null;
    return connectionScope(value[0], value[1] as string | null);
  } catch {
    return null;
  }
}

function stringIds(value: unknown): string[] {
  return Array.isArray(value) ? [...new Set(value.filter((id): id is string => typeof id === "string" && id !== ""))] : [];
}

function normalizeWatch(input: unknown): TransitionWatch | null {
  const value = record(input);
  if (typeof value.modelId !== "string" || typeof value.selectionGeneration !== "number") return null;
  if (!Number.isSafeInteger(value.selectionGeneration) || value.selectionGeneration < 0) return null;
  if (value.jobId !== null && typeof value.jobId !== "string") return null;
  return { modelId: value.modelId, selectionGeneration: value.selectionGeneration, jobId: value.jobId, notified: value.notified === true };
}
import { normalizeServerUrl } from "../../api/url";
import { ShellNavigation } from "../shell/navigation";
import { record } from "./preferences";


function normalizePendingTransition(input: unknown): ConnectionLocalState["pendingTransition"] {
  const value = record(input);
  if (typeof value.modelId !== "string" || typeof value.afterGeneration !== "number") return undefined;
  if (!Number.isSafeInteger(value.afterGeneration) || value.afterGeneration < 0) return undefined;
  return { modelId: value.modelId, afterGeneration: value.afterGeneration };
}
