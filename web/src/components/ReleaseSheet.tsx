import { useMemo, useState } from "react";
import { Check, Download, ExternalLink, Star } from "lucide-react";
import type { GrabResult, Release } from "@/lib/api";
import { age, bytes, epCode } from "@/lib/format";
import { fitScore, type Grabber, type GrabOpts, type Kind, type LivePlan } from "@/lib/releases";
import type { SearchState } from "@/lib/useSearch";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "@/components/ui/sheet";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Label, Notice, Spinner } from "./kit";

type Sort = "best" | "seeders" | "size" | "newest";

export interface ReleaseSheetProps {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  title: string;
  /** Short mono line under the title, e.g. "S02E08 · S02E09". */
  detail?: string;
  kind: Kind;
  state: SearchState;
  plan: LivePlan;
  wanted: number[];
  grabber: Grabber;
  watchFirst?: boolean;
  epLabel?: (id: number) => string;
  movieLabel?: (tmdb: number) => string;
}

export function ReleaseSheet(p: ReleaseSheetProps) {
  const { state, plan, grabber } = p;
  const [sort, setSort] = useState<Sort>("best");
  const [res, setRes] = useState("any");
  const [shape, setShape] = useState<"all" | "packs" | "singles">("all");
  const [showRejected, setShowRejected] = useState(false);
  const [open, setOpen] = useState<string | null>(null);
  const [limit, setLimit] = useState(50);
  const opts: GrabOpts = { watchFirst: p.watchFirst };

  const resolutions = useMemo(() => {
    const s = new Set<number>();
    for (const r of plan.all) if (r.info.resolution && !r.rejected) s.add(r.info.resolution);
    return [...s].sort((a, b) => b - a);
  }, [plan.all]);

  const rejectedCount = plan.all.filter((r) => r.rejected).length;
  const list = useMemo(() => {
    let xs = plan.all.filter((r) => (showRejected || !r.rejected) && (res === "any" || r.info.resolution === +res));
    if (p.kind === "tv" && shape !== "all") xs = xs.filter((r) => ((r.covers?.length ?? 0) > 1) === (shape === "packs"));
    const want = new Set(p.wanted);
    const by: Record<Sort, (a: Release, b: Release) => number> = {
      best: (a, b) => Number(a.rejected) - Number(b.rejected) || fitScore(b, want, p.kind) - fitScore(a, want, p.kind) || b.seeders - a.seeders,
      seeders: (a, b) => b.seeders - a.seeders,
      size: (a, b) => b.size - a.size,
      newest: (a, b) => a.ageHours - b.ageHours,
    };
    return xs.sort(by[sort]);
  }, [plan.all, showRejected, res, shape, sort, p.kind, p.wanted]);

  const planKeys = new Set(plan.picks.map((x) => x.release.key));
  const indexers = [...state.indexers.values()].sort((a, b) => a.name.localeCompare(b.name));
  const live = indexers.filter((i) => !i.disabled);
  const answered = live.filter((i) => i.answered >= i.queries).length;
  const secs = (t?: number) => (t ? `${((t - state.startedAt) / 1000).toFixed(2)} s` : "");
  const allCached = indexers.length > 0 && indexers.every((i) => i.cached || i.disabled);
  const allGot = plan.picks.length > 0 && plan.picks.every((x) => grabber.results.get(x.release.key)?.ok);
  const coversAll = plan.missing.length === 0;

  return (
    <Sheet open={p.open} onOpenChange={p.onOpenChange}>
      <SheetContent className="gap-0 overflow-hidden p-0">
        <div className="px-6 pt-6 pb-4 sm:px-7">
          <Label mark>Choose a release</Label>
          <SheetTitle className="mt-3 pr-10 font-display text-[clamp(2rem,5vw,2.9rem)]">{p.title}</SheetTitle>
          <SheetDescription className="mt-2 font-mono text-xs text-muted-foreground">{p.detail ?? " "}</SheetDescription>
        </div>

        <div className="space-y-2.5 px-6 pb-4 sm:px-7">
          <div className="flex flex-wrap items-center gap-x-2 font-mono text-xs">
            {!state.done && <Spinner className="size-3" />}
            <span>
              {answered}/{live.length || "…"} indexers answered
            </span>
            <span className="text-muted-foreground">
              {state.firstAt ? `· first result ${secs(state.firstAt)}` : state.done ? "· no results" : "· searching…"}
              {state.done && state.doneAt ? ` · all ${secs(state.doneAt)}` : ""} · {plan.all.length} releases
              {allCached && state.done ? " · from cache" : ""}
            </span>
          </div>
          {indexers.length > 0 && (
            <div className="flex flex-wrap gap-1.5">
              {indexers.map((i) => {
                const done = i.answered >= i.queries;
                const failed = i.errors.length > 0 && i.errors.length >= i.answered && done;
                return (
                  <span key={i.id} title={i.disabled ?? (i.errors.join("\n") || undefined)} className="inline-flex h-[22px] items-center gap-1.5 rounded-md bg-muted px-2 font-mono text-[10.5px] text-foreground/80">
                    {i.disabled ? (
                      <i className="size-1.5 rounded-full bg-muted-foreground/50" />
                    ) : !done ? (
                      <Spinner className="size-2.5 border-[1.5px]" />
                    ) : (
                      <i className={cn("size-1.5 rounded-full", failed ? "bg-destructive" : i.errors.length ? "bg-amber-500" : "bg-ok")} />
                    )}
                    {i.name}
                    <span className="text-muted-foreground">{i.disabled ? "off" : !done ? "" : failed ? "failed" : i.cached ? `${i.results} · cache` : `${i.results} · ${(i.ms / 1000).toFixed(1)}s`}</span>
                  </span>
                );
              })}
            </div>
          )}
        </div>

        {state.error && <Notice tone="bad" className="mx-6 mb-3 sm:mx-7">{state.error}</Notice>}

        {plan.picks.length > 0 && (
          <div className="mx-6 rounded-2xl bg-lime px-4 py-3.5 text-ink sm:mx-7">
            <div className="flex flex-wrap items-center gap-3">
              <Label className="flex items-center gap-1.5 text-ink/65">
                <Star className="size-3 fill-current" /> Recommended · {coversAll ? `covers all ${p.wanted.length}` : `covers ${p.wanted.length - plan.missing.length} of ${p.wanted.length}`} · {bytes(plan.size)}
              </Label>
              <Button variant="default" size="sm" className="ml-auto" disabled={allGot || grabber.busy.size > 0} onClick={() => grabber.grabAll(plan.picks.map((x) => x.release), opts)}>
                {allGot ? <Check /> : grabber.busy.size > 0 ? <Spinner /> : <Download />}
                {allGot ? "Grabbed" : "Get recommended"}
              </Button>
            </div>
            <div className="mt-2">
              {plan.picks.slice(0, 5).map((x) => (
                <div key={x.release.key} className="flex gap-3 border-t border-ink/25 py-1.5 font-mono text-[11.5px] first:border-t-0">
                  <span className="shrink-0">{grabber.results.get(x.release.key)?.ok ? "✓" : coverLabel(x.release, x.fills, p)}</span>
                  <span className="min-w-0 flex-1 truncate" title={x.release.title}>
                    {x.release.title}
                  </span>
                  <span className="shrink-0">{bytes(x.release.size)}</span>
                </div>
              ))}
              {plan.picks.length > 5 && <div className="pt-1 font-mono text-[11px] text-ink/60">+ {plan.picks.length - 5} more</div>}
              {plan.missing.length > 0 && state.done && (
                <div className="pt-1 text-[12.5px]">
                  Nothing suitable for {plan.missing.length <= 6 && p.epLabel ? plan.missing.map(p.epLabel).join(", ") : `${plan.missing.length} ${p.kind === "tv" ? "episodes" : "movies"}`}.
                </div>
              )}
            </div>
          </div>
        )}

        <div className="flex flex-wrap items-center gap-2 px-6 pt-4 pb-3 sm:px-7">
          {resolutions.length > 1 && (
            <Tabs value={res} onValueChange={setRes}>
              <TabsList className="h-9!">
                <TabsTrigger value="any" className="text-[13px]">
                  Any
                </TabsTrigger>
                {resolutions.map((r) => (
                  <TabsTrigger key={r} value={String(r)} className="text-[13px]">
                    {r === 2160 ? "4K" : `${r}p`}
                  </TabsTrigger>
                ))}
              </TabsList>
            </Tabs>
          )}
          {p.kind === "tv" && (
            <Tabs value={shape} onValueChange={(v) => setShape(v as typeof shape)}>
              <TabsList className="h-9!">
                <TabsTrigger value="all" className="text-[13px]">
                  All
                </TabsTrigger>
                <TabsTrigger value="packs" className="text-[13px]">
                  Packs
                </TabsTrigger>
                <TabsTrigger value="singles" className="text-[13px]">
                  Single episodes
                </TabsTrigger>
              </TabsList>
            </Tabs>
          )}
          <Select value={sort} onValueChange={(v) => setSort(v as Sort)}>
            <SelectTrigger size="sm" className="ml-auto rounded-full px-3.5" aria-label="Sort releases">
              <SelectValue />
            </SelectTrigger>
            <SelectContent align="end">
              <SelectItem value="best">Best match</SelectItem>
              <SelectItem value="seeders">Most seeders</SelectItem>
              <SelectItem value="size">Largest</SelectItem>
              <SelectItem value="newest">Newest</SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto px-6 pb-4 sm:px-7">
          {list.length === 0 && (
            <div className="py-10 text-center text-sm text-muted-foreground">{state.done ? "No releases for this selection." : "Waiting for indexers…"}</div>
          )}
          {list.slice(0, limit).map((r) => (
            <ReleaseRow
              key={r.key}
              r={r}
              p={p}
              picked={planKeys.has(r.key)}
              open={open === r.key}
              onToggle={() => setOpen(open === r.key ? null : r.key)}
              busy={grabber.busy.has(r.key)}
              result={grabber.results.get(r.key)}
              onGrab={(o) => grabber.grab(r, { ...opts, ...o })}
            />
          ))}
          {list.length > limit && (
            <div className="py-3 text-center">
              <Button variant="outline" size="sm" onClick={() => setLimit(limit + 100)}>
                Show {Math.min(100, list.length - limit)} more
              </Button>
            </div>
          )}
        </div>

        {rejectedCount > 0 && (
          <div className="border-t border-border px-6 py-3.5 font-mono text-[11.5px] text-muted-foreground sm:px-7">
            {rejectedCount} release{rejectedCount > 1 ? "s" : ""} {showRejected ? "shown despite" : "hidden by"} your quality rules ·{" "}
            <button className="cursor-pointer underline decoration-dash underline-offset-4 hover:text-foreground" onClick={() => setShowRejected(!showRejected)}>
              {showRejected ? "hide them" : "show them"}
            </button>
          </div>
        )}
      </SheetContent>
    </Sheet>
  );
}

