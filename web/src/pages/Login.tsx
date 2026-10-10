import { useState } from "react";
import { ArrowRight } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label, Notice, Spinner } from "@/components/kit";

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
    <div className="grid min-h-svh md:grid-cols-[1.1fr_1fr]">
      <div className="relative flex flex-col justify-between overflow-hidden bg-lime p-8 text-ink sm:p-12">
        <Label className="text-ink/60">┌─ One quiet UI for your media stack</Label>
        <div className="font-display text-[clamp(5.5rem,20vw,15rem)] leading-[0.8] tracking-[-0.07em]">
          roarr
          <span className="ml-2 inline-block size-[0.16em] rounded-full bg-ink align-baseline" />
        </div>
        <div className="mt-8 font-mono text-[11px] tracking-wider text-ink/60 uppercase">Sonarr · Radarr · Prowlarr · Seerr · Jellyfin</div>
      </div>
      <form onSubmit={submit} className="flex flex-col justify-center gap-4 p-8 sm:p-12 md:max-w-md">
        <Label mark>Sign in</Label>
        <h1 className="font-display text-5xl">Welcome back.</h1>
        <p className="text-muted-foreground">{mode === "jellyfin" ? "Use your Jellyfin account." : "Enter the roarr password."}</p>
        {mode === "jellyfin" && (
          <Input autoFocus placeholder="Username" aria-label="Username" value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" className="h-12" />
        )}
        <Input
          type="password"
          autoFocus={mode !== "jellyfin"}
          placeholder="Password"
          aria-label="Password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="current-password"
          className="h-12"
        />
        {error && <Notice tone="bad">{error}</Notice>}
        <Button size="lg" disabled={busy} className="mt-2">
          {busy ? <Spinner /> : <>Sign in <ArrowRight /></>}
        </Button>
      </form>
    </div>
  );
}
