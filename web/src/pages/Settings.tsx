import { useEffect, useState, type ReactNode } from "react";
import { Lock, Plus, RefreshCw, X } from "lucide-react";
import { toast } from "sonner";
import { api, type ServiceStatus, type Settings } from "@/lib/api";
import { errText } from "@/lib/actions";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Label, SectionHead, Spinner } from "@/components/kit";

type ServiceKey = "prowlarr" | "sonarr" | "radarr" | "seerr" | "jellyfin" | "qbittorrent";
const SERVICES: { key: ServiceKey; name: string; env: string; hint: string; user?: boolean; required?: boolean }[] = [
  { key: "prowlarr", name: "Prowlarr", env: "PROWLARR", hint: "required · all searching goes through here", required: true },
  { key: "sonarr", name: "Sonarr", env: "SONARR", hint: "shows · found via Prowlarr if left empty" },
  { key: "radarr", name: "Radarr", env: "RADARR", hint: "movies · found via Prowlarr if left empty" },
  { key: "seerr", name: "Seerr", env: "SEERR", hint: "optional · fast search and requests" },
  { key: "jellyfin", name: "Jellyfin", env: "JELLYFIN", hint: "optional · sign-in and instant library refresh" },
  { key: "qbittorrent", name: "qBittorrent", env: "QBIT", hint: "optional · watch-in-order packs and “download anyway”", user: true },
];
const RES_ORDER = [2160, 1080, 720, 576, 480];
const resName = (r: number) => (r === 2160 ? "4K" : `${r}p`);
const VIDEO_CODECS = ["av1", "x265", "x264"];
const AUDIO_CODECS = ["Opus", "AAC", "DDP", "DD", "DTS", "DTS-HD MA", "TrueHD", "Atmos", "FLAC"];
const codecName = (c: string) => ({ av1: "AV1", x265: "HEVC / x265", x264: "H.264 / x264" })[c] ?? c;
const SIZES = [
  ["smaller", "Smaller", "Strongly favour compact files."],
  ["balanced", "Balanced", "Mildly favour compact files."],
  ["ignore", "Doesn't matter", "Size only counts for the max-size limit."],
  ["larger", "Larger", "Favour higher bitrates, up to the max size."],
] as const;

