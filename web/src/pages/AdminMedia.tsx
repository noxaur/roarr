import { useEffect, useState } from "react";
import { Pause, Play, Plus, X } from "lucide-react";
import { api, type ImportsStatus, type NativeJob, type ScheduleWindow, type Settings, type TranscodeJob, type TranscodeStatus, type VerifyList } from "@/lib/api";
import { bytes, pad, pct } from "@/lib/format";
import { badPathPairs, setVerifyMode, verifyMode, type VerifyMode } from "@/lib/settings";
import { baseName, holdText, isPending, newWindow, savedShare, WEEK } from "@/lib/transcode";
import { outcome } from "@/lib/verify";
import { cn } from "@/lib/utils";
import { Field, LockedInput, Num, Segmented, Toggle } from "@/components/AdminFields";
import { Bar, Dot, Figure, Notice, SameWidth, SectionHead, State, type Tone } from "@/components/kit";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

// Admin tabs for the media pipeline: who imports, the download checks, and
// AV1 conversion. Each edits the shared settings copy; the page's save bar
// sends the whole object back.

type Edit = { s: Settings; locked: Record<string, boolean>; update: (f: (x: Settings) => void) => void };

/** Polls an endpoint while the tab is open. The setter takes a fresher answer, such as one from an action. */
function usePoll<T>(load: () => Promise<T>, ms: number): [T | null, (v: T) => void] {
  const [v, setV] = useState<T | null>(null);
  useEffect(() => {
    let stop = false;
    const tick = () =>
      load()
        .then((x) => !stop && setV(x))
        .catch(() => {});
    tick();
    const t = setInterval(() => !document.hidden && tick(), ms);
    return () => {
      stop = true;
      clearInterval(t);
    };
  }, [load, ms]);
  return [v, setV];
}

function PathMap({ value, locked, placeholder, onChange }: { value?: string; locked?: boolean; placeholder: string; onChange: (v: string) => void }) {
  const bad = badPathPairs(value);
  return (
    <>
      <LockedInput mono locked={locked} value={value ?? ""} placeholder={placeholder} onChange={onChange} />
      {bad.length > 0 && <p className="mt-2 font-mono text-[12px] text-bad">ignored: {bad.join(", ")}</p>}
    </>
  );
}

// ---- Import ------------------------------------------------------------------

const MODES = [
  ["arr", "Sonarr / Radarr"],
  ["native", "roarr"],
] as const;
const TRANSFERS = [
  ["hardlink", "Hardlink"],
  ["copy", "Copy"],
  ["move", "Move"],
] as const;

export function ImportTab({ s, locked, update }: Edit) {
  const im = s.import ?? {};
  const native = im.mode === "native";
  const [st] = usePoll(api.imports, 4000);
  const set = (f: (i: NonNullable<Settings["import"]>) => void) =>
    update((x) => {
      x.import ??= {};
      f(x.import);
    });

  return (
    <>
      <SectionHead title="Import" className="mb-3" />
      <Field label="Imported by" hint={native ? "Needs qBittorrent." : undefined}>
        <Segmented label="Imported by" value={native ? "native" : "arr"} options={MODES} disabled={locked.IMPORT_MODE} onChange={(v) => set((i) => (i.mode = v))} />
      </Field>
      {native && (
        <>
          <Field label="Library roots">
            <div className="grid gap-2 sm:grid-cols-[60px_minmax(0,1fr)] sm:items-center">
              <span className="meta">Shows</span>
              <LockedInput mono locked={locked.TV_ROOT} value={im.tvRoot ?? ""} placeholder="/media/tv" onChange={(v) => set((i) => (i.tvRoot = v))} />
              <span className="meta">Movies</span>
              <LockedInput mono locked={locked.MOVIES_ROOT} value={im.movieRoot ?? ""} placeholder="/media/movies" onChange={(v) => set((i) => (i.movieRoot = v))} />
            </div>
          </Field>
          <Field label="Transfer" hint={im.transfer === "move" ? "Stops seeding." : "Copies when a link is impossible."}>
            <Segmented label="Transfer" value={(im.transfer || "hardlink") as (typeof TRANSFERS)[number][0]} options={TRANSFERS} disabled={locked.IMPORT_TRANSFER} onChange={(v) => set((i) => (i.transfer = v))} />
          </Field>
          <Field label="Path map" hint="qBittorrent path = roarr path">
            <PathMap value={im.pathMap} locked={locked.QBIT_PATH_MAP} placeholder="/downloads=/data/downloads" onChange={(v) => set((i) => (i.pathMap = v))} />
          </Field>
          <Field label="qBittorrent">
            <div className="grid gap-2 sm:grid-cols-[60px_minmax(0,1fr)] sm:items-center">
              <span className="meta">Category</span>
              <LockedInput mono locked={locked.QBIT_CATEGORY} value={im.category ?? ""} placeholder="roarr" onChange={(v) => set((i) => (i.category = v))} className="max-w-60" />
              <span className="meta">Save to</span>
              <LockedInput mono locked={locked.QBIT_SAVE_PATH} value={im.savePath ?? ""} placeholder="client default" onChange={(v) => set((i) => (i.savePath = v))} />
            </div>
          </Field>
        </>
      )}
      {st && st.mode === "native" && <ImportJobs st={st} />}
    </>
  );
}

