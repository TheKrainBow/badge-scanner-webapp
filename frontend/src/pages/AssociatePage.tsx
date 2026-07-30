import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { api, ApiError } from "../api/client";
import type { UserRow } from "../api/types";

export function AssociatePage() {
  const { uidHex } = useParams<{ uidHex: string }>();
  const navigate = useNavigate();
  const [query, setQuery] = useState("");
  const [rows, setRows] = useState<UserRow[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    setLoading(true);
    api
      .listUsers({ query, order: "alphabetical" })
      .then((res) => setRows(res.rows ?? []))
      .finally(() => setLoading(false));
  }, [query]);

  async function associate(login: string) {
    if (!uidHex) return;
    setBusy(true);
    setError(null);
    try {
      await api.associateBadge(uidHex, login);
      navigate("/users");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Failed to associate badge");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div>
      <h1>Associate badge</h1>
      <p className="muted">
        Badge <code>{uidHex}</code> isn't in the CA directory. Pick the student it belongs to — this link is remembered
        until the CA directory is next refetched.
      </p>

      {error && <div className="error-box">{error}</div>}

      <div className="card">
        <input
          placeholder="Search a student…"
          style={{ width: "100%" }}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          autoFocus
        />
      </div>

      {loading ? (
        <p className="muted">Loading…</p>
      ) : (
        <table>
          <tbody>
            {rows.map((row) => (
              <tr key={row.entry.pk}>
                <td>
                  <div className="row">
                    {row.photoUrl && <img className="avatar" style={{ width: 28, height: 28 }} src={row.photoUrl} alt="" />}
                    <span>{row.login || row.entry.fullName}</span>
                  </div>
                </td>
                <td>
                  <button
                    className="btn"
                    disabled={busy || !row.login}
                    onClick={() => row.login && associate(row.login)}
                  >
                    Link
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
