import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { api, ApiError } from "../api/client";
import type { UserDetail } from "../api/types";

const TIG_DURATIONS = [
  { label: "2h", seconds: 7200 },
  { label: "4h", seconds: 14400 },
  { label: "8h", seconds: 28800 },
];

export function UserDetailPage() {
  const { pk } = useParams<{ pk: string }>();
  const navigate = useNavigate();
  const [detail, setDetail] = useState<UserDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const [pointsValue, setPointsValue] = useState(1);
  const [pointsReason, setPointsReason] = useState("");
  const [tigDuration, setTigDuration] = useState(TIG_DURATIONS[0].seconds);
  const [tigReason, setTigReason] = useState("");
  const [busy, setBusy] = useState(false);

  function reload() {
    if (!pk) return;
    setLoading(true);
    api
      .getUser(Number(pk))
      .then(setDetail)
      .catch((err) => setError(err instanceof ApiError ? err.message : "Failed to load user"))
      .finally(() => setLoading(false));
  }

  useEffect(reload, [pk]);

  async function givePoints() {
    if (!detail) return;
    setBusy(true);
    setError(null);
    setMessage(null);
    try {
      const res = await api.giveCoalitionPoints(detail.entry.pk, pointsValue, pointsReason);
      setMessage(res.message);
      setPointsReason("");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Failed to send points");
    } finally {
      setBusy(false);
    }
  }

  async function giveTig() {
    if (!detail) return;
    setBusy(true);
    setError(null);
    setMessage(null);
    try {
      const res = await api.giveTig(detail.entry.pk, tigDuration, tigReason);
      setMessage(res.message);
      setTigReason("");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Failed to send TIG");
    } finally {
      setBusy(false);
    }
  }

  async function refreshProfile() {
    if (!detail) return;
    setBusy(true);
    try {
      setDetail(await api.refreshUserProfile(detail.entry.pk));
    } finally {
      setBusy(false);
    }
  }

  async function refreshCoalition() {
    if (!detail) return;
    setBusy(true);
    try {
      setDetail(await api.refreshUserCoalition(detail.entry.pk));
    } finally {
      setBusy(false);
    }
  }

  async function manualBlame() {
    if (!detail) return;
    await api.addManualBlame(detail.entry.pk);
    reload();
  }

  async function deleteUser() {
    if (!detail) return;
    if (!confirm(`Remove ${detail.login || detail.entry.fullName} from the local cache?`)) return;
    await api.deleteUser(detail.entry.pk);
    navigate("/users");
  }

  if (loading) return <p className="muted">Loading…</p>;
  if (!detail) return <p className="error-box">User not found</p>;

  return (
    <div>
      <button className="btn secondary" onClick={() => navigate("/users")} style={{ marginBottom: 12 }}>
        ← Back to users
      </button>

      <div className="card row">
        {detail.photoUrl && <img className="avatar" style={{ width: 72, height: 72 }} src={detail.photoUrl} alt="" />}
        <div>
          <h2 style={{ margin: 0 }}>{detail.login || detail.entry.fullName}</h2>
          <div className="muted">
            {detail.userType} {detail.coalitionName && `· ${detail.coalitionName}`}
            {detail.location && ` · Online at ${detail.location}`}
            {detail.level != null && ` · Level ${detail.level.toFixed(2)}`}
          </div>
          {(detail.currentProjects ?? []).length > 0 && (
            <div className="muted">Working on: {(detail.currentProjects ?? []).join(", ")}</div>
          )}
        </div>
        <div style={{ flex: 1 }} />
        <button className="btn secondary" onClick={refreshProfile} disabled={busy}>
          Refresh profile
        </button>
        <button className="btn secondary" onClick={refreshCoalition} disabled={busy}>
          Refresh coalition
        </button>
      </div>

      {error && <div className="error-box">{error}</div>}
      {message && <div className="success-box">{message}</div>}

      <div className="card">
        <h3>Coalition points</h3>
        <div className="row">
          <input
            type="number"
            style={{ width: 80 }}
            value={pointsValue}
            onChange={(e) => setPointsValue(Number(e.target.value))}
          />
          <input
            placeholder="Reason"
            style={{ flex: 1 }}
            value={pointsReason}
            onChange={(e) => setPointsReason(e.target.value)}
          />
          <button className="btn" onClick={givePoints} disabled={busy}>
            Send
          </button>
        </div>
      </div>

      <div className="card">
        <h3>TIG</h3>
        <div className="row">
          <select value={tigDuration} onChange={(e) => setTigDuration(Number(e.target.value))}>
            {TIG_DURATIONS.map((d) => (
              <option key={d.seconds} value={d.seconds}>
                {d.label}
              </option>
            ))}
          </select>
          <input placeholder="Reason" style={{ flex: 1 }} value={tigReason} onChange={(e) => setTigReason(e.target.value)} />
          <button className="btn" onClick={giveTig} disabled={busy}>
            Send TIG
          </button>
        </div>
      </div>

      <div className="card row">
        <button className="btn secondary" onClick={manualBlame}>
          Record a blame without a badge
        </button>
        <button className="btn danger" onClick={deleteUser}>
          Remove from cache
        </button>
      </div>

      <div className="card">
        <h3>Scans</h3>
        {(detail.scans ?? []).length === 0 ? (
          <p className="muted">No blames recorded for this user.</p>
        ) : (
          <table>
            <thead>
              <tr>
                <th>When</th>
                <th>Reason</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              {(detail.scans ?? []).map((s) => (
                <tr key={s.id}>
                  <td className="muted">{new Date(s.timestamp).toLocaleString()}</td>
                  <td>{s.reason ?? "—"}</td>
                  <td>{s.blameStatus}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
