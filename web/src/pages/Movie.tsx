import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ListChecks, RefreshCw, Trash2 } from "lucide-react";
import { api, type MovieDetail, type Release } from "@/lib/api";
import { autoGet, run } from "@/lib/actions";
import { bytes } from "@/lib/format";
import { useGrabber, useLivePlan, type GrabOpts } from "@/lib/releases";
import { useRouter } from "@/lib/router";
import { useStore } from "@/lib/store";
import { peek } from "@/lib/cache";
import { keys, loadMovie, loadMovieByTmdb } from "@/lib/prefetch";
import { baseName, jobTag, useConverting } from "@/lib/transcode";
import { forMovie, isBlocking, isFlagged } from "@/lib/verify";
import { useSearch } from "@/lib/useSearch";
import { cn } from "@/lib/utils";
import { Checkbox } from "@/components/ui/checkbox";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { Dot, Notice, SectionHead, Spec, State } from "@/components/kit";
import { useConfirm } from "@/components/Confirm";
import { FileFacts, ManageMenu } from "@/components/Manage";
import { ReleaseSheet } from "@/components/ReleaseSheet";
import { VerdictActions, VerdictChecks } from "@/components/Verdict";
import { Crumbs, GetCard, TitleHead, TitleLayout, TitleSkeleton, type GetOption } from "@/components/Title";

export function MoviePage({ id, tmdb }: { id?: number; tmdb?: number }) {
  const { library } = useStore();
  const key = id ? keys.movie(id) : keys.movieTmdb(tmdb ?? 0);
  // A hover on the link usually filled the cache already.
  const [d, setD] = useState<MovieDetail | null>(() => peek<MovieDetail>(key) ?? null);
  const [error, setError] = useState<string | null>(null);
  const shown = useRef(!!d);
  const load = useCallback(
    (fresh = false) =>
      (id ? loadMovie(id, fresh) : loadMovieByTmdb(tmdb ?? 0, fresh))
        .then((x) => {
          shown.current = true;
          setD(x);
        })
        // A failed refresh keeps the page that is on screen.
        .catch((e) => !shown.current && setError(String(e.message ?? e))),
    [id, tmdb],
  );
  useEffect(() => {
    load();
  }, [load]);
  if (error) return <Notice tone="bad">{error}</Notice>;
  if (!d) return <TitleSkeleton section="Movies" known={library.find((i) => i.kind === "movie" && (id ? i.id === id : i.tmdbId === tmdb))} />;
  return <MovieView d={d} reload={() => load(true)} />;
}

type Scope = "this" | "missing" | "all" | "custom";

