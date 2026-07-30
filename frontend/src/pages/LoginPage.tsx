import { useState, type FormEvent } from "react";
import { useAuth } from "../auth/AuthContext";
import { ApiError } from "../api/client";

export function LoginPage() {
  const { login } = useAuth();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      await login(username, password);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Login failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div style={{ display: "flex", alignItems: "center", justifyContent: "center", minHeight: "100vh" }}>
      <form onSubmit={onSubmit} className="card" style={{ width: 320 }}>
        <h2>Badge Scanner</h2>
        {error && <div className="error-box">{error}</div>}
        <div style={{ marginBottom: 12 }}>
          <label htmlFor="username" style={{ display: "block", marginBottom: 4 }}>
            Username
          </label>
          <input
            id="username"
            style={{ width: "100%" }}
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoFocus
          />
        </div>
        <div style={{ marginBottom: 16 }}>
          <label htmlFor="password" style={{ display: "block", marginBottom: 4 }}>
            Password
          </label>
          <input
            id="password"
            type="password"
            style={{ width: "100%" }}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </div>
        <button className="btn" type="submit" disabled={busy} style={{ width: "100%" }}>
          {busy ? "Logging in…" : "Log in"}
        </button>
      </form>
    </div>
  );
}