export function SettingsPage() {
  const [saved, setSaved] = useState<Settings | null>(null);
  const [s, setS] = useState<Settings | null>(null);
  const [locked, setLocked] = useState<Record<string, boolean>>({});
  const [status, setStatus] = useState<Record<string, ServiceStatus>>({});
  const [version, setVersion] = useState("");
  const [saving, setSaving] = useState(false);
  const [checking, setChecking] = useState(false);
  const [tab, setTab] = useState(() => (location.hash.slice(1) || "connections"));

  const loadStatus = (fresh = false) => {
    setChecking(true);
    return api
      .status(fresh)
      .then((r) => {
        setStatus(r.services);
        setVersion(r.version);
      })
      .catch(() => {})
      .finally(() => setChecking(false));
  };
  useEffect(() => {
    api.settings().then((r) => {
      setS(r.settings);
      setSaved(r.settings);
      setLocked(r.locked);
    });
    loadStatus();
  }, []);

  if (!s || !saved) return <Skeleton className="h-[480px] rounded-2xl" />;

  const changes = countChanges(saved, s);
  const update = (f: (x: Settings) => void) => {
    const next = structuredClone(s);
    f(next);
    setS(next);
  };
  const save = async () => {
    setSaving(true);
    try {
      const r = await api.saveSettings(s);
      setS(r.settings);
      setSaved(r.settings);
      setLocked(r.locked);
      toast.success("Saved. Changes apply right away.");
      setTimeout(() => loadStatus(true), 600);
    } catch (e) {
      toast.error(errText(e));
    } finally {
      setSaving(false);
    }
  };

  const p = s.prefs;
  const b = s.behaviour;
  const connected = SERVICES.filter((x) => status[x.key]?.ok).length;

  return (
    <Tabs
      value={tab}
      onValueChange={(v) => {
        setTab(v);
        history.replaceState(null, "", `#${v}`);
      }}
      orientation="vertical"
      className="grid gap-10 md:grid-cols-[250px_minmax(0,1fr)] md:gap-14"
    >
      <div>
        <Label mark>roarr {version}</Label>
        <h1 className="mt-4 font-display text-[clamp(2.75rem,5vw,3.5rem)]">Settings</h1>
        <TabsList className="mt-7 h-auto! w-full flex-row flex-wrap items-stretch rounded-2xl bg-transparent p-0 md:flex-col">
          {(
            [
              ["connections", "Connections", Object.keys(status).length ? `${connected}/${SERVICES.length}` : ""],
              ["picking", "Picking releases", ""],
              ["behaviour", "Behaviour", ""],
              ["signin", "Sign-in", ""],
            ] as const
          ).map(([v, name, extra]) => (
            <TabsTrigger
              key={v}
              value={v}
              className="h-10 flex-none justify-between rounded-full px-4 text-[15px] data-[state=active]:bg-foreground! data-[state=active]:text-background! data-[state=active]:shadow-none md:w-full"
            >
              {name}
              {extra && <span className="font-mono text-[11px] opacity-70">{extra}</span>}
            </TabsTrigger>
          ))}
        </TabsList>
        <p className="mt-6 hidden text-[13px] leading-snug text-muted-foreground md:block">
          Fields with a <Lock className="inline size-3 -translate-y-px" /> are set in your docker-compose file.
        </p>
      </div>

      <div className="min-w-0 pt-1">
        <TabsContent value="connections">
          <SectionHead index={1} title="Connections" className="mb-3">
            <Button variant="outline" size="xs" className="ml-auto" disabled={checking} onClick={() => loadStatus(true)}>
              {checking ? <Spinner className="size-3" /> : <RefreshCw />} Check again
            </Button>
          </SectionHead>
          {SERVICES.map((svc) => (
            <ServiceRow key={svc.key} svc={svc} cfg={s[svc.key]} status={status[svc.key]} locked={locked} update={update} />
          ))}
        </TabsContent>

        <TabsContent value="picking" className="space-y-1">
          <SectionHead index={2} title="Picking releases" className="mb-3" />
          <Field label="Resolutions" hint="In order of preference. roarr skips anything not listed.">
            <Ranked value={p.resolutions} options={RES_ORDER} label={resName} onChange={(v) => update((x) => (x.prefs.resolutions = v))} />
          </Field>
          <Field label="Video codecs" hint="In order of preference. The first gets the biggest bonus; unlisted codecs get none.">
            <Ranked value={p.videoCodecs ?? []} options={VIDEO_CODECS} label={codecName} onChange={(v) => update((x) => (x.prefs.videoCodecs = v))} />
          </Field>
          <Field label="Audio codecs" hint="In order of preference.">
            <Ranked value={p.audioCodecs ?? []} options={AUDIO_CODECS} label={(c) => c} onChange={(v) => update((x) => (x.prefs.audioCodecs = v))} />
          </Field>
          <Field label="File size" hint={SIZES.find(([k]) => k === (p.size || "balanced"))?.[2]}>
            <div role="radiogroup" aria-label="File size" className="inline-flex max-w-full flex-wrap gap-0.5 rounded-[22px] bg-muted p-[3px]">
              {SIZES.map(([k, name]) => {
                const on = (p.size || "balanced") === k;
                return (
                  <button
                    key={k}
                    role="radio"
                    aria-checked={on}
                    onClick={() => update((x) => (x.prefs.size = k))}
                    className={cn(
                      "h-[34px] cursor-pointer rounded-full px-3.5 text-sm font-medium text-foreground/60 transition-colors outline-none hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50",
                      on && "bg-background text-foreground shadow-sm",
                    )}
                  >
                    {name}
                  </button>
                );
              })}
            </div>
          </Field>
          <Field label="Audio language">
            <Input value={p.language} onChange={(e) => update((x) => (x.prefs.language = e.target.value))} placeholder="English" className="max-w-60" />
          </Field>
          <Field label="Max size" hint="Gigabytes per hour of video. 0 means no limit.">
            <div className="flex items-center gap-2">
              <Input type="number" min={0} step={0.5} value={p.maxGbPerHour} onChange={(e) => update((x) => (x.prefs.maxGbPerHour = +e.target.value))} className="w-24" />
              <span className="font-mono text-xs text-muted-foreground">GB / hour</span>
            </div>
          </Field>
          <Field label="Min seeders">
            <Input type="number" min={0} value={p.minSeeders} onChange={(e) => update((x) => (x.prefs.minSeeders = +e.target.value))} className="w-24" />
          </Field>
          <Field label="Prefer">
            <div className="grid gap-3 sm:grid-cols-2">
              <Toggle checked={p.preferDualAudio} onChange={(v) => update((x) => (x.prefs.preferDualAudio = v))}>
                Dual audio
              </Toggle>
              <Toggle checked={p.allowHdr} onChange={(v) => update((x) => (x.prefs.allowHdr = v))}>
                Allow HDR / Dolby Vision
              </Toggle>
              <Toggle checked={p.allowRemux} onChange={(v) => update((x) => (x.prefs.allowRemux = v))}>
                Allow remuxes <Sub>very large</Sub>
              </Toggle>
            </div>
          </Field>
          <Field label="Preferred groups" hint="Comma separated.">
            <ListInput placeholder="NTb, FLUX" value={p.preferredGroups} onChange={(v) => update((x) => (x.prefs.preferredGroups = v))} />
          </Field>
          <Field label="Blocked words" hint="Releases containing any of these are skipped.">
            <ListInput placeholder="HDCAM, YIFY" value={p.blockedTerms} onChange={(v) => update((x) => (x.prefs.blockedTerms = v))} />
          </Field>
        </TabsContent>

        <TabsContent value="behaviour" className="space-y-1">
          <SectionHead index={3} title="Behaviour" className="mb-3" />
          <Field label="Play in order" hint="Download season packs episode by episode and import each one as soon as it's done. Needs qBittorrent.">
            <Toggle checked={b.watchFirst} onChange={(v) => update((x) => (x.behaviour.watchFirst = v))}>
              On by default
            </Toggle>
          </Field>
          <Field label="Search early" hint="Start searching a show as soon as its page opens, so the best pick is ready when you are.">
            <Toggle checked={b.prefetch} onChange={(v) => update((x) => (x.behaviour.prefetch = v))}>
              Prefetch
            </Toggle>
          </Field>
          <Field label="Seerr requests" hint="Fetch approved requests automatically. Turn on Seerr's “prevent search” for its Sonarr/Radarr servers to avoid double grabs.">
            <Toggle checked={b.seerrAutoFetch} disabled={locked.SEERR_AUTO_FETCH} onChange={(v) => update((x) => (x.behaviour.seerrAutoFetch = v))}>
              Get approved requests
            </Toggle>
          </Field>
          <Field label="Indexer timeout" hint="Seconds before a slow indexer is given up on.">
            <Num value={b.indexerTimeout} min={5} unit="seconds" disabled={locked.INDEXER_TIMEOUT} onChange={(v) => update((x) => (x.behaviour.indexerTimeout = v))} />
          </Field>
          <Field label="Result cache" hint="How long indexer answers are reused.">
            <Num value={b.cacheMinutes} min={1} unit="minutes" disabled={locked.CACHE_MINUTES} onChange={(v) => update((x) => (x.behaviour.cacheMinutes = v))} />
          </Field>
        </TabsContent>

        <TabsContent value="signin" className="space-y-1">
          <SectionHead index={4} title="Sign-in" className="mb-3" />
          <Field label="Who can sign in">
            <Select value={s.authMode} disabled={locked.AUTH_MODE} onValueChange={(v) => update((x) => (x.authMode = v))}>
              <SelectTrigger className="h-10 w-64 rounded-[10px] bg-card">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="jellyfin">Jellyfin accounts</SelectItem>
                <SelectItem value="password">One shared password</SelectItem>
                <SelectItem value="none">Anyone (trusted network only)</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          {s.authMode === "jellyfin" && (
            <Field label="Admins only">
              <Toggle checked={s.jellyfinAdminOnly} disabled={locked.JELLYFIN_ADMIN_ONLY} onChange={(v) => update((x) => (x.jellyfinAdminOnly = v))}>
                Only Jellyfin administrators
              </Toggle>
            </Field>
          )}
          {s.authMode === "password" && (
            <Field label="Password">
              <Input type="password" disabled={locked.AUTH_PASSWORD} value={s.authPassword ?? ""} onChange={(e) => update((x) => (x.authPassword = e.target.value))} className="max-w-72" />
            </Field>
          )}
        </TabsContent>
      </div>

      {changes > 0 && (
        <div className="fixed inset-x-0 bottom-6 z-40 flex justify-center px-4">
          <div className="flex items-center gap-3 rounded-full bg-ink py-2 pr-2 pl-5 text-paper shadow-2xl dark:bg-paper dark:text-ink">
            <span className="text-sm">
              {changes} unsaved change{changes > 1 ? "s" : ""}
            </span>
            <Button size="sm" variant="ghost" className="hover:bg-paper/15 dark:hover:bg-ink/10" onClick={() => setS(saved)} disabled={saving}>
              Discard
            </Button>
            <Button size="sm" className="bg-lime text-ink hover:bg-lime/85" onClick={save} disabled={saving}>
              {saving && <Spinner />} Save
            </Button>
          </div>
        </div>
      )}
    </Tabs>
  );
}