const jobTone = (j: NativeJob): Tone => (j.state === "failed" ? "bad" : j.state === "done" ? "have" : j.state === "waiting" ? "none" : "live");

function ImportJobs({ st }: { st: ImportsStatus }) {
  const jobs = st.jobs ?? [];
  return (
    <section className="mt-8">
      <SectionHead title="Recent imports" className="mb-2">
        <span className="font-mono text-[12px] text-muted-foreground tabular">{(st.files ?? []).length} {(st.files ?? []).length === 1 ? "file" : "files"} tracked</span>
      </SectionHead>
      {jobs.length === 0 && <p className="meta border-t py-4">None yet.</p>}
      {jobs.slice(0, 12).map((j) => (
        <div key={j.hash} className="grid grid-cols-[10px_minmax(0,1fr)_auto] items-center gap-x-3 border-t py-3">
          <Dot tone={jobTone(j)} pulse={j.state === "downloading" || j.state === "importing"} />
          <div className="min-w-0">
            <div className="truncate text-[14px]">{j.titleName}</div>
            <div className="truncate font-mono text-[11px] text-muted-foreground" title={j.release}>
              {j.release}
            </div>
            {j.message && <div className={cn("mt-1 text-[12px]", j.state === "failed" ? "text-bad" : "text-foreground/80")}>{j.message}</div>}
          </div>
          <span className={cn("font-mono text-[12px] tabular", j.state === "failed" ? "text-bad" : "text-muted-foreground")}>
            {j.state === "downloading" ? pct(j.progress) : j.state}
          </span>
        </div>
      ))}
    </section>
  );
}

// ---- Checks --------------------------------------------------------------------

const VMODES = [
  ["auto", "Auto"],
  ["on", "On"],
  ["off", "Off"],
] as const;

export function ChecksTab({ s, locked, update }: Edit) {
  const v = s.verify ?? { autoRegrab: false };
  const [st] = usePoll(api.verify, 5000);
  const tools = st?.tools;
  const set = (f: (x: NonNullable<Settings["verify"]>) => void) =>
    update((x) => {
      x.verify ??= { autoRegrab: false };
      f(x.verify);
    });

  return (
    <>
      <SectionHead title="Checks" className="mb-3">
        {st && <span className={cn("font-mono text-[12px]", st.enabled ? "text-have" : "text-muted-foreground")}>{st.enabled ? "running" : "off"}</span>}
      </SectionHead>
      {st && !tools?.ffprobe && verifyMode(v) !== "off" && (
        <Notice tone="miss" className="mb-3">
          ffprobe not found. Set <code className="font-mono text-[12px]">FFPROBE_PATH</code> or install ffmpeg.
        </Notice>
      )}
      <Field label="Check downloads" hint={verifyMode(v) === "auto" ? "On when ffprobe is found." : undefined}>
        <Segmented<VerifyMode> label="Check downloads" value={verifyMode(v)} options={VMODES} disabled={locked.VERIFY_MEDIA} onChange={(m) => set((x) => setVerifyMode(x, m))} />
        {tools && (
          <div className="mt-2 grid grid-cols-[60px_minmax(0,1fr)] gap-y-1 font-mono text-[12px]">
            <span className="text-muted-foreground">ffprobe</span>
            <span className={cn("truncate", !tools.ffprobe && "text-miss")}>{tools.ffprobe || "missing"}</span>
            <span className="text-muted-foreground">ffmpeg</span>
            <span className={cn("truncate", !tools.ffmpeg && "text-miss")}>{tools.ffmpeg || "missing, no frame check"}</span>
          </div>
        )}
      </Field>
      <Field label="Failed downloads">
        <Toggle checked={v.autoRegrab} disabled={locked.VERIFY_AUTO_REGRAB} onChange={(b) => set((x) => (x.autoRegrab = b))}>
          Re-grab automatically
        </Toggle>
      </Field>
      <Field label="Path map" hint="Client path = roarr path">
        <PathMap value={v.pathMap} locked={locked.VERIFY_PATH_MAP} placeholder="/downloads=/data/downloads" onChange={(p) => set((x) => (x.pathMap = p))} />
      </Field>
      {st && <CheckCounts st={st} />}
    </>
  );
}

