import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, ApiError } from "../api/client";
import { useEvents } from "../events/EventsContext";
import type {
  Account,
  ApiKey,
  ApiKeyCreated,
  ApiKeyScope,
  AppSettings,
  CADirectoryInfo,
  IntraBulkInfo,
} from "../api/types";

interface JobProgress {
  current: number;
  total: number;
  currentItem: string;
}

// Shared by every refresh section: subscribes to progress/complete/error
// events for one job name, returns live progress plus a message setter so
// each section can still show its own success/error text on completion.
function useJobProgress(job: string, onMessage: (message: string | null) => void, onComplete?: () => void) {
  const [progress, setProgress] = useState<JobProgress | null>(null);
  const { subscribe } = useEvents();

  useEffect(() => {
    const unsubs = [
      subscribe("progress", (e) => {
        if (e.job === job) setProgress({ current: e.current, total: e.total, currentItem: e.currentItem });
      }),
      subscribe("refreshComplete", (e) => {
        if (e.job !== job) return;
        setProgress(null);
        onMessage(`Done — ${e.count} processed`);
        onComplete?.();
      }),
      subscribe("refreshError", (e) => {
        if (e.job !== job) return;
        setProgress(null);
        onMessage(e.message);
      }),
    ];
    return () => unsubs.forEach((u) => u());
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [subscribe, job]);

  return progress;
}

function ProgressBar({ progress }: { progress: JobProgress }) {
  return (
    <div style={{ margin: "8px 0" }}>
      <progress value={progress.total > 0 ? progress.current : undefined} max={progress.total > 0 ? progress.total : undefined} style={{ width: "100%" }} />
      <p className="muted" style={{ margin: "4px 0 0" }}>
        {progress.total > 0 ? `${progress.current} of ${progress.total}` : `${progress.current} fetched`}
        {progress.currentItem ? ` — fetching: ${progress.currentItem}` : ""}
      </p>
    </div>
  );
}

export function AdminPage() {
  return (
    <div>
      <h1>Admin</h1>
      <SettingsSection />
      <CADirectorySection />
      <IntraSection />
      <UsersSection />
      <ApiKeysSection />
    </div>
  );
}

function SettingsSection() {
  const [settings, setSettings] = useState<AppSettings | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api.getSettings().then(setSettings);
  }, []);

  async function save() {
    if (!settings) return;
    setBusy(true);
    setError(null);
    setMessage(null);
    try {
      setSettings(await api.putSettings(settings));
      setMessage("Settings saved");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Failed to save settings");
    } finally {
      setBusy(false);
    }
  }

  if (!settings) return <p className="muted">Loading settings…</p>;

  const currentSettings = settings;
  type StringField = Exclude<keyof AppSettings, "displayDetailedScans">;

  function field(key: StringField, label: string, type = "text") {
    return (
      <div style={{ marginBottom: 10 }}>
        <label style={{ display: "block", marginBottom: 4 }}>{label}</label>
        <input
          type={type}
          style={{ width: "100%" }}
          value={currentSettings[key]}
          onChange={(e) => {
            const value = e.target.value;
            setSettings((prev) => (prev ? { ...prev, [key]: value } : prev));
          }}
        />
      </div>
    );
  }

  return (
    <div className="card">
      <h3>CA / 42 settings</h3>
      {error && <div className="error-box">{error}</div>}
      {message && <div className="success-box">{message}</div>}
      {field("caEndpoint", "CA endpoint")}
      {field("caUsername", "CA username")}
      {field("caPassword", "CA password", "password")}
      {field("ftTokenUrl", "42 OAuth token URL")}
      {field("ftEndpoint", "42 API endpoint")}
      {field("ftUid", "42 API client id")}
      {field("ftSecret", "42 API client secret", "password")}
      {field("closerId", "Closer ID (for TIGs)")}
      {field("campusId", "Campus ID")}
      <button className="btn" onClick={save} disabled={busy}>
        Save
      </button>
    </div>
  );
}