function coverLabel(r: Release, fills: number[], p: ReleaseSheetProps): string {
  if (p.kind === "movie") return p.movieLabel && p.wanted.length > 1 ? p.movieLabel(fills[0]) : "Movie";
  const n = r.covers?.length ?? 0;
  if (r.kind === "complete") return `All · ${n}`;
  if (r.kind === "multi-season") return `S${r.info.seasons?.[0]}–${r.info.seasons?.[r.info.seasons.length - 1]}`;
  if (r.kind === "season") return `S${String(r.info.seasons?.[0] ?? "").padStart(2, "0")} pack`;
  if (fills.length === 1 && p.epLabel) return p.epLabel(fills[0]).replace(/^S\d+/, "");
  return `${fills.length} eps`;
}

function kindBadge(r: Release) {
  const n = r.covers?.length ?? 0;
  switch (r.kind) {
    case "complete":
      return <Badge variant="ice">Complete{n ? ` · ${n} eps` : ""}</Badge>;
    case "multi-season":
      return (
        <Badge variant="ice">
          S{r.info.seasons?.[0]}–S{r.info.seasons?.[r.info.seasons.length - 1]}
          {n ? ` · ${n} eps` : ""}
        </Badge>
      );
    case "season":
      return (
        <Badge variant="ice">
          Season {r.info.seasons?.[0]}
          {n ? ` · ${n} eps` : ""}
        </Badge>
      );
    case "multi":
      return <Badge variant="ice">{n || r.info.episodes?.length} eps</Badge>;
    case "episode":
      if (r.info.seasons?.length && r.info.episodes?.length) return <Badge>{epCode(r.info.seasons[0], r.info.episodes[0])}</Badge>;
      if (r.info.absolute?.length) return <Badge>#{r.info.absolute[0]}</Badge>;
  }
  return null;
}

