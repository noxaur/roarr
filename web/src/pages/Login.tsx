import { useState } from "react";
import { api } from "../lib/api";
import { Logo } from "../components/Icons";

export function Login({ mode, onDone }: { mode: string; onDone: () => void }) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.login(username, password);
      onDone();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <form className="login" onSubmit={submit}>
      <div className="row" style={{ justifyContent: "center", marginBottom: 8 }}>
        <Logo width={34} height={34} />
        <b style={{ fontSize: 20 }}>roarr</b>
      </div>
      <div className="small muted" style={{ textAlign: "center" }}>
        {mode === "jellyfin" ? "Sign in with your Jellyfin account" : "Enter the roarr password"}
      </div>
      {mode === "jellyfin" && <input className="field" autoFocus placeholder="Username" value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" />}
      <input
        className="field"
        type="password"
        autoFocus={mode !== "jellyfin"}
        placeholder="Password"
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        autoComplete="current-password"
      />
      {error && <div className="notice bad small">{error}</div>}
      <button className="btn primary" disabled={busy}>
        {busy ? <span className="spinner" /> : "Sign in"}
      </button>
    </form>
  );
}