function CADirectorySection() {
  const [info, setInfo] = useState<CADirectoryInfo | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<string | null>(null);

  function reload() {
    api.caInfo().then(setInfo);
  }

  useEffect(reload, []);

  const progress = useJobProgress("ca", setMessage, reload);

  async function refresh() {
    setBusy(true);
    setMessage(null);
    try {
      await api.refreshCADirectory();
      // Completion message/reload comes from the "refreshComplete" event
      // (useJobProgress above) — this request itself may return after
      // nginx's own timeout for very large directories, well before the
      // backend actually finishes, so don't treat its response as done.
    } catch (err) {
      setMessage(err instanceof ApiError ? err.message : "CA refresh failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="card">
      <h3>CA directory</h3>
      <p className="muted">
        {info ? (
          <>
            {info.userCount} users cached
            {info.fetchedAt ? `, last fetched ${new Date(info.fetchedAt).toLocaleString()}` : " (never fetched)"}
          </>
        ) : (
          "Loading…"
        )}
      </p>
      {message && <div className="success-box">{message}</div>}
      {progress && <ProgressBar progress={progress} />}
      <button className="btn" onClick={refresh} disabled={busy || !!progress}>
        {busy || progress ? "Refetching…" : "Refetch CA users"}
      </button>
      <p className="muted" style={{ marginTop: 8 }}>
        Slow — only run when badges are missing or changed. This also clears any manual badge-to-student links.
      </p>
    </div>
  );
}

function IntraSection() {
  const [info, setInfo] = useState<IntraBulkInfo | null>(null);
  const [busyUsers, setBusyUsers] = useState(false);
  const [busyCoalitions, setBusyCoalitions] = useState(false);
  const [usersMessage, setUsersMessage] = useState<string | null>(null);
  const [coalitionsMessage, setCoalitionsMessage] = useState<string | null>(null);

  function reload() {
    api.intraInfo().then(setInfo);
  }

  useEffect(reload, []);

  const usersProgress = useJobProgress("intra", setUsersMessage, reload);
  const coalitionsProgress = useJobProgress("coalitions", setCoalitionsMessage, reload);

  async function refreshUsers() {
    setBusyUsers(true);
    setUsersMessage(null);
    try {
      await api.refreshIntraUsers();
      // Completion comes from the "refreshComplete" event — see the
      // matching comment on CADirectorySection.refresh.
    } catch (err) {
      setUsersMessage(err instanceof ApiError ? err.message : "42 users refresh failed");
    } finally {
      setBusyUsers(false);
    }
  }

  async function refreshCoalitions() {
    setBusyCoalitions(true);
    setCoalitionsMessage(null);
    try {
      await api.refreshCoalitions();
    } catch (err) {
      setCoalitionsMessage(err instanceof ApiError ? err.message : "Coalitions refresh failed");
    } finally {
      setBusyCoalitions(false);
    }
  }

  return (
    <div className="card">
      <h3>42 intra</h3>
      <p className="muted">
        {info ? (
          <>
            {info.userCount} cached profiles
            {info.fetchedAt
              ? `, last bulk-refreshed ${new Date(info.fetchedAt).toLocaleString()}`
              : " (never bulk-refreshed)"}
          </>
        ) : (
          "Loading…"
        )}
      </p>
      {usersMessage && <div className="success-box">{usersMessage}</div>}
      {usersProgress && <ProgressBar progress={usersProgress} />}
      {coalitionsMessage && <div className="success-box">{coalitionsMessage}</div>}
      {coalitionsProgress && <ProgressBar progress={coalitionsProgress} />}
      <button className="btn" onClick={refreshUsers} disabled={busyUsers || !!usersProgress}>
        {busyUsers || usersProgress ? "Refetching…" : "Refetch 42 users"}
      </button>{" "}
      <button className="btn" onClick={refreshCoalitions} disabled={busyCoalitions || !!coalitionsProgress}>
        {busyCoalitions || coalitionsProgress ? "Refetching…" : "Refetch coalitions"}
      </button>
      <p className="muted" style={{ marginTop: 8 }}>
        Slow — loops every cached CA user through the 42 API. Only run when profiles or coalitions are stale.
      </p>
    </div>
  );
}

function UsersSection() {
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [isAdmin, setIsAdmin] = useState(false);
  const [error, setError] = useState<string | null>(null);

  function reload() {
    api.listAccounts().then(setAccounts);
  }

  useEffect(reload, []);

  async function create() {
    setError(null);
    try {
      await api.createAccount(username, password, isAdmin);
      setUsername("");
      setPassword("");
      setIsAdmin(false);
      reload();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Failed to create account");
    }
  }

  async function remove(id: number) {
    if (!confirm("Delete this account?")) return;
    await api.deleteAccount(id);
    reload();
  }

  async function resetPassword(id: number) {
    const newPassword = prompt("New password (min 8 characters):");
    if (!newPassword) return;
    await api.patchAccount(id, { password: newPassword });
    alert("Password updated");
  }

  async function toggleAdmin(account: Account) {
    await api.patchAccount(account.id, { isAdmin: !account.isAdmin });
    reload();
  }

  return (
    <div className="card">
      <h3>Accounts</h3>
      <table>
        <thead>
          <tr>
            <th>Username</th>
            <th>Admin</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {accounts.map((a) => (
            <tr key={a.id}>
              <td>{a.username}</td>
              <td>
                <input type="checkbox" checked={a.isAdmin} onChange={() => toggleAdmin(a)} />
              </td>
              <td>
                <button className="btn secondary" onClick={() => resetPassword(a.id)}>
                  Reset password
                </button>{" "}
                <button className="btn danger" onClick={() => remove(a.id)}>
                  Delete
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      <h4>New account</h4>
      {error && <div className="error-box">{error}</div>}
      <div className="row">
        <input placeholder="Username" value={username} onChange={(e) => setUsername(e.target.value)} />
        <input
          placeholder="Password (min 8 chars)"
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
        <label>
          <input type="checkbox" checked={isAdmin} onChange={(e) => setIsAdmin(e.target.checked)} /> Admin
        </label>
        <button className="btn" onClick={create}>
          Create
        </button>
      </div>
    </div>
  );
}

const API_KEY_SCOPES: { value: ApiKeyScope; label: string }[] = [
  { value: "full", label: "Full access" },
  { value: "lookup", label: "Lookup only (badge → login/coalition/photo, nothing else)" },
];

function ApiKeysSection() {
  const navigate = useNavigate();
  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [name, setName] = useState("");
  const [permissions, setPermissions] = useState<ApiKeyScope[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [createdKey, setCreatedKey] = useState<ApiKeyCreated | null>(null);

  function reload() {
    api.listApiKeys().then(setKeys);
  }

  useEffect(reload, []);

  function togglePermission(scope: ApiKeyScope) {
    setPermissions((prev) => (prev.includes(scope) ? prev.filter((p) => p !== scope) : [...prev, scope]));
  }

  async function create() {
    setError(null);
    if (!name || permissions.length === 0) {
      setError("Name and at least one permission are required");
      return;
    }
    try {
      const created = await api.createApiKey(name, permissions);
      setCreatedKey(created);
      setName("");
      setPermissions([]);
      reload();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Failed to create API key");
    }
  }

  async function revoke(id: number) {
    if (!confirm("Revoke this API key? Anything using it will stop working immediately.")) return;
    await api.deleteApiKey(id);
    reload();
  }

  async function copySecret() {
    if (!createdKey) return;
    try {
      await navigator.clipboard.writeText(createdKey.clientSecret);
    } catch {
      // clipboard access can be denied — the secret is still shown below, so this is non-fatal
    }
  }

  return (
    <div className="card">
      <h3>API keys</h3>
      <p className="muted">
        Credentials for external clients (the webapp itself, the C badge-lookup client, etc). A "lookup"-scope key
        can only reach the restricted badge-lookup endpoint — no TIG, no coalition points, no blame.
      </p>

      {createdKey && (
        <div className="success-box">
          <strong>{createdKey.name}</strong> created. Copy the secret now — it will not be shown again.
          <div className="row" style={{ marginTop: 6 }}>
            <input readOnly value={createdKey.clientId} style={{ width: "auto" }} />
            <input readOnly value={createdKey.clientSecret} style={{ width: "auto" }} />
            <button className="btn secondary" onClick={copySecret}>
              Copy secret
            </button>
            <button className="btn secondary" onClick={() => setCreatedKey(null)}>
              Dismiss
            </button>
          </div>
        </div>
      )}

      <table>
        <thead>
          <tr>
            <th>Name</th>
            <th>Client ID</th>
            <th>Permissions</th>
            <th>Rate limit</th>
            <th>Last used</th>
            <th>Created</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {keys.map((k) => (
            <tr key={k.id}>
              <td>{k.name}</td>
              <td>{k.clientId}</td>
              <td>{k.permissions.join(", ")}</td>
              <td className="muted">
                {k.rateLimitPerMinute > 0 || k.rateLimitPerHour > 0
                  ? [
                      k.rateLimitPerMinute > 0 ? `${k.rateLimitPerMinute}/min` : null,
                      k.rateLimitPerHour > 0 ? `${k.rateLimitPerHour}/hr` : null,
                    ]
                      .filter(Boolean)
                      .join(", ")
                  : "unlimited"}
              </td>
              <td>{k.lastUsedAt ? new Date(k.lastUsedAt).toLocaleString() : "never"}</td>
              <td>{new Date(k.createdAt).toLocaleString()}</td>
              <td>
                <button className="btn secondary" onClick={() => navigate(`/admin/api-keys/${k.id}`)}>
                  Manage
                </button>{" "}
                <button className="btn danger" onClick={() => revoke(k.id)}>
                  Revoke
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      <h4>New API key</h4>
      {error && <div className="error-box">{error}</div>}
      <div className="row">
        <input placeholder="Name (e.g. kiosk-1)" value={name} onChange={(e) => setName(e.target.value)} />
        {API_KEY_SCOPES.map((scope) => (
          <label key={scope.value}>
            <input
              type="checkbox"
              checked={permissions.includes(scope.value)}
              onChange={() => togglePermission(scope.value)}
            />{" "}
            {scope.label}
          </label>
        ))}
        <button className="btn" onClick={create}>
          Create
        </button>
      </div>
    </div>
  );
}