function ServiceRow({
  svc,
  cfg,
  status,
  locked,
  update,
}: {
  svc: (typeof SERVICES)[number];
  cfg: Settings[ServiceKey];
  status?: ServiceStatus;
  locked: Record<string, boolean>;
  update: (f: (x: Settings) => void) => void;
}) {
  const broken = !!status && !status.ok && (status.configured || !!svc.required);
  const [open, setOpen] = useState(false);
  useEffect(() => {
    if (broken) setOpen(true);
  }, [broken]);
  const lockedUrl = locked[`${svc.env}_URL`];
  const allLocked = lockedUrl && (svc.user ? locked[`${svc.env}_PASSWORD`] : locked[`${svc.env}_API_KEY`]);
  const line = status?.ok ? [`v${status.version}`, `${status.ms} ms`, svc.hint].join(" · ") : status?.configured ? status.error : svc.hint;

  return (
    <div className={cn("border-t border-dashed border-dash", open && "my-2 rounded-2xl border-transparent bg-muted/70 px-4 md:-mx-4")}>
      <div className="grid grid-cols-[10px_minmax(0,1fr)_auto] items-center gap-x-3.5 gap-y-1 py-4 sm:grid-cols-[10px_140px_minmax(0,1fr)_auto]">
        <i className={cn("size-2 rounded-full", status?.ok ? "bg-ok" : broken ? "bg-destructive" : "bg-muted-foreground/40")} />
        <b className="text-base font-semibold tracking-tight">{svc.name}</b>
        <span className={cn("col-start-2 row-start-2 truncate font-mono text-[11.5px] sm:col-start-3 sm:row-start-1", broken ? "text-destructive" : "text-muted-foreground")} title={line}>
          {line ?? "…"}
        </span>
        <div className="col-start-3 row-start-1 sm:col-start-4">
          {allLocked ? (
            <Badge>
              <Lock /> env
            </Badge>
          ) : (
            <Button variant={open ? "outline" : "ghost"} size="xs" onClick={() => setOpen(!open)}>
              {open ? "Done" : status?.configured || cfg.url ? "Edit" : "Connect"}
            </Button>
          )}
        </div>
      </div>
      {open && !allLocked && (
        <div className="grid gap-x-4 gap-y-2.5 pb-4 sm:grid-cols-[110px_minmax(0,1fr)] sm:items-center sm:pl-6">
          <FieldLabel>URL</FieldLabel>
          <LockedInput
            locked={lockedUrl}
            placeholder={status?.url && !cfg.url ? `${status.url} (found automatically)` : "http://host:port"}
            value={cfg.url}
            onChange={(v) => update((x) => (x[svc.key].url = v))}
          />
          {svc.user ? (
            <>
              <FieldLabel>Username</FieldLabel>
              <LockedInput locked={locked[`${svc.env}_USERNAME`]} value={cfg.username ?? ""} onChange={(v) => update((x) => (x[svc.key].username = v))} />
              <FieldLabel>Password</FieldLabel>
              <LockedInput type="password" locked={locked[`${svc.env}_PASSWORD`]} value={cfg.password ?? ""} onChange={(v) => update((x) => (x[svc.key].password = v))} />
            </>
          ) : (
            <>
              <FieldLabel>API key</FieldLabel>
              <LockedInput type="password" locked={locked[`${svc.env}_API_KEY`]} value={cfg.apiKey ?? ""} onChange={(v) => update((x) => (x[svc.key].apiKey = v))} />
            </>
          )}
        </div>
      )}
    </div>
  );
}

