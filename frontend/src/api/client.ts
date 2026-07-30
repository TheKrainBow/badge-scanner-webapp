// Typed fetch client: sends the session cookie via credentials: "include"
// on every request — the dashboard's only auth layer (see
// backend/internal/auth/auth.go's package doc comment for why it doesn't
// also need an API-key header: that used to be required here too, but a
// key deletable from this same dashboard's own Admin page was a pure
// self-lockout risk with no compensating security benefit over the session
// cookie already in place).
import type {
  Account,
  ApiKey,
  ApiKeyCreated,
  ApiKeyScope,
  ApiKeyUpdate,
  ApiKeyUsage,
  ApiKeyUsageEntry,
  AppSettings,
  CADirectoryInfo,
  ClusterData,
  ClusterOccupant,
  IntraBulkInfo,
  ScanRecord,
  UserDetail,
  UserRow,
} from "./types";

const API_BASE = import.meta.env.VITE_API_BASE ?? "http://localhost:8080";

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    ...init,
    credentials: "include",
    headers: {
      ...(init?.body ? { "Content-Type": "application/json" } : {}),
      ...init?.headers,
    },
  });
  if (!res.ok) {
    let message = `HTTP ${res.status}`;
    try {
      const body = await res.json();
      if (body?.error) message = body.error;
    } catch {
      // ignore non-JSON error bodies
    }
    throw new ApiError(res.status, message);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

function get<T>(path: string): Promise<T> {
  return request<T>(path);
}
function post<T>(path: string, body?: unknown): Promise<T> {
  return request<T>(path, { method: "POST", body: body !== undefined ? JSON.stringify(body) : undefined });
}
function patch<T>(path: string, body?: unknown): Promise<T> {
  return request<T>(path, { method: "PATCH", body: body !== undefined ? JSON.stringify(body) : undefined });
}
function put<T>(path: string, body?: unknown): Promise<T> {
  return request<T>(path, { method: "PUT", body: JSON.stringify(body) });
}
function del<T>(path: string): Promise<T> {
  return request<T>(path, { method: "DELETE" });
}

export const api = {
  login: (username: string, password: string) => post<Account>("/api/auth/login", { username, password }),
  logout: () => post<{ ok: boolean }>("/api/auth/logout"),
  me: () => get<Account>("/api/auth/me"),

  associateBadge: (uidHex: string, login: string) => post<ScanRecord>("/api/badges/associate", { uidHex, login }),

  listUsers: (params: {
    query?: string;
    type?: string;
    scannedOnly?: boolean;
    errorOnly?: boolean;
    coalition?: string;
    order?: string;
  }) => {
    const qs = new URLSearchParams();
    if (params.query) qs.set("query", params.query);
    if (params.type) qs.set("type", params.type);
    if (params.scannedOnly) qs.set("scannedOnly", "true");
    if (params.errorOnly) qs.set("errorOnly", "true");
    if (params.coalition) qs.set("coalition", params.coalition);
    if (params.order) qs.set("order", params.order);
    return get<{ rows: UserRow[] | null; coalitions: string[] | null }>(`/api/users?${qs.toString()}`);
  },
  getUser: (pk: number) => get<UserDetail>(`/api/users/${pk}`),
  deleteUser: (pk: number) => del<{ ok: boolean }>(`/api/users/${pk}`),
  refreshUserProfile: (pk: number) => post<UserDetail>(`/api/users/${pk}/refresh-profile`),
  refreshUserCoalition: (pk: number) => post<UserDetail>(`/api/users/${pk}/refresh-coalition`),
  addManualBlame: (pk: number) => post<ScanRecord>(`/api/users/${pk}/manual-blame`),

  giveCoalitionPoints: (pk: number, value: number, reason: string) =>
    post<{ message: string }>("/api/coalitions/score", { pk, value, reason }),
  giveTig: (pk: number, durationSeconds: number, reason: string) =>
    post<{ message: string }>("/api/tig", { pk, durationSeconds, reason }),

  getClusters: (force = false) =>
    get<{
      clusters: ClusterData["clusters"] | null;
      layouts: ClusterData["layouts"] | null;
      occupants: ClusterData["occupants"] | null;
    }>(`/api/clusters?force=${force}`),
  refreshOccupants: () => post<Record<string, ClusterOccupant>>("/api/clusters/refresh-occupants"),

  caInfo: () => get<CADirectoryInfo>("/api/ca/info"),
  refreshCADirectory: () => post<{ count: number }>("/api/ca/refresh"),

  intraInfo: () => get<IntraBulkInfo>("/api/intra/info"),
  refreshIntraUsers: () => post<{ count: number }>("/api/intra/refresh"),
  refreshCoalitions: () => post<{ count: number }>("/api/coalitions/refresh"),

  getSettings: () => get<AppSettings>("/api/admin/settings"),
  putSettings: (settings: AppSettings) => put<AppSettings>("/api/admin/settings", settings),

  listAccounts: () => get<Account[]>("/api/admin/users"),
  createAccount: (username: string, password: string, isAdmin: boolean) =>
    post<Account>("/api/admin/users", { username, password, isAdmin }),
  deleteAccount: (id: number) => del<{ ok: boolean }>(`/api/admin/users/${id}`),
  patchAccount: (id: number, patchBody: { password?: string; isAdmin?: boolean }) =>
    patch<Account>(`/api/admin/users/${id}`, patchBody),

  listApiKeys: () => get<ApiKey[]>("/api/admin/api-keys"),
  getApiKey: (id: number) => get<ApiKey>(`/api/admin/api-keys/${id}`),
  createApiKey: (name: string, permissions: ApiKeyScope[], rateLimitPerMinute = 0, rateLimitPerHour = 0) =>
    post<ApiKeyCreated>("/api/admin/api-keys", { name, permissions, rateLimitPerMinute, rateLimitPerHour }),
  updateApiKey: (id: number, update: ApiKeyUpdate) => patch<ApiKey>(`/api/admin/api-keys/${id}`, update),
  deleteApiKey: (id: number) => del<{ ok: boolean }>(`/api/admin/api-keys/${id}`),
  apiKeyUsage: (id: number) => get<ApiKeyUsage[]>(`/api/admin/api-keys/${id}/usage`),
  listAllApiKeyUsage: () => get<ApiKeyUsageEntry[]>("/api/admin/api-keys/usage"),
};
