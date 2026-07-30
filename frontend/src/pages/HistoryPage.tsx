import { useEffect, useState } from "react";
import { api } from "../api/client";
import { useEvents } from "../events/EventsContext";
import type { ApiKeyUsageEntry } from "../api/types";

export function HistoryPage() {
  const [rows, setRows] = useState<ApiKeyUsageEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const { subscribe } = useEvents();

  useEffect(() => {
    api
      .listAllApiKeyUsage()
      .then((r) => setRows(r ?? []))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    return subscribe("lookup", (event) => {
      setRows((prev) => [
        {
          badger: event.keyName,
          uidHex: event.uidHex,
          login: event.login,
          coalitionName: event.coalitionName,
          coalitionColor: event.coalitionColor,
          found: event.found,
          timestamp: event.timestamp,
        },
        ...prev,
      ]);
    });
  }, [subscribe]);

  return (
    <div>
      <h1>History</h1>
      <p className="muted">Every badge lookup performed by any API key, newest first — updates live.</p>

      {loading ? (
        <p className="muted">Loading…</p>
      ) : (rows ?? []).length === 0 ? (
        <p className="muted">No badge lookups recorded yet.</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>Badger</th>
              <th>User</th>
              <th>Coalition</th>
              <th>Scanned at</th>
            </tr>
          </thead>
          <tbody>
            {(rows ?? []).map((r, i) => (
              <tr key={i}>
                <td>{r.badger}</td>
                <td>{r.found ? r.login : <span className="muted">not found ({r.uidHex})</span>}</td>
                <td style={{ color: r.coalitionColor || undefined }}>{r.coalitionName}</td>
                <td className="muted">{new Date(r.timestamp).toLocaleString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
