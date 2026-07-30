import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import type { UserRow } from "../api/types";

export function UsersPage() {
  const [rows, setRows] = useState<UserRow[]>([]);
  const [coalitions, setCoalitions] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [query, setQuery] = useState("");
  const [type, setType] = useState("");
  const [coalition, setCoalition] = useState("");
  const [scannedOnly, setScannedOnly] = useState(false);
  const [errorOnly, setErrorOnly] = useState(false);
  const [order, setOrder] = useState("latest");

  function reload() {
    setLoading(true);
    api
      .listUsers({ query, type, coalition, scannedOnly, errorOnly, order })
      .then((res) => {
        setRows(res.rows ?? []);
        setCoalitions(res.coalitions ?? []);
      })
      .finally(() => setLoading(false));
  }

  useEffect(reload, [query, type, coalition, scannedOnly, errorOnly, order]);

  async function remove(row: UserRow) {
    if (!confirm(`Remove ${row.login || row.entry.fullName} from the local cache?`)) return;
    await api.deleteUser(row.entry.pk);
    reload();
  }

  return (
    <div>
      <h1>Users</h1>

      <div className="card row" style={{ flexWrap: "wrap" }}>
        <input placeholder="Search…" value={query} onChange={(e) => setQuery(e.target.value)} />
        <select value={type} onChange={(e) => setType(e.target.value)}>
          <option value="">All types</option>
          <option value="Student">Student</option>
          <option value="Piscine">Piscine</option>
        </select>
        <select value={coalition} onChange={(e) => setCoalition(e.target.value)}>
          <option value="">All coalitions</option>
          {coalitions.map((c) => (
            <option key={c} value={c}>
              {c}
            </option>
          ))}
        </select>
        <label>
          <input type="checkbox" checked={scannedOnly} onChange={(e) => setScannedOnly(e.target.checked)} /> Scanned only
        </label>
        <label>
          <input type="checkbox" checked={errorOnly} onChange={(e) => setErrorOnly(e.target.checked)} /> Errors only
        </label>
        <select value={order} onChange={(e) => setOrder(e.target.value)}>
          <option value="latest">Latest scan</option>
          <option value="alphabetical">A→Z</option>
          <option value="blames">Blame count</option>
        </select>
      </div>

      {loading ? (
        <p className="muted">Loading…</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th></th>
              <th>Login</th>
              <th>Type</th>
              <th>Coalition</th>
              <th>Scans</th>
              <th>Last scan</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.entry.pk}>
                <td>{row.photoUrl && <img className="avatar" style={{ width: 32, height: 32 }} src={row.photoUrl} alt="" />}</td>
                <td>
                  <Link to={`/users/${row.entry.pk}`}>{row.login || row.entry.fullName}</Link>
                  {row.hasError && <span className="pill" style={{ marginLeft: 6, background: "var(--danger)" }}>error</span>}
                </td>
                <td className="muted">{row.userType}</td>
                <td style={{ color: row.coalitionColor || undefined }}>{row.coalitionName}</td>
                <td>
                  {row.scanCount}
                  {row.pendingCount > 0 && <span className="pill" style={{ marginLeft: 6 }}>{row.pendingCount} pending</span>}
                </td>
                <td className="muted">{row.lastScan ? new Date(row.lastScan).toLocaleString() : "—"}</td>
                <td>
                  <button className="btn danger row-actions" onClick={() => remove(row)}>
                    Delete
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
