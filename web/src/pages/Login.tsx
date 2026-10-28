import { useState } from "react";
import { ArrowRight } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Notice, Spinner } from "@/components/kit";
import { Wordmark } from "@/components/Nav";

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
    <div className="grid min-h-svh place-items-center p-6">
      <form onSubmit={submit} className="flex w-full max-w-[360px] flex-col gap-3">
        <Wordmark className="mb-4" />
        <h1 className="text-[22px] font-medium">Sign in</h1>
        <p className="meta -mt-1">{mode === "jellyfin" ? "Jellyfin account" : "roarr password"}</p>
        {mode === "jellyfin" && (
          <Input autoFocus placeholder="Username" aria-label="Username" value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" className="h-10 bg-card" />
        )}
        <Input
          type="password"
          autoFocus={mode !== "jellyfin"}
          placeholder="Password"
          aria-label="Password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="current-password"
          className="h-10 bg-card"
        />
        {error && <Notice tone="bad">{error}</Notice>}
        <Button size="lg" disabled={busy} className="mt-1">
          {busy ? <Spinner /> : <>Sign in <ArrowRight /></>}
        </Button>
      </form>
    </div>
  );
}
