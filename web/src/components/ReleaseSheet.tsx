import { Fragment, useMemo, useState, type ReactNode } from "react";
import { ArrowDown, ArrowUp, Check, Download, ExternalLink } from "lucide-react";
import type { GrabResult, Release } from "@/lib/api";
import { age, bytes, epCode } from "@/lib/format";
import { codecName, langCode, langsOf, resName } from "@/lib/langs";
import { fitScore, type Grabber, type GrabOpts, type Kind, type LivePlan } from "@/lib/releases";
import type { SearchState } from "@/lib/useSearch";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "@/components/ui/sheet";
import { Dot, Notice, Spec, Spinner } from "./kit";

type SortKey = "best" | "score" | "res" | "size" | "seeders" | "age";
type AudioFilter = "any" | "en" | "multi" | `lang:${string}`;
type SubsFilter = "any" | "en" | "tagged" | "none";
type Shape = "all" | "packs" | "singles";

export interface ReleaseSheetProps {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  title: string;
  /** Short line under the title: "S02E08, S02E09". */
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

/**
 * Every release the indexers returned for the current selection, as a
 * comparison table: what you would get (resolution, source, video, audio,
 * languages, subtitles), what it costs (size, seeders) and why roarr did or
 * did not pick it. Filters narrow, headers sort, a row expands for detail.
 */
export function ReleaseSheet(p: ReleaseSheetProps) {
  const { state, plan, grabber } = p;
  const [sort, setSort] = useState<{ key: SortKey; dir: 1 | -1 }>({ key: "best", dir: -1 });
  const [res, setRes] = useState<Set<number>>(new Set());
  const [audio, setAudio] = useState<AudioFilter>("any");
  const [subs, setSubs] = useState<SubsFilter>("any");
  const [shape, setShape] = useState<Shape>("all");
  const [showRejected, setShowRejected] = useState(false);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [limit, setLimit] = useState(60);
  const opts: GrabOpts = { watchFirst: p.watchFirst };
  const want = useMemo(() => new Set(p.wanted), [p.wanted]);

  // Filter options come from what is actually on offer.
  const facets = useMemo(() => {
    const r = new Set<number>();
    const langs = new Set<string>();
    for (const x of plan.all) {
      if (x.info.resolution) r.add(x.info.resolution);
      for (const l of x.info.languages ?? []) langs.add(l);
    }
    return { res: [...r].sort((a, b) => b - a), langs: [...langs].sort() };
  }, [plan.all]);

  const rejectedCount = plan.all.filter((r) => r.rejected).length;
  const list = useMemo(() => {
    const xs = plan.all.filter((r) => {
      if (!showRejected && r.rejected) return false;
      if (res.size && !res.has(r.info.resolution ?? 0)) return false;
      const l = langsOf(r.info);
      if (audio === "en" && l.english === "no") return false;
      if (audio === "multi" && !l.audio.includes("Multi")) return false;
      if (audio.startsWith("lang:") && !l.audio.includes(audio.slice(5))) return false;
      if (subs === "en" && !l.subs.includes("English") && !l.subs.includes("Multi")) return false;
      if (subs === "tagged" && !l.subbed) return false;
      if (subs === "none" && l.subbed) return false;
      if (p.kind === "tv" && shape !== "all" && ((r.covers?.length ?? 0) > 1) !== (shape === "packs")) return false;
      return true;
    });
    const by: Record<SortKey, (a: Release, b: Release) => number> = {
      best: (a, b) => fitScore(a, want, p.kind) - fitScore(b, want, p.kind) || a.seeders - b.seeders,
      score: (a, b) => a.score - b.score,
      res: (a, b) => (a.info.resolution ?? 0) - (b.info.resolution ?? 0),
      size: (a, b) => a.size - b.size,
      seeders: (a, b) => a.seeders - b.seeders,
      age: (a, b) => b.ageHours - a.ageHours,
    };
    const f = by[sort.key];
    return xs.sort((a, b) => Number(a.rejected) - Number(b.rejected) || sort.dir * f(a, b));
  }, [plan.all, showRejected, res, audio, subs, shape, sort, p.kind, want]);

  const picks = useMemo(() => new Map(plan.picks.map((x) => [x.release.key, x.fills])), [plan.picks]);
  const bestScore = plan.picks.length ? Math.max(...plan.picks.map((x) => x.release.score)) : 0;
  const indexers = [...state.indexers.values()];
  const live = indexers.filter((i) => !i.disabled);
  const answered = live.filter((i) => i.answered >= i.queries).length;
  const failed = live.filter((i) => i.answered >= i.queries && i.errors.length > 0 && i.errors.length >= i.answered);
  const allGot = plan.picks.length > 0 && plan.picks.every((x) => grabber.results.get(x.release.key)?.ok);

  const toggleSort = (key: SortKey) =>
    setSort((s) => (s.key === key ? { key, dir: s.dir === 1 ? -1 : 1 } : { key, dir: key === "age" ? 1 : -1 }));

  return (
    <Sheet open={p.open} onOpenChange={p.onOpenChange}>
      <SheetContent className="w-full gap-0 overflow-hidden p-0 sm:max-w-[1180px]">
        <div className="flex flex-wrap items-end justify-between gap-x-6 gap-y-2 border-b px-5 pt-5 pb-3.5 sm:px-6">
          <div className="min-w-0 pr-8">
            <SheetTitle className="truncate text-[20px] leading-tight font-medium">{p.title}</SheetTitle>
            <SheetDescription className="mt-1 truncate font-mono text-[12px]">{p.detail ?? " "}</SheetDescription>
          </div>
          <div className="flex items-center gap-2 font-mono text-[11.5px] text-muted-foreground tabular">
            {!state.done && <Spinner className="size-3" />}
            <span>
              {answered}/{live.length || "…"} indexers
            </span>
            <span>{plan.all.length} releases</span>
            {failed.length > 0 && (
              <span className="text-bad" title={failed.map((i) => `${i.name}: ${i.errors[0]}`).join("\n")}>
                {failed.length} failed
              </span>
            )}
          </div>
        </div>

        {state.error && (
          <Notice tone="bad" className="mx-5 mt-3 sm:mx-6">
            {state.error}
          </Notice>
        )}

        {p.wanted.length > 0 && (
          <div className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b bg-card px-5 py-2.5 text-[13px] sm:px-6">
            {plan.picks.length > 0 ? (
              <>
                <span className="inline-flex items-center gap-2">
                  <Dot tone="have" />
                  <b className="font-medium">Best pick</b>
                  <span className="text-muted-foreground">
                    {plan.picks.length === 1 ? "1 release" : `${plan.picks.length} releases`}, {bytes(plan.size)}
                    {plan.missing.length > 0 && `, ${plan.missing.length} of ${p.wanted.length} not covered`}
                  </span>
                </span>
                <Button size="sm" className="ml-auto" disabled={allGot || grabber.busy.size > 0} onClick={() => grabber.grabAll(plan.picks.map((x) => x.release), opts)}>
                  {allGot ? <Check /> : grabber.busy.size > 0 ? <Spinner /> : <Download />}
                  {allGot ? "Sent" : "Get best pick"}
                </Button>
              </>
            ) : (
              <span className="inline-flex items-center gap-2 text-muted-foreground">
                <Dot tone={state.done ? "miss" : "live"} pulse={!state.done} />
                {state.done ? "Nothing passes your rules. Pick one below, or show rejected." : "Searching…"}
              </span>
            )}
          </div>
        )}

        <div className="flex flex-wrap items-center gap-x-1 gap-y-1.5 px-5 py-2.5 sm:px-6">
          {facets.res.length > 1 && (
            <Group label="Res">
              {facets.res.map((r) => (
                <Chip
                  key={r}
                  on={res.has(r)}
                  onClick={() =>
                    setRes((s) => {
                      const n = new Set(s);
                      if (n.has(r)) n.delete(r);
                      else n.add(r);
                      return n;
                    })
                  }
                >
                  {resName(r)}
                </Chip>
              ))}
            </Group>
          )}
          <Group label="Audio">
            <Chip on={audio === "any"} onClick={() => setAudio("any")}>
              Any
            </Chip>
            <Chip on={audio === "en"} onClick={() => setAudio("en")}>
              EN
            </Chip>
            <Chip on={audio === "multi"} onClick={() => setAudio("multi")}>
              Multi
            </Chip>
            {facets.langs
              .filter((l) => l !== "English")
              .map((l) => (
                <Chip key={l} on={audio === `lang:${l}`} onClick={() => setAudio(`lang:${l}`)}>
                  {langCode(l)}
                </Chip>
              ))}
          </Group>
          <Group label="Subs">
            <Chip on={subs === "any"} onClick={() => setSubs("any")}>
              Any
            </Chip>
            <Chip on={subs === "en"} onClick={() => setSubs("en")}>
              EN
            </Chip>
            <Chip on={subs === "tagged"} onClick={() => setSubs("tagged")}>
              Tagged
            </Chip>
            <Chip on={subs === "none"} onClick={() => setSubs("none")}>
              None
            </Chip>
          </Group>
          {p.kind === "tv" && (
            <Group label="Shape">
              <Chip on={shape === "all"} onClick={() => setShape("all")}>
                All
              </Chip>
              <Chip on={shape === "packs"} onClick={() => setShape("packs")}>
                Packs
              </Chip>
              <Chip on={shape === "singles"} onClick={() => setShape("singles")}>
                Singles
              </Chip>
            </Group>
          )}
          {rejectedCount > 0 && (
            <label className="ml-auto inline-flex h-7 cursor-pointer items-center gap-2 text-[12.5px] text-muted-foreground select-none">
              <input type="checkbox" className="accent-foreground" checked={showRejected} onChange={(e) => setShowRejected(e.target.checked)} />
              Show {rejectedCount} rejected
            </label>
          )}
        </div>

        <div className="min-h-0 flex-1 overflow-auto px-5 pb-4 sm:px-6">
          <div className="hidden grid-cols-[44px_minmax(220px,1fr)_58px_66px_92px_92px_90px_90px_70px_56px_36px] items-center gap-x-2.5 border-b pb-1.5 md:grid">
            <Th sort={sort} k="score" onClick={toggleSort} title="Score">
              Score
            </Th>
            <Th sort={sort} k="best" onClick={toggleSort}>
              Release
            </Th>
            <Th sort={sort} k="res" onClick={toggleSort}>
              Res
            </Th>
            <Th>Source</Th>
            <Th>Video</Th>
            <Th>Audio</Th>
            <Th>Lang</Th>
            <Th>Subs</Th>
            <Th sort={sort} k="size" onClick={toggleSort} right>
              Size
            </Th>
            <Th sort={sort} k="seeders" onClick={toggleSort} right>
              Seeds
            </Th>
            <span />
          </div>

          {list.length === 0 && <div className="py-12 text-center text-[13px] text-muted-foreground">{state.done ? "No releases match." : "Waiting for indexers…"}</div>}

          {list.slice(0, limit).map((r) => (
            <Row
              key={r.key}
              r={r}
              p={p}
              fills={picks.get(r.key)}
              bestScore={bestScore}
              open={expanded === r.key}
              onToggle={() => setExpanded(expanded === r.key ? null : r.key)}
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
      </SheetContent>
    </Sheet>
  );
}

function Group({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="mr-3 inline-flex items-center gap-1">
      <span className="meta mr-0.5">{label}</span>
      {children}
    </div>
  );
}

function Chip({ on, onClick, children }: { on: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      aria-pressed={on}
      onClick={onClick}
      className={cn(
        "h-7 cursor-pointer rounded-md border border-transparent px-2 font-mono text-[11.5px] text-muted-foreground outline-none hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50",
        on && "border-border bg-card text-foreground",
      )}
    >
      {children}
    </button>
  );
}

function Th({
  children,
  sort,
  k,
  onClick,
  right,
  title,
}: {
  children: ReactNode;
  sort?: { key: SortKey; dir: 1 | -1 };
  k?: SortKey;
  onClick?: (k: SortKey) => void;
  right?: boolean;
  title?: string;
}) {
  const active = sort && k && sort.key === k;
  if (!k || !onClick) return <span className={cn("meta", right && "text-right")}>{children}</span>;
  return (
    <button
      type="button"
      title={title}
      onClick={() => onClick(k)}
      className={cn("meta inline-flex cursor-pointer items-center gap-0.5 outline-none hover:text-foreground focus-visible:underline", right && "justify-end", active && "text-foreground")}
    >
      {children}
      {active && (sort.dir === -1 ? <ArrowDown className="size-3" /> : <ArrowUp className="size-3" />)}
    </button>
  );
}

/** "S01 pack", "S01–S03", "E05", "3 eps", or the movie title in a collection. */
function coverLabel(r: Release, p: ReleaseSheetProps): string {
  if (p.kind === "movie") return p.movieLabel && r.movieTmdb && p.wanted.length > 1 ? p.movieLabel(r.movieTmdb) : "";
  const n = r.covers?.length ?? 0;
  const i = r.info;
  switch (r.kind) {
    case "complete":
      return `All${n ? ` ${n} eps` : ""}`;
    case "multi-season":
      return `S${String(i.seasons?.[0]).padStart(2, "0")}–S${String(i.seasons?.[i.seasons.length - 1]).padStart(2, "0")}${n ? ` ${n} eps` : ""}`;
    case "season":
      return `S${String(i.seasons?.[0] ?? "").padStart(2, "0")} pack${n ? ` ${n} eps` : ""}`;
    case "multi":
      return `${n || i.episodes?.length} eps`;
    default:
      if (i.seasons?.length && i.episodes?.length) return epCode(i.seasons[0], i.episodes[0]);
      if (i.absolute?.length) return `#${i.absolute[0]}`;
      return "";
  }
}

function LangSpecs({ r }: { r: Release }) {
  const l = langsOf(r.info);
  if (!l.audioTagged) return <Spec dim title="No language in the name; usually the original audio">EN?</Spec>;
  return (
    <>
      {l.audio.map((a) => (
        <Spec key={a} tone={a === "English" || a === "Multi" ? "have" : undefined} title={a}>
          {langCode(a)}
        </Spec>
      ))}
    </>
  );
}

function SubSpecs({ r }: { r: Release }) {
  const l = langsOf(r.info);
  if (l.hardsub)
    return (
      <Spec tone="bad" title="Burnt-in subtitles">
        hard
      </Spec>
    );
  if (l.subs.length)
    return (
      <>
        {l.subs.map((s) => (
          <Spec key={s} tone={s === "English" || s === "Multi" ? "have" : undefined} title={s}>
            {langCode(s)}
          </Spec>
        ))}
      </>
    );
  if (l.subbed) return <Spec title="Subtitles mentioned, language unknown">subs</Spec>;
  return <span className="meta">–</span>;
}

function Row({
  r,
  p,
  fills,
  bestScore,
  open,
  onToggle,
  busy,
  result,
  onGrab,
}: {
  r: Release;
  p: ReleaseSheetProps;
  fills?: number[];
  bestScore: number;
  open: boolean;
  onToggle: () => void;
  busy: boolean;
  result?: GrabResult;
  onGrab: (o: GrabOpts) => void;
}) {
  const i = r.info;
  const picked = !!fills;
  const want = new Set(p.wanted);
  const nFills = (r.covers ?? []).filter((id) => want.has(id)).length;
  const extras = (r.covers?.length ?? 0) - nFills;
  const cover = coverLabel(r, p);
  const video = [codecName(i.codec), i.hdr, i.tenBit && "10bit"].filter(Boolean) as string[];
  const audioBits = [i.audio, i.channels].filter(Boolean).join(" ");
  const sent = result?.ok;
  const failed = result && !result.ok;
  const showDetail = open || failed;

  const why = (
    <>
      {r.rejected ? (
        <span className="text-bad">Rejected: {(r.reasons ?? []).join(", ")}</span>
      ) : picked ? (
        <span className="text-have">Best pick{p.kind === "tv" && fills ? `, fills ${fills.length} of ${p.wanted.length}` : ""}</span>
      ) : bestScore ? (
        <span className="text-muted-foreground">
          Not picked: {Math.round(r.score)} vs {Math.round(bestScore)}
          {p.kind === "tv" && extras > 0 ? `, +${extras} episodes you did not ask for` : ""}
        </span>
      ) : null}
    </>
  );

  const action = sent ? (
    <span className="grid size-8 place-items-center rounded-md bg-have-soft text-have" aria-label="Sent to download client">
      <Check className="size-4" />
    </span>
  ) : (
    <Button variant={picked ? "default" : "outline"} size="icon-sm" disabled={busy} onClick={() => onGrab({})} aria-label={`Get ${r.title}`} title="Get this release">
      {busy ? <Spinner /> : <Download />}
    </Button>
  );

  const scoreCell = (
    <span
      className={cn(
        "grid h-7 w-[38px] place-items-center rounded-md font-mono text-[12.5px] font-medium tabular",
        picked ? "bg-have text-background" : r.rejected ? "bg-bad-soft text-bad" : "bg-muted",
      )}
      title={picked ? "Best pick" : r.rejected ? "Rejected" : "Score"}
    >
      {r.rejected ? "–" : Math.round(r.score)}
    </span>
  );

  return (
    <div className={cn("border-b", r.rejected && "opacity-70")}>
      {/* Desktop: one grid row. */}
      <div
        className={cn("hidden min-h-[52px] cursor-pointer grid-cols-[44px_minmax(220px,1fr)_58px_66px_92px_92px_90px_90px_70px_56px_36px] items-center gap-x-2.5 py-1.5 outline-none hover:bg-card md:grid", open && "bg-card")}
        role="button"
        tabIndex={0}
        aria-expanded={open}
        onClick={onToggle}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            onToggle();
          }
        }}
      >
        {scoreCell}
        <span className="min-w-0">
          <span className="block truncate font-mono text-[12px] leading-snug" title={r.title}>
            {r.title}
          </span>
          <span className="mt-0.5 flex items-center gap-x-2 text-[11.5px] text-muted-foreground">
            {cover && <span className="font-mono text-foreground/80">{cover}</span>}
            {i.group && <span className="font-mono">{i.group}</span>}
            <span className="truncate">{r.indexers.length > 1 ? `${r.indexers.length} indexers` : r.indexer}</span>
            <span className="tabular">{age(r.ageHours)}</span>
            {(i.repack || i.proper) && <span>{i.repack ? "repack" : "proper"}</span>}
          </span>
        </span>
        <span className="font-mono text-[12px] tabular">{resName(i.resolution) || <span className="text-muted-foreground">?</span>}</span>
        <span className="truncate font-mono text-[12px]">{i.source || <span className="text-muted-foreground">?</span>}</span>
        <span className="flex flex-wrap gap-1">
          {video.map((v) => (
            <Spec key={v} tone={v === "DV" || v.startsWith("HDR") ? "live" : undefined}>
              {v}
            </Spec>
          ))}
        </span>
        <span className="truncate font-mono text-[12px]">{audioBits || <span className="text-muted-foreground">?</span>}</span>
        <span className="flex flex-wrap gap-1">
          <LangSpecs r={r} />
        </span>
        <span className="flex flex-wrap gap-1">
          <SubSpecs r={r} />
        </span>
        <span className="text-right font-mono text-[12px] tabular">{bytes(r.size)}</span>
        <span className={cn("text-right font-mono text-[12px] tabular", r.seeders === 0 ? "text-bad" : r.seeders < 5 ? "text-miss" : r.seeders >= 20 && "text-have")}>{r.seeders}</span>
        <span onClick={(e) => e.stopPropagation()}>{action}</span>
      </div>

      {/* Phone: stacked card. */}
      <div className="flex items-start gap-3 py-2.5 md:hidden">
        <button className="flex min-w-0 flex-1 cursor-pointer items-start gap-3 text-left outline-none focus-visible:underline" aria-expanded={open} onClick={onToggle}>
          {scoreCell}
          <span className="min-w-0 flex-1">
            <span className="block font-mono text-[12px] leading-snug break-all">{r.title}</span>
            <span className="mt-1.5 flex flex-wrap gap-1">
              {cover && <Spec>{cover}</Spec>}
              {i.resolution && <Spec>{resName(i.resolution)}</Spec>}
              {i.source && <Spec>{i.source}</Spec>}
              {video.map((v) => (
                <Spec key={v}>{v}</Spec>
              ))}
              {audioBits && <Spec>{audioBits}</Spec>}
              <LangSpecs r={r} />
              <SubSpecs r={r} />
            </span>
            <span className="mt-1.5 flex flex-wrap gap-x-3 font-mono text-[11.5px] text-muted-foreground tabular">
              <span>{bytes(r.size)}</span>
              <span className={cn(r.seeders === 0 && "text-bad")}>{r.seeders} seeds</span>
              <span>{age(r.ageHours)}</span>
              {i.group && <span>{i.group}</span>}
            </span>
          </span>
        </button>
        {action}
      </div>

      {showDetail && (
        <div className="mb-2 ml-0 flex flex-wrap items-center gap-x-4 gap-y-2 rounded-md bg-muted/70 px-3.5 py-2.5 text-[12.5px] md:ml-[54px]">
          <span className="mr-auto">
            {why}
            {p.kind === "tv" && !picked && !r.rejected && nFills > 0 && <span className="text-muted-foreground">, fills {nFills}</span>}
          </span>
          {r.notes && r.notes.length > 0 && (
            <span className="flex basis-full flex-wrap gap-1">
              {r.notes.map((n) => (
                <Spec key={n} dim>
                  {n}
                </Spec>
              ))}
            </span>
          )}
          {r.infoUrl && (
            <Button asChild variant="ghost" size="sm">
              <a href={r.infoUrl} target="_blank" rel="noreferrer noopener">
                <ExternalLink /> Indexer page
              </a>
            </Button>
          )}
          {failed && (
            <Fragment>
              <span className="basis-full text-bad">
                {result.message}
                {result.rejections?.length ? `: ${result.rejections.join(", ")}` : ""}
              </span>
              {result.canForce && (
                <Button variant="destructive" size="sm" disabled={busy} onClick={() => onGrab({ force: true })}>
                  Download anyway
                </Button>
              )}
            </Fragment>
          )}
        </div>
      )}
    </div>
  );
}