function CheckCounts({ st }: { st: VerifyList }) {
  const n = { fail: 0, warn: 0, checking: 0, ok: 0, regrabFailed: 0 };
  for (const r of st.items ?? []) n[outcome(r)]++;
  const regrabbed = (st.items ?? []).filter((r) => r.rejected && !r.regrab?.error).length;
  return (
    <div className="mt-6 flex flex-wrap gap-x-6 gap-y-1 border-t pt-4">
      <Figure value={(st.items ?? []).length} label="followed" />
      <Figure value={n.checking} label="waiting" tone={n.checking ? "live" : undefined} />
      <Figure value={n.fail + n.regrabFailed} label="failed" tone={n.fail + n.regrabFailed ? "bad" : undefined} />
      <Figure value={n.warn} label="warned" tone={n.warn ? "miss" : undefined} />
      <Figure value={regrabbed} label="re-grabbed" />
    </div>
  );
}

// ---- AV1 -------------------------------------------------------------------------

export function TranscodeTab({ s, locked, update }: Edit) {
  const t = s.transcode;
  const [st, setSt] = usePoll(api.transcode, 3000);
  const set = (f: (x: Settings["transcode"]) => void) => update((x) => f(x.transcode));

  return (
    <>
      <SectionHead title="AV1" className="mb-3">
        {st && (
          <span className={cn("font-mono text-[12px]", st.available ? "text-muted-foreground" : "text-miss")}>
            {st.available ? st.encoder : (st.error ?? "no AV1 encoder")}
          </span>
        )}
      </SectionHead>
      <Field label="Convert imports">
        <Toggle checked={t.enabled} disabled={locked.TRANSCODE} onChange={(b) => set((x) => (x.enabled = b))}>
          Re-encode to AV1
        </Toggle>
      </Field>
      <Field label="Quality" hint="CRF 0 = per resolution. Preset 1 slowest, 13 fastest.">
        <div className="flex flex-wrap gap-x-6 gap-y-2">
          <Num label="CRF" value={t.crf} min={0} max={63} unit="CRF" disabled={locked.TRANSCODE_CRF} onChange={(n) => set((x) => (x.crf = n))} />
          <Num label="Preset" value={t.preset} min={1} max={13} unit="preset" disabled={locked.TRANSCODE_PRESET} onChange={(n) => set((x) => (x.preset = n))} />
          <Num label="Film grain" value={t.filmGrain} min={0} max={50} unit="grain" disabled={locked.TRANSCODE_FILM_GRAIN} onChange={(n) => set((x) => (x.filmGrain = n))} />
        </div>
      </Field>
      <Field label="Keep if smaller by">
        <Num value={t.minSavings} min={0} max={90} unit="%" disabled={locked.TRANSCODE_MIN_SAVINGS} onChange={(n) => set((x) => (x.minSavings = n))} />
      </Field>
      <Field label="Load" hint="Nice 0 to 19.">
        <div className="flex flex-wrap gap-x-6 gap-y-2">
          <Num label="Concurrency" value={t.concurrency} min={1} max={8} unit="at once" disabled={locked.TRANSCODE_CONCURRENCY} onChange={(n) => set((x) => (x.concurrency = n))} />
          <Num label="Nice" value={t.nice} min={0} max={19} unit="nice" disabled={locked.TRANSCODE_NICE} onChange={(n) => set((x) => (x.nice = n))} />
        </div>
      </Field>
      <Field label={st?.zone ? `Schedule (${st.zone})` : "Schedule"} hint="Convert only inside these hours, in server time. No windows = any time. A window that ends before it starts runs past midnight.">
        <Schedule value={t.schedule ?? []} onChange={(w) => set((x) => (x.schedule = w))} />
      </Field>
      <Field label="Path map" hint="Sonarr/Radarr path = roarr path">
        <PathMap value={t.pathMap} locked={locked.TRANSCODE_PATH_MAP} placeholder="/tv=/media/tv,/movies=/media/movies" onChange={(p) => set((x) => (x.pathMap = p))} />
      </Field>
      {st && <TranscodeQueue st={st} onChange={setSt} />}
    </>
  );
}

