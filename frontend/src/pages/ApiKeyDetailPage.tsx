import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { api, ApiError } from "../api/client";
import type { ApiKey, ApiKeyScope, ApiKeyUsage } from "../api/types";

const API_KEY_SCOPES: { value: ApiKeyScope; label: string }[] = [
  { value: "full", label: "Full access" },
  { value: "lookup", label: "Lookup only (badge → login/coalition/photo, nothing else)" },
];

export function ApiKeyDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [key, setKey] = useState<ApiKey | null>(null);
  const [name, setName] = useState("");
  const [permissions, setPermissions] = useState<ApiKeyScope[]>([]);
  const [rateLimitPerMinute, setRateLimitPerMinute] = useState(0);
  const [rateLimitPerHour, setRateLimitPerHour] = useState(0);
  const [usage, setUsage] = useState<ApiKeyUsage[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  function reload() {
    if (!id) return;
    setLoading(true);
    Promise.all([api.getApiKey(Number(id)), api.apiKeyUsage(Number(id))])
      .then(([k, u]) => {
        setKey(k);
        setName(k.name);
        setPermissions(k.permissions);
        setRateLimitPerMinute(k.rateLimitPerMinute);
        setRateLimitPerHour(k.rateLimitPerHour);
        setUsage(u ?? []);
      })
      .catch((err) => setError(err instanceof ApiError ? err.message : "Failed to load API key"))
      .finally(() => setLoading(false));
  }

  useEffect(reload, [id]);

  function togglePermission(scope: ApiKeyScope) {
    setPermissions((prev) => (prev.includes(scope) ? prev.filter((p) => p !== scope) : [...prev, scope]));
  }

  async function save() {
    if (!id) return;
    setBusy(true);
    setError(null);
    setMessage(null);
    try {
      const updated = await api.updateApiKey(Number(id), { name, permissions, rateLimitPerMinute, rateLimitPerHour });
      setKey(updated);
      setMessage("Saved");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Failed to save API key");
    } finally {
      setBusy(false);
    }
  }

  async function revoke() {
    if (!id || !confirm("Revoke this API key? Anything using it will stop working immediately.")) return;
    await api.deleteApiKey(Number(id));
    navigate("/admin");
  }

  if (loading) return <p className="muted">Loading…</p>;
  if (!key) return <p className="error-box">{error ?? "API key not found"}</p>;

  return (
    <div>
      <button className="btn secondary" onClick={() => navigate("/admin")} style={{ marginBottom: 12 }}>
        ← Back to Admin
      </button>

      <div className="card">
        <h2 style={{ marginTop: 0 }}>{key.name}</h2>
        <p className="muted">
          Client ID <code>{key.clientId}</code> — the secret was shown once at creation and can't be retrieved again.
        </p>

        {error && <div className="error-box">{error}</div>}
        {message && <div className="success-box">{message}</div>}

        <div style={{ marginBottom: 10 }}>
          <label style={{ display: "block", marginBottom: 4 }}>Name</label>
          <input style={{ width: "100%" }} value={name} onChange={(e) => setName(e.target.value)} />
        </div>

        <div style={{ marginBottom: 10 }}>
          <label style={{ display: "block", marginBottom: 4 }}>Permissions</label>
          {API_KEY_SCOPES.map((scope) => (
            <label key={scope.value} style={{ display: "block" }}>
              <input
                type="checkbox"
                checked={permissions.includes(scope.value)}
                onChange={() => togglePermission(scope.value)}
              />{" "}
              {scope.label}
            </label>
          ))}
        </div>

        <div className="row" style={{ marginBottom: 10 }}>
          <div>
            <label style={{ display: "block", marginBottom: 4 }}>Rate limit — per minute</label>
            <input
              type="number"
              min={0}
              style={{ width: 100 }}
              value={rateLimitPerMinute}
              onChange={(e) => setRateLimitPerMinute(Math.max(0, Number(e.target.value)))}
            />
          </div>
          <div>
            <label style={{ display: "block", marginBottom: 4 }}>Rate limit — per hour</label>
            <input
              type="number"
              min={0}
              style={{ width: 100 }}
              value={rateLimitPerHour}
              onChange={(e) => setRateLimitPerHour(Math.max(0, Number(e.target.value)))}
            />
          </div>
        </div>
        <p className="muted" style={{ marginTop: -4 }}>0 = unlimited. Enforced on badge lookups only.</p>

        <button className="btn" onClick={save} disabled={busy}>
          Save
        </button>{" "}
        <button className="btn danger" onClick={revoke}>
          Revoke key
        </button>
      </div>

      <div className="card">
        <h3>Usage history</h3>
        {usage.length === 0 ? (
          <p className="muted">No lookups recorded for this key yet.</p>
        ) : (
          <table>
            <thead>
              <tr>
                <th>When</th>
                <th>Badge UID</th>
                <th>Result</th>
                <th>Coalition</th>
              </tr>
            </thead>
            <tbody>
              {usage.map((u, i) => (
                <tr key={i}>
                  <td className="muted">{new Date(u.timestamp).toLocaleString()}</td>
                  <td>{u.uidHex}</td>
                  <td>{u.found ? u.login : "not found"}</td>
                  <td style={{ color: u.coalitionColor || undefined }}>{u.coalitionName}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
