import { Fragment, useEffect, useMemo, useState, type ReactNode } from "react";
import { ArrowDown, ArrowUp, Check, ExternalLink } from "lucide-react";
import type { GrabResult, Release } from "@/lib/api";
import { age, bytes, epCode } from "@/lib/format";
import { codecName, langCode, langsOf, resName } from "@/lib/langs";
import { fitScore, type Grabber, type GrabOpts, type Kind, type LivePlan } from "@/lib/releases";
import type { SearchState } from "@/lib/useSearch";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "@/components/ui/sheet";
import { Notice, Spec, Spinner, type Tone } from "./kit";

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
  /** The seasons the selection touches, as cells: each release lights the ones it covers. */
  cells?: { id: number; tone: Tone }[];
  /** Column label for the cells: "S02". */
  cellsLabel?: string;
}

const fillOf: Record<Tone, string> = { have: "bg-have", miss: "bg-miss", live: "bg-live", bad: "bg-bad", none: "bg-unlit" };

/** The season's cells, unlit where this release does not cover them. */
function Covers({ r, cells }: { r: Release; cells: { id: number; tone: Tone }[] }) {
  const covers = new Set(r.covers ?? []);
  return (
    <span aria-hidden className="flex flex-wrap gap-1">
      {cells.map((c) => (
        <i key={c.id} className={cn("block size-2 rounded-[1px]", covers.has(c.id) ? fillOf[c.tone] : "bg-unlit")} />
      ))}
    </span>
  );
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
  // The releases to get. roarr's picks start selected; once you change the
  // selection, new picks no longer replace it.
  const pickKey = plan.picks.map((x) => x.release.key).join(",");
  const [chosen, setChosen] = useState<Set<string>>(new Set());
  const [touched, setTouched] = useState(false);
  useEffect(() => {
    if (!touched) setChosen(new Set(pickKey ? pickKey.split(",") : []));
  }, [pickKey, touched]);
  useEffect(() => setTouched(false), [p.wanted.join(",")]); // eslint-disable-line react-hooks/exhaustive-deps
  const choose = (key: string, on: boolean) => {
    setTouched(true);
    setChosen((s) => {
      const n = new Set(s);
      if (on) n.add(key);
      else n.delete(key);
      return n;
    });
  };

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
  const selected = plan.all.filter((r) => chosen.has(r.key) && !grabber.results.get(r.key)?.ok);
  const size = selected.reduce((n, r) => n + r.size, 0);
  const cells = p.kind === "tv" ? (p.cells ?? []) : [];

  const toggleSort = (key: SortKey) =>
    setSort((s) => (s.key === key ? { key, dir: s.dir === 1 ? -1 : 1 } : { key, dir: key === "age" ? 1 : -1 }));

  const cols = cells.length ? "grid-cols-[16px_minmax(64px,auto)_minmax(240px,1fr)_auto_72px_64px_120px_48px]" : "grid-cols-[16px_minmax(240px,1fr)_auto_72px_64px_120px_48px]";

  return (
    <Sheet open={p.open} onOpenChange={p.onOpenChange}>
      <SheetContent className="w-full gap-0 overflow-hidden p-0 sm:max-w-[1180px]">
        <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 pt-6 pr-16 pb-4 pl-6">
          <div className="flex min-w-0 items-baseline gap-3 pr-8">
            <span className="label-caps">Releases</span>
            <SheetTitle className="truncate text-[14px] font-normal">{p.title}</SheetTitle>
            <SheetDescription className="truncate font-mono text-[12px]">{p.detail ?? ""}</SheetDescription>
          </div>
          <div className="flex items-center gap-2 font-mono text-[12px] text-muted-foreground tabular">
            {!state.done && <Spinner />}
            <span>
              {answered}/{live.length || "…"} indexers
            </span>
            {failed.length > 0 && (
              <span className="text-bad" title={failed.map((i) => `${i.name}: ${i.errors[0]}`).join("\n")}>
                · {failed.length} failed
              </span>
            )}
          </div>
        </div>

        {state.error && (
          <Notice tone="bad" className="mx-6 mb-3">
            {state.error}
          </Notice>
        )}

        <div className="flex flex-wrap items-center gap-x-1 gap-y-2 px-6 pb-3">
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
            <label className="ml-auto inline-flex h-8 cursor-pointer items-center gap-2 text-[13px] text-muted-foreground select-none">
              <Checkbox checked={showRejected} onCheckedChange={(v) => setShowRejected(v === true)} />
              Show rejected
            </label>
          )}
        </div>

        <div className="min-h-0 flex-1 overflow-auto px-6 pb-4">
          <div className={cn("hidden h-8 items-center gap-x-4 border-b md:grid", cols)}>
            <span />
            {cells.length > 0 && <span className="meta">covers {p.cellsLabel}</span>}
            <Th sort={sort} k="best" onClick={toggleSort}>
              release
            </Th>
            <Th sort={sort} k="res" onClick={toggleSort}>
              format
            </Th>
            <Th sort={sort} k="size" onClick={toggleSort} right>
              size
            </Th>
            <Th sort={sort} k="seeders" onClick={toggleSort} right>
              seeders
            </Th>
            <Th>indexer</Th>
            <Th sort={sort} k="age" onClick={toggleSort} right>
              age
            </Th>
          </div>

          {list.length === 0 && <div className="py-12 text-center text-[13px] text-muted-foreground">{state.done ? (plan.all.length ? "No releases match." : "Nothing found.") : "Waiting for indexers…"}</div>}

          {list.slice(0, limit).map((r) => (
            <Row
              key={r.key}
              r={r}
              p={p}
              cols={cols}
              cells={cells}
              fills={picks.get(r.key)}
              bestScore={bestScore}
              open={expanded === r.key}
              onToggle={() => setExpanded(expanded === r.key ? null : r.key)}
              chosen={chosen.has(r.key)}
              onChoose={(on) => choose(r.key, on)}
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

        <div className="flex items-center justify-end gap-4 border-t px-6 py-4">
          {plan.missing.length > 0 && state.done && <span className="meta mr-auto">{plan.missing.length === 1 ? (p.epLabel?.(plan.missing[0]) ?? "1 episode") : `${plan.missing.length} episodes`} not available yet</span>}
          <Button size="lg" className="min-w-[240px] justify-between" disabled={selected.length === 0 || grabber.busy.size > 0} onClick={() => grabber.grabAll(selected, opts)}>
            <span className="flex items-center gap-2">
              {grabber.busy.size > 0 && <Spinner className="size-4" />}
              {selected.length === 0 ? "Select releases" : `Get ${selected.length === 1 ? (p.detail ?? "1 release") : `${selected.length} releases`}`}
            </span>
            {size > 0 && <span className="font-mono text-[12px] tabular">{bytes(size)}</span>}
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}

function Group({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="mr-4 inline-flex items-center gap-1">
      <span className="meta mr-1">{label}</span>
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
        "h-8 cursor-pointer rounded-[2px] px-2 font-mono text-[12px] text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/60",
        on && "bg-accent text-foreground",
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
      className={cn("meta inline-flex cursor-pointer items-center gap-1 outline-none hover:text-foreground focus-visible:underline", right && "justify-end", active && "text-foreground")}
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
  cols,
  cells,
  fills,
  bestScore,
  open,
  onToggle,
  chosen,
  onChoose,
  busy,
  result,
  onGrab,
}: {
  r: Release;
  p: ReleaseSheetProps;
  cols: string;
  cells: { id: number; tone: Tone }[];
  fills?: number[];
  bestScore: number;
  open: boolean;
  onToggle: () => void;
  chosen: boolean;
  onChoose: (on: boolean) => void;
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
  const tags = [resName(i.resolution), i.source, codecName(i.codec), i.hdr, i.tenBit && "10bit"].filter(Boolean) as string[];
  const audioBits = [i.audio, i.channels].filter(Boolean).join(" ");
  const sent = result?.ok;
  const failed = result && !result.ok;
  const showDetail = open || failed;

  const why = r.rejected ? (
    <span className="text-bad">Rejected: {(r.reasons ?? []).join(", ")}</span>
  ) : picked ? (
    <span className="text-foreground">
      roarr's pick, score {Math.round(r.score)}
      {p.kind === "tv" && fills ? `, fills ${fills.length} of ${p.wanted.length}` : ""}
    </span>
  ) : bestScore ? (
    <span className="text-muted-foreground">
      Score {Math.round(r.score)}, the pick has {Math.round(bestScore)}
      {p.kind === "tv" && extras > 0 ? `. Also ${extras} episodes you did not select` : ""}
    </span>
  ) : (
    <span className="text-muted-foreground">Score {Math.round(r.score)}</span>
  );

  const box = sent ? (
    <Check className="size-4 text-have" aria-label="Sent to the download client" />
  ) : busy ? (
    <Spinner />
  ) : (
    <Checkbox checked={chosen} onCheckedChange={(v) => onChoose(v === true)} onClick={(e) => e.stopPropagation()} aria-label={`Select ${r.title}`} />
  );

  return (
    <div className={cn("border-b", r.rejected && "opacity-60")}>
      {/* Desktop: one grid row. */}
      <div
        className={cn("hidden min-h-9 cursor-pointer items-center gap-x-4 py-1 outline-none hover:bg-card md:grid", cols, (open || chosen) && "bg-card")}
        role="button"
        tabIndex={0}
        aria-expanded={open}
        onClick={onToggle}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            onToggle();
          } else if (e.key === " ") {
            e.preventDefault();
            onChoose(!chosen);
          }
        }}
      >
        <span className="grid place-items-center">{box}</span>
        {cells.length > 0 && <Covers r={r} cells={cells} />}
        <span className="min-w-0 truncate font-mono text-[12px]" title={r.title}>
          {r.title}
        </span>
        <span className="flex flex-wrap gap-1">
          {tags.map((v) => (
            <Spec key={v} tone={v === "DV" || v.startsWith("HDR") ? "live" : r.rejected && /CAM|TS|TC/i.test(v) ? "bad" : undefined}>
              {v}
            </Spec>
          ))}
          <LangSpecs r={r} />
        </span>
        <span className="text-right font-mono text-[12px] tabular">{bytes(r.size)}</span>
        <span className={cn("text-right font-mono text-[12px] tabular", r.seeders === 0 && "text-bad")}>{r.seeders}</span>
        <span className="truncate text-[13px] text-muted-foreground">{r.indexers.length > 1 ? `${r.indexers.length} indexers` : r.indexer}</span>
        <span className="text-right font-mono text-[12px] text-muted-foreground tabular">{age(r.ageHours)}</span>
      </div>

      {/* Phone: stacked card. */}
      <div className="flex items-start gap-3 py-3 md:hidden">
        <span className="pt-1">{box}</span>
        <button className="min-w-0 flex-1 cursor-pointer text-left outline-none focus-visible:underline" aria-expanded={open} onClick={onToggle}>
          <span className="block font-mono text-[12px] leading-5 break-all">{r.title}</span>
          {cells.length > 0 && <Covers r={r} cells={cells} />}
          <span className="mt-2 flex flex-wrap gap-1">
            {cover && <Spec>{cover}</Spec>}
            {tags.map((v) => (
              <Spec key={v}>{v}</Spec>
            ))}
            <LangSpecs r={r} />
          </span>
          <span className="mt-2 flex flex-wrap gap-x-3 font-mono text-[12px] text-muted-foreground tabular">
            <span>{bytes(r.size)}</span>
            <span className={cn(r.seeders === 0 && "text-bad")}>{r.seeders} seeders</span>
            <span>{age(r.ageHours)}</span>
          </span>
        </button>
      </div>

      {showDetail && (
        <div className="mb-2 flex flex-wrap items-center gap-x-4 gap-y-2 rounded-[2px] bg-card px-4 py-3 text-[13px] md:ml-8">
          <span className="mr-auto">
            {why}
            {p.kind === "tv" && !picked && !r.rejected && nFills > 0 && <span className="text-muted-foreground">, fills {nFills}</span>}
          </span>
          <span className="flex basis-full flex-wrap gap-x-4 gap-y-1 font-mono text-[12px] text-muted-foreground">
            {cover && <span>{cover}</span>}
            {audioBits && <span>{audioBits}</span>}
            {i.group && <span>{i.group}</span>}
            {(i.repack || i.proper) && <span>{i.repack ? "repack" : "proper"}</span>}
            <span className="inline-flex gap-1">
              <SubSpecs r={r} />
            </span>
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
                <Button variant="outline" size="sm" className="text-destructive" disabled={busy} onClick={() => onGrab({ force: true })}>
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