/** Day choices, as qBittorrent's speed schedule offers them. */
const DAY_SETS: [string, string, number[]][] = [
  ["every", "Every day", [1, 2, 3, 4, 5, 6, 0]],
  ["weekdays", "Weekdays", [1, 2, 3, 4, 5]],
  ["weekends", "Weekends", [6, 0]],
  ...WEEK.map(({ day, name }): [string, string, number[]] => [String(day), name, [day]]),
];
const sameDays = (a: number[], b: number[]) => a.length === b.length && a.every((d) => b.includes(d));

/** Half-hour steps; a time typed into settings.json elsewhere stays selectable. */
const TIMES = Array.from({ length: 48 }, (_, i) => `${pad(Math.floor(i / 2))}:${i % 2 ? "30" : "00"}`);
const END_TIMES = [...TIMES, "24:00"];

/**
 * Weekly windows, one line each: days, from, to. A window needs 424 px for one line; below that it takes two, and
 * the windows get more space between them so each still reads as one.
 */
function Schedule({ value, onChange }: { value: ScheduleWindow[]; onChange: (v: ScheduleWindow[]) => void }) {
  const edit = (i: number, w: ScheduleWindow) => onChange(value.map((x, k) => (k === i ? w : x)));
  return (
    <div className="@container">
    <div className="grid gap-4 @[26.5rem]:gap-2">
      {value.map((w, i) => {
        const { from, to } = w;
        return (
          <div key={i} className="flex flex-wrap items-center gap-2">
            <DaySelect value={w.days?.length ? w.days : DAY_SETS[0][2]} onChange={(days) => edit(i, { ...w, days })} />
            <div className="flex flex-wrap items-center gap-2">
              <TimeSelect label="From" value={from} times={TIMES} onChange={(v) => edit(i, { ...w, from: v })} />
              <span className="text-muted-foreground">–</span>
              <TimeSelect label="To" value={to} times={END_TIMES} onChange={(v) => edit(i, { ...w, to: v })} />
              <span className="w-5 text-[12px] text-muted-foreground" title={to <= from ? "Ends the next day" : undefined}>
                {to <= from ? "+1" : ""}
              </span>
              <Button variant="ghost" size="icon-sm" className="text-muted-foreground" aria-label="Remove window" onClick={() => onChange(value.filter((_, k) => k !== i))}>
                <X />
              </Button>
            </div>
          </div>
        );
      })}
      <div className="flex items-center gap-4">
        <Button variant="outline" onClick={() => onChange([...value, newWindow()])}>
          <Plus /> Add window
        </Button>
        {value.length === 0 && <span className="text-[13px] text-muted-foreground">Any time</span>}
      </div>
    </div>
    </div>
  );
}

