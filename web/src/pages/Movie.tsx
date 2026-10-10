import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Check } from "lucide-react";
import { api, type MovieDetail, type Release } from "@/lib/api";
import { autoGet } from "@/lib/actions";
import { bytes } from "@/lib/format";
import { useGrabber, useLivePlan, type GrabOpts } from "@/lib/releases";
import { useRouter } from "@/lib/router";
import { useStore } from "@/lib/store";
import { useSearch } from "@/lib/useSearch";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Label, Notice } from "@/components/kit";
import { ReleaseSheet } from "@/components/ReleaseSheet";
import { Crumbs, GetCard, PosterColumn, TitleHead, TitleLayout, TitleSkeleton, type GetOption } from "@/components/Title";

export function MoviePage({ id, tmdb }: { id?: number; tmdb?: number }) {
  const [d, setD] = useState<MovieDetail | null>(null);
  const [error, setError] = useState<string | null>(null);
  const load = useCallback(
    () =>
      (id ? api.movie(id) : api.movieByTmdb(tmdb ?? 0))
        .then(setD)
        .catch((e) => setError(String(e.message ?? e))),
    [id, tmdb],
  );
  useEffect(() => {
    load();
  }, [load]);
  if (error) return <Notice tone="bad">{error}</Notice>;
  if (!d) return <TitleSkeleton />;
  return <MovieView d={d} reload={load} />;
}

type Scope = "this" | "missing" | "all" | "custom";