/** Ordered preference list: chips in rank order (× removes), then the unused options (+ appends last). */
function Ranked<T extends string | number>({ value, options, label, onChange }: { value: T[]; options: readonly T[]; label: (v: T) => string; onChange: (v: T[]) => void }) {
  return (
    <div className="flex flex-wrap gap-1.5">
      {value.map((v, i) => (
        <span key={v} className="inline-flex h-8 items-center gap-1.5 rounded-full bg-foreground pr-1 pl-3 text-[13px] font-medium text-background">
          <span className="font-mono text-[11px] opacity-60">{i + 1}</span>
          {label(v)}
          <button
            aria-label={`Remove ${label(v)}`}
            className="grid size-6 cursor-pointer place-items-center rounded-full hover:bg-background/20"
            onClick={() => onChange(value.filter((y) => y !== v))}
          >
            <X className="size-3" />
          </button>
        </span>
      ))}
      {options
        .filter((v) => !value.includes(v))
        .map((v) => (
          <Button key={v} variant="outline" size="sm" onClick={() => onChange([...value, v])}>
            <Plus /> {label(v)}
          </Button>
        ))}
    </div>
  );
}

function FieldLabel({ children }: { children: ReactNode }) {
  return <span className="label">{children}</span>;
}

function LockedInput({ locked, type, value, placeholder, onChange }: { locked?: boolean; type?: string; value: string; placeholder?: string; onChange: (v: string) => void }) {
  return (
    <div className="relative">
      <Input type={type} disabled={locked} value={value} placeholder={placeholder} onChange={(e) => onChange(e.target.value)} className={cn(locked && "pr-9")} />
      {locked && <Lock aria-label="Set by environment" className="absolute top-1/2 right-3 size-3.5 -translate-y-1/2 text-muted-foreground" />}
    </div>
  );
}