function DaySelect({ value, onChange }: { value: number[]; onChange: (v: number[]) => void }) {
  const set = DAY_SETS.find(([, , days]) => sameDays(days, value));
  const custom = WEEK.filter((d) => value.includes(d.day))
    .map((d) => d.short)
    .join(" ");
  return (
    <Select value={set?.[0] ?? "custom"} onValueChange={(k) => onChange(DAY_SETS.find(([key]) => key === k)?.[2] ?? value)}>
      <SelectTrigger aria-label="Days" className="h-9 w-32 bg-card">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {!set && <SelectItem value="custom">{custom}</SelectItem>}
        {DAY_SETS.map(([k, name]) => (
          <SelectItem key={k} value={k}>
            {name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function TimeSelect({ label, value, times: base, onChange }: { label: string; value: string; times: string[]; onChange: (v: string) => void }) {
  const times = base.includes(value) ? base : [...base, value].sort();
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger aria-label={label} className="h-9 w-24 bg-card tabular">
        <SelectValue />
      </SelectTrigger>
      <SelectContent className="max-h-72">
        {times.map((t) => (
          <SelectItem key={t} value={t} className="tabular">
            {t}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

const ORDER: Record<string, number> = { running: 0, queued: 1, failed: 2, done: 3, skipped: 4 };

function TranscodeQueue({ st, onChange }: { st: TranscodeStatus; onChange: (st: TranscodeStatus) => void }) {
  const jobs = [...(st.jobs ?? [])].sort((a, b) => ORDER[a.state] - ORDER[b.state]);
  const c = st.counts;
  const [busy, setBusy] = useState(false);
  const hold = holdText(st);
  const toggle = () => {
    setBusy(true);
    api
      .transcodePause(!st.paused)
      .then(onChange)
      .catch(() => {})
      .finally(() => setBusy(false));
  };
  return (
    <section className="mt-8">
      <SectionHead title="Queue" className="mb-3">
        {hold && <State tone={st.held ? "miss" : "live"}>{hold}</State>}
        <Button variant="outline" size="sm" className="ml-auto" disabled={busy || !st.enabled} onClick={toggle}>
          {st.paused ? <Play /> : <Pause />}
          <SameWidth shown={st.paused ? "Resume" : "Pause"} other={st.paused ? "Pause" : "Resume"} />
        </Button>
      </SectionHead>
      <div className="mb-3 flex flex-wrap gap-x-6 gap-y-1">
        <Figure value={c.running} label="running" tone={c.running ? "live" : undefined} />
        <Figure value={c.queued} label="queued" />
        <Figure value={c.done} label="done" />
        <Figure value={c.skipped} label="kept original" />
        <Figure value={c.failed} label="failed" tone={c.failed ? "bad" : undefined} />
        <Figure value={bytes(st.bytesSaved) === "—" ? "0" : bytes(st.bytesSaved)} label="saved" tone={st.bytesSaved ? "have" : undefined} />
      </div>
      {jobs.length === 0 && <p className="meta border-t py-4">Nothing converted yet.</p>}
      {jobs.slice(0, 20).map((j) => (
        <TranscodeRow key={j.id} j={j} />
      ))}
    </section>
  );
}

function TranscodeRow({ j }: { j: TranscodeJob }) {
  const tone: Tone = j.state === "failed" ? "bad" : j.state === "done" ? "have" : isPending(j) ? "live" : "none";
  const share = savedShare(j);
  return (
    <div className="grid grid-cols-[10px_minmax(0,1fr)_auto] items-center gap-x-3 border-t py-3 md:grid-cols-[10px_minmax(0,1fr)_200px]">
      <Dot tone={tone} pulse={j.state === "running" && !j.paused} />
      <div className="min-w-0">
        <div className="truncate text-[14px]">{j.title || baseName(j.path)}</div>
        <div className="truncate font-mono text-[11px] text-muted-foreground" title={j.path}>
          {baseName(j.path)}
        </div>
        {j.message && <div className={cn("mt-1 text-[12px]", j.state === "failed" ? "text-bad" : "text-foreground/80")}>{j.message}</div>}
      </div>
      <div className="min-w-0 text-right font-mono text-[12px] tabular">
        {j.state === "running" ? (
          <>
            <div className="flex justify-between gap-3">
              <span className="text-muted-foreground">{j.paused ? "paused" : (j.speed ?? "")}</span>
              <span>{j.percent.toFixed(0)}%</span>
            </div>
            <Bar value={j.percent / 100} segments={32} stretch className="mt-2" />
          </>
        ) : j.state === "done" ? (
          <span className="text-have">
            {bytes(j.srcBytes ?? 0)} → {bytes(j.outBytes ?? 0)}
            {share > 0 && <span className="text-muted-foreground"> −{Math.round(share * 100)}%</span>}
          </span>
        ) : (
          <span className={j.state === "failed" ? "text-bad" : "text-muted-foreground"}>{j.state === "skipped" ? "kept original" : j.state}</span>
        )}
      </div>
    </div>
  );
}