function MovieView({ d, reload }: { d: MovieDetail; reload: () => void }) {
  const { reloadActivity, reloadLibrary } = useStore();
  const { navigate, search: params } = useRouter();
  const parts = d.collection?.movies ?? [];
  const inCollection = parts.length > 1;
  const titles = useMemo(() => {
    const m = new Map<number, string>([[d.tmdbId!, d.title]]);
    for (const p of parts) m.set(p.tmdbId, p.title);
    return m;
  }, [d, parts]);
  const missingParts = parts.filter((p) => !p.hasFile).map((p) => p.tmdbId);

  const [scope, setScope] = useState<Scope>("this");
  const [ticked, setTicked] = useState<Set<number>>(new Set());
  const [sheet, setSheet] = useState(params.get("pick") === "1");
  const [busy, setBusy] = useState(false);
  const [gotFor, setGotFor] = useState<string | null>(null);

  const selArr = useMemo(() => {
    const ids = scope === "this" ? [d.tmdbId!] : scope === "missing" ? missingParts : scope === "all" ? parts.map((p) => p.tmdbId) : [...ticked];
    return [...ids].sort((a, b) => a - b);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [scope, ticked, d.tmdbId, parts.length, missingParts.join(",")]);
  const sel = useMemo(() => new Set(selArr), [selArr]);
  const selKey = selArr.join(",");

  const streams = useMemo(() => selArr.map((t) => ({ kind: "movie", tmdb: String(t) })), [selArr]);
  const search = useSearch(`movie:${d.tmdbId}`, streams);
  const relevant = useCallback((r: Release) => r.movieTmdb !== undefined && sel.has(r.movieTmdb), [sel]);
  const plan = useLivePlan(search, relevant, selArr, "movie");

  const libraryIdFor = (tmdb: number) => (tmdb === d.tmdbId && d.inLibrary ? d.id : (parts.find((p) => p.tmdbId === tmdb)?.libraryId ?? 0));

  const later = useRef(0);
  const after = (movieId?: number, tmdb?: number) => {
    reloadActivity();
    clearTimeout(later.current);
    later.current = window.setTimeout(() => {
      reloadLibrary();
      if (!d.inLibrary && movieId && tmdb === d.tmdbId) navigate(`/movie/${movieId}`, { replace: true });
      else reload();
    }, 1200);
  };

  const grabber = useGrabber(
    useCallback(
      async (r: Release, opts: GrabOpts) => {
        const res = await api.grab({ kind: "movie", movieId: libraryIdFor(r.movieTmdb!), tmdbId: r.movieTmdb, release: r, force: opts.force });
        if (res.ok) after(res.movieId, r.movieTmdb);
        return res;
      },
      // eslint-disable-next-line react-hooks/exhaustive-deps
      [d],
    ),
  );

  const get = async () => {
    setBusy(true);
    try {
      if (search.done && plan.picks.length) {
        if (await grabber.grabAll(plan.picks.map((x) => x.release))) setGotFor(selKey);
      } else {
        const res = await autoGet({ kind: "movie", movies: selArr }, selArr.length === 1 ? (titles.get(selArr[0]) ?? d.title) : `${selArr.length} movies`);
        if (res) {
          setGotFor(selKey);
          after(res.grabs?.find((g) => g.result.ok)?.result.movieId, selArr[0]);
        }
      }
    } finally {
      setBusy(false);
    }
  };

  const toggle = (tmdb: number) => {
    const n = new Set(scope === "custom" ? ticked : sel);
    if (n.has(tmdb)) n.delete(tmdb);
    else n.add(tmdb);
    setTicked(n);
    setScope("custom");
  };

  const n = selArr.length;
  const options: GetOption[] = [
    { value: "this", label: d.title, sub: [d.year, d.hasFile ? "on disk" : d.queued ? "downloading" : "not on disk"].filter(Boolean).join(" · "), count: 1 },
    ...(inCollection
      ? [
          { value: "missing", label: `Missing from the collection`, sub: d.collection?.title, count: missingParts.length, disabled: missingParts.length === 0 },
          { value: "all", label: "Whole collection", sub: d.collection?.title, count: parts.length },
          { value: "custom", label: "Movies I tick", sub: "in the list below", count: scope === "custom" ? ticked.size || "–" : "–" },
        ]
      : []),
  ];
  const cta =
    n === 0 ? "Tick movies to get" : n === 1 ? (selArr[0] === d.tmdbId && d.hasFile ? "Get it again" : `Get ${titles.get(selArr[0]) === d.title ? "it" : titles.get(selArr[0])}`) : `Get ${n} movies`;
  const sheetTitle = n === 1 ? (titles.get(selArr[0]) ?? d.title) : `${n} movies from ${d.collection?.title ?? "the collection"}`;

  return (
    <>
      <Crumbs section="Movies" />
      <TitleLayout
        poster={<PosterColumn poster={d.poster} title={d.title} year={d.year} />}
        head={
          <TitleHead
            poster={d.poster}
            title={d.title}
            year={d.year}
            meta={["Movie", d.year, d.network, d.runtime && `${d.runtime} min`, d.genres?.slice(0, 3).join(", ")]}
            overview={d.overview}
            badges={
              d.hasFile ? (
                <Badge variant="ok">
                  <Check /> On disk{d.sizeOnDisk ? ` · ${bytes(d.sizeOnDisk)}` : ""}
                </Badge>
              ) : d.queued ? (
                <Badge variant="ice">Downloading</Badge>
              ) : d.inLibrary ? (
                <Badge variant="pink">Missing</Badge>
              ) : (
                <Badge variant="ice">Not in Radarr yet · added when you get it</Badge>
              )
            }
          />
        }
        aside={
          <GetCard
            options={options}
            value={scope}
            onValueChange={(v) => {
              if (v === "custom" && scope !== "custom") setTicked(new Set(sel));
              setScope(v as Scope);
            }}
            cta={cta}
            onGet={get}
            busy={busy}
            done={gotFor === selKey}
            disabled={n === 0}
            state={search}
            plan={plan}
            wantedCount={n}
            onChoose={() => setSheet(true)}
          />
        }
        body={
          inCollection && (
            <section className="border-t border-dashed border-dash pt-5">
              <div className="mb-3 flex items-baseline gap-3">
                <Label>{d.collection?.title}</Label>
                <span className="font-mono text-xs text-muted-foreground">
                  {parts.filter((p) => p.hasFile).length}/{parts.length} on disk
                </span>
              </div>
              {parts.map((p) => {
                const on = sel.has(p.tmdbId);
                return (
                  <label
                    key={p.tmdbId}
                    className={cn(
                      "-mx-2.5 flex min-h-11 cursor-pointer items-center gap-3.5 rounded-lg px-2.5 transition-colors hover:bg-muted/60",
                      on && "bg-lime/45 hover:bg-lime/55 dark:bg-lime/12 dark:hover:bg-lime/18",
                    )}
                  >
                    <Checkbox checked={on} onCheckedChange={() => toggle(p.tmdbId)} aria-label={p.title} />
                    <span className="w-10 shrink-0 font-mono text-xs text-muted-foreground">{p.year ?? ""}</span>
                    <span className={cn("min-w-0 flex-1 truncate", p.tmdbId === d.tmdbId && "font-semibold")}>{p.title}</span>
                    {p.hasFile ? (
                      <span className="font-mono text-[11px] tracking-wide text-ok uppercase">✓ on disk</span>
                    ) : p.inLibrary ? (
                      <Badge variant="pink">Missing</Badge>
                    ) : (
                      <span className="font-mono text-[11px] text-muted-foreground uppercase">not added</span>
                    )}
                  </label>
                );
              })}
            </section>
          )
        }
      />
      <ReleaseSheet
        open={sheet}
        onOpenChange={setSheet}
        title={sheetTitle}
        detail={n > 1 ? selArr.map((t) => titles.get(t)).join(" · ") : d.year ? String(d.year) : undefined}
        kind="movie"
        state={search}
        plan={plan}
        wanted={selArr}
        grabber={grabber}
        movieLabel={(t) => titles.get(t) ?? String(t)}
      />
    </>
  );
}