function ReleaseRow({
  r,
  p,
  picked,
  open,
  onToggle,
  busy,
  result,
  onGrab,
}: {
  r: Release;
  p: ReleaseSheetProps;
  picked: boolean;
  open: boolean;
  onToggle: () => void;
  busy: boolean;
  result?: GrabResult;
  onGrab: (o: GrabOpts) => void;
}) {
  const i = r.info;
  const wanted = new Set(p.wanted);
  const fills = (r.covers ?? []).filter((id) => wanted.has(id)).length;
  const extras = (r.covers?.length ?? 0) - fills;
  const tags = [
    i.resolution ? (i.resolution === 2160 ? "4K" : `${i.resolution}p`) : null,
    i.source,
    i.codec,
    i.hdr,
    i.audio ? `${i.audio}${i.channels ? ` ${i.channels}` : ""}` : null,
    i.dualAudio ? "dual audio" : i.multi ? "multi" : null,
    ...(i.languages ?? []),
    i.repack ? "repack" : i.proper ? "proper" : null,
    i.group,
  ].filter(Boolean) as string[];

  return (
    <div className={cn("border-t border-border py-3", r.rejected && "opacity-55")}>
      <div className="grid grid-cols-[38px_minmax(0,1fr)_auto_auto] items-center gap-3 sm:gap-3.5">
        <span
          title={(r.notes ?? []).join("\n") || undefined}
          className={cn("grid size-[38px] place-items-center rounded-[10px] font-mono text-sm font-semibold", picked ? "bg-lime text-ink" : "bg-muted")}
        >
          {Math.round(r.score)}
        </span>
        <button onClick={onToggle} className="min-w-0 cursor-pointer text-left outline-none focus-visible:underline" aria-expanded={open}>
          <div className="font-mono text-[12.5px] leading-snug break-all">{r.title}</div>
          <div className="mt-1.5 flex flex-wrap items-center gap-1">
            {p.kind === "tv" && kindBadge(r)}
            {p.kind === "movie" && p.movieLabel && r.movieTmdb && p.wanted.length > 1 && <Badge variant="ice">{p.movieLabel(r.movieTmdb)}</Badge>}
            {tags.map((t) => (
              <Badge key={t} className={cn("h-5 px-2 text-[10px]", t === i.group && "normal-case")}>
                {t}
              </Badge>
            ))}
            <span className="font-mono text-[10.5px] text-muted-foreground">{r.indexers.length > 1 ? `${r.indexers.length} indexers` : r.indexer}</span>
            {p.kind === "tv" && extras > 0 && <span className="font-mono text-[10.5px] text-muted-foreground">+{extras} not needed</span>}
          </div>
          {r.rejected && r.reasons && <div className="mt-1 text-xs text-destructive">{r.reasons.join(" · ")}</div>}
        </button>
        <div className="flex flex-col items-end font-mono text-xs leading-tight tabular">
          <span>{bytes(r.size)}</span>
          <span className={cn(r.seeders >= 20 ? "text-ok" : r.seeders < 5 && "text-destructive")}>▲ {r.seeders}</span>
          <span className="text-muted-foreground">{age(r.ageHours)}</span>
        </div>
        {result?.ok ? (
          <span className="grid size-8 place-items-center rounded-full bg-lime text-ink" aria-label="Grabbed">
            <Check className="size-4" />
          </span>
        ) : (
          <Button variant={picked ? "default" : "outline"} size="icon-sm" disabled={busy} onClick={() => onGrab({})} aria-label={`Grab ${r.title}`}>
            {busy ? <Spinner /> : <Download />}
          </Button>
        )}
      </div>
      {(open || (result && !result.ok)) && !result?.ok && (
        <div className="mt-3 ml-[50px] flex flex-wrap items-center gap-2 rounded-xl bg-muted px-3.5 py-2.5 text-[13px]">
          <span className="mr-auto">
            {p.kind === "tv" ? (
              <>
                Fills <b>{fills}</b> wanted episode{fills === 1 ? "" : "s"}
                {extras > 0 && <span className="text-muted-foreground"> (+{extras} you have or didn't pick)</span>}
              </>
            ) : (
              "Movie release"
            )}{" "}
            · {r.seeders} seeders
          </span>
          {r.infoUrl && (
            <Button asChild variant="ghost" size="sm">
              <a href={r.infoUrl} target="_blank" rel="noreferrer noopener">
                <ExternalLink /> Details
              </a>
            </Button>
          )}
          {result && !result.ok && result.canForce && (
            <Button variant="destructive" size="sm" disabled={busy} onClick={() => onGrab({ force: true })}>
              Download anyway
            </Button>
          )}
          {result && !result.ok && (
            <div className="basis-full text-xs text-destructive">
              {result.message}
              {result.rejections?.length ? `: ${result.rejections.join(" · ")}` : ""}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