function MovieView({ d, reload }: { d: MovieDetail; reload: () => void }) {
  const { reloadActivity, reloadLibrary, verify } = useStore();
  const check = useMemo(() => {
    const r = d.inLibrary ? forMovie(verify, d.id) : undefined;
    return r && isFlagged(r) ? r : undefined;
  }, [verify, d.id, d.inLibrary]);
  const converting = useConverting("movie", d.inLibrary ? d.id : 0);
  const job = d.file ? converting.get(baseName(d.file.path)) : undefined;
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
  // The button confirms the grab for 2 s: "Got Dune: Part Two · 8.1 GB".
  const [got, setGot] = useState<{ key: string; label: string } | null>(null);
  const gotTimer = useRef(0);
  const markGot = (key: string) => {
    const what = key.split(",").length === 1 ? (titles.get(+key) ?? d.title) : `${key.split(",").length} movies`;
    setGot({ key, label: `Got ${what}` });
    clearTimeout(gotTimer.current);
    gotTimer.current = window.setTimeout(() => setGot(null), 2000);
  };
  useEffect(() => () => clearTimeout(gotTimer.current), []);

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

  const confirm = useConfirm();
  const changed = () => {
    reload();
    reloadLibrary();
  };
  const fileSize = d.file?.size ?? d.sizeOnDisk ?? 0;

  const deleteFile = async () => {
    const a = await confirm({
      title: `Delete ${d.title} from disk?`,
      body: <p>Frees {bytes(fileSize)}. Radarr uses its recycle bin when one is set up; otherwise the file is gone.</p>,
      options: [{ id: "unmonitor", label: "Stop monitoring", hint: "so Radarr does not download it again", checked: true }],
      confirm: "Delete file",
      danger: true,
    });
    if (!a) return;
    const ok = await run(`Deleting ${d.title}…`, async () => {
      const res = await api.deleteMovieFile(d.id);
      if (a.checked.unmonitor) await api.patchTitle("movie", d.id, { monitored: false });
      return res;
    });
    if (ok) changed();
  };

  // Replace = delete the file, then get the movie again.
  const replace = async (pick = false) => {
    const a = await confirm({
      title: `Replace ${d.title}?`,
      body: <p>Deletes the current file ({bytes(fileSize)}) and downloads the movie again.</p>,
      options: [{ id: "pick", label: "Pick the release manually", checked: pick }],
      confirm: "Replace",
      danger: true,
    });
    if (!a) return false;
    if (!(await run(`Deleting ${d.title}…`, () => api.deleteMovieFile(d.id)))) return false;
    setScope("this");
    if (a.checked.pick) {
      setSheet(true);
      changed();
      return false;
    }
    return true;
  };

  const replaceNow = async () => {
    if (!(await replace())) return;
    const res = await autoGet({ kind: "movie", movies: [d.tmdbId!] }, d.title);
    if (res) {
      markGot(String(d.tmdbId));
      after(d.id, d.tmdbId);
    }
  };

  const get = async () => {
    if (d.inLibrary && d.hasFile && sel.has(d.tmdbId!)) {
      if (!(await replace())) return;
    }
    setBusy(true);
    try {
      if (search.done && plan.picks.length) {
        if (await grabber.grabAll(plan.picks.map((x) => x.release))) markGot(selKey);
      } else {
        const res = await autoGet({ kind: "movie", movies: selArr }, selArr.length === 1 ? (titles.get(selArr[0]) ?? d.title) : `${selArr.length} movies`);
        if (res) {
          markGot(selKey);
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
    { value: "this", label: d.title },
    ...(inCollection
      ? [
          { value: "missing", label: "Missing from collection", disabled: missingParts.length === 0 },
          { value: "all", label: "Whole collection" },
          { value: "custom", label: "Selected movies" },
        ]
      : []),
  ];
  const cta =
    got?.key === selKey
      ? got.label
      : n === 0
        ? "Select movies"
        : n === 1
          ? selArr[0] === d.tmdbId && d.hasFile
            ? `Replace ${d.title}`
            : `Get ${titles.get(selArr[0])}`
          : `Get ${n} movies`;
  const sheetDetail = n > 1 ? selArr.map((t) => titles.get(t)).join(", ") : d.year ? String(d.year) : undefined;

  return (
    <>
      <Crumbs section="Movies" />
      <TitleLayout
        head={
          <TitleHead
            poster={d.poster}
            title={d.title}
            year={d.year}
            meta={[d.year, d.runtime && `${d.runtime} min`, d.hasFile && d.sizeOnDisk ? bytes(d.sizeOnDisk) : null]}
            state={
              d.hasFile ? (
                <>
                  <State tone="have">On disk</State>
                  {check && isBlocking(check) && <State tone="bad">Fails check</State>}
                  {!d.monitored && <State tone="none">Not monitored</State>}
                </>
              ) : d.queued ? (
                <State tone="live">Downloading</State>
              ) : d.inLibrary ? (
                <>
                  <State tone="miss">Missing</State>
                  {!d.monitored && <State tone="none">Not monitored</State>}
                </>
              ) : (
                <State tone="none">Not in Radarr</State>
              )
            }
            actions={
              d.inLibrary && (
                <ManageMenu kind="movie" id={d.id} title={d.title} monitored={d.monitored} profileId={d.qualityProfileId} path={d.path} onChanged={changed}>
                    <DropdownMenuItem disabled={!d.hasFile} onSelect={() => replaceNow()}>
                      <RefreshCw /> Replace
                    </DropdownMenuItem>
                    <DropdownMenuItem disabled={!d.hasFile} onSelect={() => replace(true)}>
                      <ListChecks /> Replace, pick release manually
                    </DropdownMenuItem>
                    <DropdownMenuItem variant="destructive" disabled={!d.hasFile} onSelect={deleteFile}>
                      <Trash2 /> Delete file
                    </DropdownMenuItem>
                  </ManageMenu>
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
            disabled={n === 0}
            plan={plan}
            wantedCount={n}
            onChoose={() => setSheet(true)}
            chooseLabel={n === 1 ? "Pick release manually" : "Pick releases manually"}
          />
        }
        body={
          <>
            {(d.file || check) && (
              <section className="border-t pt-4">
                <SectionHead title="File" className="mb-3">
                  {job && (
                    <Spec tone="live" title="Converting to AV1">
                      {jobTag(job)}
                    </Spec>
                  )}
                </SectionHead>
                {check && (
                  <div className="mb-4 rounded-[2px] bg-card p-4">
                    <div className="mb-2 truncate font-mono text-[11px] text-muted-foreground" title={check.release}>
                      {check.release}
                    </div>
                    <VerdictChecks r={check} />
                    <VerdictActions r={check} className="mt-3" />
                  </div>
                )}
                {d.file && <FileFacts file={d.file} />}
              </section>
            )}
            {inCollection && (
              <section className={cn("border-t pt-4", (d.file || check) && "mt-6")}>
                <SectionHead title={d.collection?.title} className="mb-2" />
                {parts.map((p) => {
                  const on = sel.has(p.tmdbId);
                  const tone = p.hasFile ? "have" : p.inLibrary ? "miss" : "none";
                  return (
                    <label key={p.tmdbId} className={cn("flex h-10 cursor-pointer items-center gap-4 border-b px-2 hover:bg-card", on && "bg-card")}>
                      <Checkbox checked={on} onCheckedChange={() => toggle(p.tmdbId)} aria-label={p.title} />
                      <span className="w-10 shrink-0 font-mono text-[12px] text-muted-foreground tabular">{p.year ?? ""}</span>
                      <span className={cn("min-w-0 flex-1 truncate text-[14px]", p.tmdbId === d.tmdbId && "font-medium")}>{p.title}</span>
                      <span className="inline-flex items-center gap-2 font-mono text-[12px] text-muted-foreground">
                        <Dot tone={tone} />
                        {p.hasFile ? "on disk" : p.inLibrary ? <span className="text-miss">missing</span> : "not added"}
                      </span>
                    </label>
                  );
                })}
              </section>
            )}
          </>
        }
      />
      <ReleaseSheet
        open={sheet}
        onOpenChange={setSheet}
        title={n === 1 ? (titles.get(selArr[0]) ?? d.title) : (d.collection?.title ?? d.title)}
        detail={sheetDetail}
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
