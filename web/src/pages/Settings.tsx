import { useEffect, useRef, useState } from "react";
import { Lock, Plus, RefreshCw, X } from "lucide-react";
import { toast } from "sonner";
import { api, type ServiceStatus, type Settings } from "@/lib/api";
import { errText } from "@/lib/actions";
import { cached, peek, put } from "@/lib/cache";
import { countChanges } from "@/lib/settings";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { SectionHead, Spinner } from "@/components/kit";
import { Field, FieldLabel, LockedInput, Num, Segmented, Sub, Toggle } from "@/components/AdminFields";
import { ChecksTab, ImportTab, TranscodeTab } from "./AdminMedia";

type ServiceKey = "prowlarr" | "sonarr" | "radarr" | "seerr" | "jellyfin" | "qbittorrent";
const SERVICES: { key: ServiceKey; name: string; env: string; hint: string; user?: boolean; required?: boolean }[] = [
  { key: "prowlarr", name: "Prowlarr", env: "PROWLARR", hint: "required, all searching", required: true },
  { key: "sonarr", name: "Sonarr", env: "SONARR", hint: "shows, found via Prowlarr if empty" },
  { key: "radarr", name: "Radarr", env: "RADARR", hint: "movies, found via Prowlarr if empty" },
  { key: "seerr", name: "Seerr", env: "SEERR", hint: "optional, TMDB search and requests" },
  { key: "jellyfin", name: "Jellyfin", env: "JELLYFIN", hint: "optional, sign-in and library refresh" },
  { key: "qbittorrent", name: "qBittorrent", env: "QBIT", hint: "optional, watch-first packs and download anyway", user: true },
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

type SettingsAnswer = { settings: Settings; locked: Record<string, boolean> };

export function SettingsPage() {
  // A hover on the Admin link usually filled the cache already.
  const [first] = useState(() => peek<SettingsAnswer>("settings"));
  const [saved, setSaved] = useState<Settings | null>(first?.settings ?? null);
  const [s, setS] = useState<Settings | null>(first?.settings ?? null);
  const [locked, setLocked] = useState<Record<string, boolean>>(first?.locked ?? {});
  const edited = useRef(false);
  const [status, setStatus] = useState<Record<string, ServiceStatus>>({});
  const [version, setVersion] = useState("");
  const [saving, setSaving] = useState(false);
  const [checking, setChecking] = useState(false);
  const [tab, setTab] = useState(() => location.hash.slice(1) || "picking");

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
    // Check the cached copy against the server, unless the form is already being changed.
    cached("settings", api.settings, 5_000).then((r) => {
      if (edited.current) return;
      setS(r.settings);
      setSaved(r.settings);
      setLocked(r.locked);
    });
    loadStatus();
  }, []);

  if (!s || !saved) return <Skeleton className="h-[480px]" />;

  const changes = countChanges(saved, s);
  const update = (f: (x: Settings) => void) => {
    const next = structuredClone(s);
    f(next);
    edited.current = true;
    setS(next);
  };
  const save = async () => {
    setSaving(true);
    try {
      const r = await api.saveSettings(s);
      put("settings", r);
      edited.current = false;
      setS(r.settings);
      setSaved(r.settings);
      setLocked(r.locked);
      toast.success("Saved");
      setTimeout(() => loadStatus(true), 600);
    } catch (e) {
      toast.error(errText(e));
    } finally {
      setSaving(false);
    }
  };

  const p = s.prefs;
  const b = s.behaviour;

  return (
    <Tabs
      value={tab}
      onValueChange={(v) => {
        setTab(v);
        history.replaceState(null, "", `#${v}`);
      }}
      orientation="vertical"
      className="grid gap-8 md:grid-cols-[220px_minmax(0,1fr)] md:gap-12"
    >
      <div>
        <h1 className="text-[24px] leading-8 font-semibold">Admin</h1>
        <p className="meta mt-1 font-mono">roarr {version}</p>
        <TabsList className="mt-6 h-auto! w-full flex-row flex-wrap items-stretch gap-1 rounded-none bg-transparent p-0 md:flex-col">
          {(
            [
              ["picking", "Release rules", ""],
              ["behaviour", "Behaviour", ""],
              ["import", "Import", ""],
              ["checks", "Checks", ""],
              ["av1", "AV1", ""],
              ["connections", "Connections", ""],
              ["signin", "Sign-in", ""],
            ] as const
          ).map(([v, name]) => (
            <TabsTrigger
              key={v}
              value={v}
              className="h-9 flex-none justify-between rounded-[2px] border-0 px-3 text-[14px] font-normal text-muted-foreground data-[state=active]:bg-card! data-[state=active]:text-foreground data-[state=active]:shadow-none md:w-full"
            >
              {name}
            </TabsTrigger>
          ))}
        </TabsList>
      </div>

      <div className="min-w-0 pt-1">
        <TabsContent value="connections">
          <SectionHead title="Connections" className="mb-3">
            <Button variant="outline" size="xs" className="ml-auto" disabled={checking} onClick={() => loadStatus(true)}>
              {checking ? <Spinner className="size-3" /> : <RefreshCw />} Check again
            </Button>
          </SectionHead>
          {SERVICES.map((svc) => (
            <ServiceRow key={svc.key} svc={svc} cfg={s[svc.key]} status={status[svc.key]} locked={locked} update={update} />
          ))}
        </TabsContent>

        <TabsContent value="picking" className="space-y-1">
          <SectionHead title="Release rules" className="mb-3" />
          <Field label="Resolutions" hint="In order of preference. Others are rejected.">
            <Ranked value={p.resolutions} options={RES_ORDER} label={resName} onChange={(v) => update((x) => (x.prefs.resolutions = v))} />
          </Field>
          <Field label="Video codecs" hint="In order of preference. Unlisted codecs get no bonus.">
            <Ranked value={p.videoCodecs ?? []} options={VIDEO_CODECS} label={codecName} onChange={(v) => update((x) => (x.prefs.videoCodecs = v))} />
          </Field>
          <Field label="Audio codecs" hint="In order of preference.">
            <Ranked value={p.audioCodecs ?? []} options={AUDIO_CODECS} label={(c) => c} onChange={(v) => update((x) => (x.prefs.audioCodecs = v))} />
          </Field>
          <Field label="File size" hint={SIZES.find(([k]) => k === (p.size || "balanced"))?.[2]}>
            <Segmented label="File size" value={p.size || "balanced"} options={SIZES.map(([k, name]) => [k, name] as const)} onChange={(k) => update((x) => (x.prefs.size = k))} />
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
          <Field label="Blocked words" hint="Releases containing any of these are rejected.">
            <ListInput placeholder="HDCAM, YIFY" value={p.blockedTerms} onChange={(v) => update((x) => (x.prefs.blockedTerms = v))} />
          </Field>
        </TabsContent>

        <TabsContent value="behaviour" className="space-y-1">
          <SectionHead title="Behaviour" className="mb-3" />
          <Field label="Watch first" hint="Download packs episode by episode and import each as it finishes. Needs qBittorrent.">
            <Toggle checked={b.watchFirst} disabled={locked.WATCH_FIRST} onChange={(v) => update((x) => (x.behaviour.watchFirst = v))}>
              On by default
            </Toggle>
          </Field>
          <Field label="Search early" hint="Search a show as soon as its page opens.">
            <Toggle checked={b.prefetch} disabled={locked.PREFETCH} onChange={(v) => update((x) => (x.behaviour.prefetch = v))}>
              Prefetch
            </Toggle>
          </Field>
          <Field label="Seerr requests" hint="Fetch approved requests automatically. Turn on Seerr's prevent-search for its Sonarr/Radarr servers to avoid double grabs.">
            <Toggle checked={b.seerrAutoFetch} disabled={locked.SEERR_AUTO_FETCH} onChange={(v) => update((x) => (x.behaviour.seerrAutoFetch = v))}>
              Get approved requests
            </Toggle>
          </Field>
          <Field label="Indexer timeout" hint="Seconds before a slow indexer is skipped.">
            <Num value={b.indexerTimeout} min={5} unit="seconds" disabled={locked.INDEXER_TIMEOUT} onChange={(v) => update((x) => (x.behaviour.indexerTimeout = v))} />
          </Field>
          <Field label="Result cache" hint="How long indexer answers are reused.">
            <Num value={b.cacheMinutes} min={1} unit="minutes" disabled={locked.CACHE_MINUTES} onChange={(v) => update((x) => (x.behaviour.cacheMinutes = v))} />
          </Field>
        </TabsContent>

        <TabsContent value="import" className="space-y-1">
          <ImportTab s={s} locked={locked} update={update} />
        </TabsContent>

        <TabsContent value="checks" className="space-y-1">
          <ChecksTab s={s} locked={locked} update={update} />
        </TabsContent>

        <TabsContent value="av1" className="space-y-1">
          <TranscodeTab s={s} locked={locked} update={update} />
        </TabsContent>

        <TabsContent value="signin" className="space-y-1">
          <SectionHead title="Sign-in" className="mb-3" />
          <Field label="Who can sign in">
            <Select value={s.authMode} disabled={locked.AUTH_MODE} onValueChange={(v) => update((x) => (x.authMode = v))}>
              <SelectTrigger className="h-9 w-64 bg-card">
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
          <div className="flex items-center gap-3 rounded-[2px] border bg-popover py-2 pr-2 pl-4">
            <span className="text-sm">
              {changes} unsaved change{changes > 1 ? "s" : ""}
            </span>
            <Button size="sm" variant="ghost" onClick={() => setS(saved)} disabled={saving}>
              Discard
            </Button>
            <Button size="sm" onClick={save} disabled={saving}>
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
  const line = status?.ok ? [`v${status.version}`, `${status.ms} ms`, svc.hint].join(", ") : status?.configured ? status.error : svc.hint;

  return (
    <div className={cn("border-t", open && "my-2 rounded-[2px] border-transparent bg-card px-4 md:-mx-4")}>
      <div className="grid grid-cols-[8px_minmax(0,1fr)_auto] items-center gap-x-4 gap-y-1 py-4 sm:grid-cols-[8px_140px_minmax(0,1fr)_auto]">
        <i className={cn("size-2 rounded-[1px]", status?.ok ? "bg-have" : broken ? "bg-bad" : "bg-unlit")} />
        <b className="text-[14px] font-medium">{svc.name}</b>
        <span className={cn("col-start-2 row-start-2 truncate font-mono text-[12px] sm:col-start-3 sm:row-start-1", broken ? "text-bad" : "text-muted-foreground")} title={line}>
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
        <div className="grid gap-x-4 gap-y-3 pb-4 sm:grid-cols-[110px_minmax(0,1fr)] sm:items-center sm:pl-6">
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
    <div className="flex flex-wrap gap-2">
      {value.map((v, i) => (
        <span key={v} className="inline-flex h-8 items-center gap-2 rounded-[2px] bg-accent pr-1 pl-3 text-[13px] font-medium text-foreground">
          <span className="font-mono text-[11px] text-muted-foreground">{i + 1}</span>
          {label(v)}
          <button
            aria-label={`Remove ${label(v)}`}
            className="grid size-6 cursor-pointer place-items-center rounded-[2px] text-muted-foreground hover:bg-background/40 hover:text-foreground"
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
