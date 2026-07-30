import { NavLink } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";

export function Nav() {
  const { user, logout } = useAuth();
  return (
    <nav className="nav">
      <NavLink to="/history">History</NavLink>
      <NavLink to="/users">Users</NavLink>
      <NavLink to="/clusters">Clusters</NavLink>
      {user?.isAdmin && <NavLink to="/admin">Admin</NavLink>}
      <div style={{ flex: 1 }} />
      <div className="muted" style={{ padding: "8px 12px", fontSize: 13 }}>
        {user?.username}
      </div>
      <button className="btn secondary" onClick={() => logout()}>
        Log out
      </button>
    </nav>
  );
}