function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <div className="grid gap-x-8 gap-y-2.5 border-t border-dashed border-dash py-5 sm:grid-cols-[200px_minmax(0,1fr)]">
      <div>
        <div className="font-semibold tracking-tight">{label}</div>
        {hint && <p className="mt-1 text-[13px] leading-snug text-muted-foreground">{hint}</p>}
      </div>
      <div className="min-w-0 self-center">{children}</div>
    </div>
  );
}

function Toggle({ checked, disabled, onChange, children }: { checked: boolean; disabled?: boolean; onChange: (v: boolean) => void; children: ReactNode }) {
  return (
    <label className={cn("flex cursor-pointer items-center gap-3 text-[15px]", disabled && "cursor-not-allowed opacity-60")}>
      <Switch checked={checked} disabled={disabled} onCheckedChange={onChange} />
      <span>{children}</span>
      {disabled && <Lock className="size-3 text-muted-foreground" />}
    </label>
  );
}

function Sub({ children }: { children: ReactNode }) {
  return <span className="ml-1 font-mono text-[11px] text-muted-foreground">{children}</span>;
}

function Num({ value, min, unit, disabled, onChange }: { value: number; min: number; unit: string; disabled?: boolean; onChange: (v: number) => void }) {
  return (
    <div className="flex items-center gap-2">
      <Input type="number" min={min} value={value} disabled={disabled} onChange={(e) => onChange(+e.target.value)} className="w-24" />
      <span className="font-mono text-xs text-muted-foreground">{unit}</span>
      {disabled && <Lock className="size-3 text-muted-foreground" />}
    </div>
  );
}

/** Comma-separated list; parsed on blur so typing a comma isn't swallowed. */
function ListInput({ value, placeholder, onChange }: { value: string[] | null; placeholder: string; onChange: (v: string[]) => void }) {
  const joined = (value ?? []).join(", ");
  const [text, setText] = useState(joined);
  useEffect(() => setText(joined), [joined]);
  return (
    <Input
      placeholder={placeholder}
      value={text}
      onChange={(e) => {
        setText(e.target.value);
        onChange(splitList(e.target.value));
      }}
      onBlur={() => setText(joined)}
    />
  );
}

function splitList(v: string): string[] {
  return v
    .split(",")
    .map((x) => x.trim())
    .filter(Boolean);
}

/** Number of leaf values that differ, for the save bar. */
function countChanges(a: unknown, b: unknown): number {
  if (Array.isArray(a) || Array.isArray(b)) return JSON.stringify(a ?? []) === JSON.stringify(b ?? []) ? 0 : 1;
  if (a && b && typeof a === "object" && typeof b === "object") {
    const keys = new Set([...Object.keys(a), ...Object.keys(b)]);
    let n = 0;
    for (const k of keys) n += countChanges((a as Record<string, unknown>)[k], (b as Record<string, unknown>)[k]);
    return n;
  }
  return (a ?? "") === (b ?? "") ? 0 : 1;
}
