import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, ApiError } from "../api/client";
import type { ClusterData } from "../api/types";

export function ClustersPage() {
  const navigate = useNavigate();
  const [data, setData] = useState<ClusterData | null>(null);
  const [activeCluster, setActiveCluster] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  function load(force = false) {
    setLoading(true);
    setError(null);
    api
      .getClusters(force)
      .then((d) => {
        // Go nil slices/maps marshal to JSON null, not []/{} — normalize
        // once here rather than guarding every usage site below.
        const normalized: ClusterData = { clusters: d.clusters ?? [], layouts: d.layouts ?? {}, occupants: d.occupants ?? {} };
        setData(normalized);
        if (normalized.clusters.length > 0) setActiveCluster((cur) => cur ?? normalized.clusters[0].id);
      })
      .catch((err) => setError(err instanceof ApiError ? err.message : "Failed to load clusters"))
      .finally(() => setLoading(false));
  }

  useEffect(() => load(), []);

  async function refreshOccupants() {
    const occupants = await api.refreshOccupants();
    setData((d) => (d ? { ...d, occupants } : d));
  }

  async function openSeatOccupant(login: string) {
    const res = await api.listUsers({ query: login });
    const match = (res.rows ?? []).find((r) => r.login?.toLowerCase() === login.toLowerCase());
    if (match) navigate(`/users/${match.entry.pk}`);
  }

  if (loading) return <p className="muted">Loading…</p>;
  if (error) return <div className="error-box">{error}</div>;
  if (!data || data.clusters.length === 0) return <p className="muted">No clusters configured for this campus.</p>;

  const layout = activeCluster != null ? data.layouts[activeCluster] : undefined;

  return (
    <div>
      <div className="row" style={{ justifyContent: "space-between" }}>
        <h1>Clusters</h1>
        <div className="row">
          <button className="btn secondary" onClick={refreshOccupants}>
            Refresh occupants
          </button>
          <button className="btn secondary" onClick={() => load(true)}>
            Reload clusters
          </button>
        </div>
      </div>

      <div className="row" style={{ flexWrap: "wrap", marginBottom: 12 }}>
        {data.clusters.map((c) => (
          <button
            key={c.id}
            className={c.id === activeCluster ? "btn" : "btn secondary"}
            onClick={() => setActiveCluster(c.id)}
          >
            {c.name}
          </button>
        ))}
      </div>

      {layout && (
        <div className="card">
          <svg viewBox={`0 0 ${layout.viewBoxWidth} ${layout.viewBoxHeight}`} style={{ width: "100%", maxHeight: 700 }}>
            {layout.rowLabels.map((label, i) => (
              <text key={i} x={label.x} y={label.y} fontSize={16} fill="#9199a8">
                {label.text}
              </text>
            ))}
            {layout.seats.map((seat) => {
              const occupant = data.occupants[seat.host];
              return (
                <rect
                  key={seat.host}
                  x={seat.x}
                  y={seat.y}
                  width={seat.width}
                  height={seat.height}
                  fill={occupant ? "#4180db" : "#2a2e38"}
                  stroke="#7f7f7f"
                  style={{ cursor: occupant ? "pointer" : "default" }}
                  onClick={() => occupant && openSeatOccupant(occupant.login)}
                >
                  <title>{occupant ? occupant.login : seat.host}</title>
                </rect>
              );
            })}
          </svg>
        </div>
      )}
    </div>
  );
}
