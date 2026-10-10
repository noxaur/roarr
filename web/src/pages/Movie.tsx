import { useCallback, useEffect, useMemo, useState } from "react";
import { api, type MovieDetail, type Release } from "../lib/api";
import { useSearch } from "../lib/useSearch";
import { useRouter } from "../lib/router";
import { useStore } from "../lib/store";
import { Releases, type GrabOpts } from "../components/Releases";
import { PosterImg } from "../components/Poster";
import { useToast } from "../components/Toasts";
import { BoltIcon, CheckIcon } from "../components/Icons";
import { bytes, cls } from "../lib/format";
import { TitleSkeleton } from "./Series";

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
  if (error) return <div className="notice bad">{error}</div>;
  if (!d) return <TitleSkeleton />;
  return <MovieView d={d} reload={load} />;
}

function MovieView({ d, reload }: { d: MovieDetail; reload: () => void }) {
  const { mode, setMode, reloadActivity, reloadLibrary } = useStore();
  const { navigate } = useRouter();
  const toast = useToast();
  const [sel, setSel] = useState<Set<number>>(() => new Set([d.tmdbId!]));
  const [busy, setBusy] = useState(false);
  const parts = d.collection?.movies ?? [];
  const titles = useMemo(() => {
    const m = new Map<number, string>([[d.tmdbId!, d.title]]);
    for (const p of parts) m.set(p.tmdbId, p.title);
    return m;
  }, [d, parts]);
  const missingParts = parts.filter((p) => !p.hasFile).map((p) => p.tmdbId);

  const selArr = useMemo(() => [...sel].sort((a, b) => a - b), [sel]);
  const streams = useMemo(() => selArr.map((t) => ({ kind: "movie", tmdb: String(t) })), [selArr]);
  const search = useSearch(`movie:${d.tmdbId}`, streams);
  const relevant = useCallback((r: Release) => r.movieTmdb !== undefined && sel.has(r.movieTmdb), [sel]);

  const libraryIdFor = (tmdb: number) => (tmdb === d.tmdbId && d.inLibrary ? d.id : parts.find((p) => p.tmdbId === tmdb)?.libraryId ?? 0);

  const after = (movieId?: number, tmdb?: number) => {
    reloadActivity();
    setTimeout(() => {
      reloadLibrary();
      if (!d.inLibrary && movieId && tmdb === d.tmdbId) navigate(`/movie/${movieId}`, { replace: true });
      else reload();
    }, 1200);
  };

  const onGrab = async (r: Release, opts: GrabOpts) => {
    const res = await api.grab({ kind: "movie", movieId: libraryIdFor(r.movieTmdb!), tmdbId: r.movieTmdb, release: r, force: opts.force });
    if (res.ok) after(res.movieId, r.movieTmdb);
    return res;
  };

  const runAuto = async (ids: number[]) => {
    setBusy(true);
    toast(`Finding the best release${ids.length > 1 ? "s" : ""}…`);
    try {
      const res = await api.auto({ kind: "movie", movies: ids });
      const ok = (res.grabs ?? []).filter((g) => g.result.ok);
      toast(
        <div>
          <b>{res.message}</b>
          {(res.grabs ?? []).map((g) => (
            <div key={g.release} className="small muted ellipsis">
              {g.result.ok ? "✓" : "✕"} {g.release}
            </div>
          ))}
        </div>,
        ok.length ? "ok" : "bad",
      );
      if (ok.length) after(ok[0].result.movieId, ids[0]);
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), "bad");
    } finally {
      setBusy(false);
    }
  };

  const scopeLabel = sel.size === 1 ? titles.get(selArr[0]) ?? "Movie" : `${sel.size} movies from ${d.collection?.title ?? "the collection"}`;

  return (
    <>
      <div className="hero">
        {d.fanart && <div className="backdrop" style={{ backgroundImage: `url(${d.fanart})` }} />}
        <div className="hero-inner">
          <div className="poster">
            <PosterImg src={d.poster} title={d.title} />
          </div>
          <div className="grow">
            <div className="small muted">{[d.year, d.network, d.runtime ? `${d.runtime} min` : null, d.genres?.slice(0, 3).join(", ")].filter(Boolean).join(" · ")}</div>
            <h1>{d.title}</h1>
            {d.overview && <p className="overview">{d.overview}</p>}
            <div className="row wrap" style={{ marginTop: 12 }}>
              {d.hasFile ? (
                <span className="tag good">On disk{d.sizeOnDisk ? ` · ${bytes(d.sizeOnDisk)}` : ""}</span>
              ) : d.queued ? (
                <span className="tag info">Downloading</span>
              ) : d.inLibrary ? (
                <span className="tag warn">Missing</span>
              ) : (
                <span className="tag info">Not in Radarr yet. Added automatically when you grab.</span>
              )}
              <span className="spacer" />
              <div className="seg">
                <button className={cls(mode === "manual" && "on")} onClick={() => setMode("manual")}>
                  Manual
                </button>
                <button className={cls(mode === "auto" && "on")} onClick={() => setMode("auto")}>
                  <BoltIcon width={12} height={12} /> Auto
                </button>
              </div>
              {mode === "auto" && (
                <button className="btn primary sm" disabled={busy} onClick={() => runAuto(selArr)}>
                  <BoltIcon width={12} height={12} /> Get {sel.size > 1 ? `${sel.size} movies` : "it"}
                </button>
              )}
            </div>
          </div>
        </div>
      </div>

      <div className={parts.length > 1 ? "title-layout" : ""} style={parts.length > 1 ? undefined : { marginTop: 18 }}>
        {parts.length > 1 && (
          <div className="seasons">
            <div className="season">
              <div className="season-head" style={{ cursor: "default" }}>
                <b>{d.collection?.title}</b>
                <span className="small muted mono">
                  {parts.filter((p) => p.hasFile).length}/{parts.length}
                </span>
                <span className="spacer" />
                <button className="btn xs" onClick={() => setSel(new Set(missingParts.length ? missingParts : parts.map((p) => p.tmdbId)))}>
                  {missingParts.length ? "All missing" : "All"}
                </button>
              </div>
              <div className="episodes">
                {parts.map((p) => (
                  <div
                    key={p.tmdbId}
                    className={cls("episode", sel.has(p.tmdbId) && "sel")}
                    onClick={(ev) => {
                      if (ev.shiftKey || ev.metaKey || ev.ctrlKey) {
                        const n = new Set(sel);
                        if (n.has(p.tmdbId)) n.delete(p.tmdbId);
                        else n.add(p.tmdbId);
                        setSel(n);
                      } else setSel(new Set([p.tmdbId]));
                    }}
                  >
                    <input
                      type="checkbox"
                      checked={sel.has(p.tmdbId)}
                      onClick={(ev) => ev.stopPropagation()}
                      onChange={() => {
                        const n = new Set(sel);
                        if (n.has(p.tmdbId)) n.delete(p.tmdbId);
                        else n.add(p.tmdbId);
                        setSel(n);
                      }}
                      style={{ accentColor: "var(--accent)" }}
                    />
                    <span className="num">{p.year ?? ""}</span>
                    <span className="grow ellipsis">{p.title}</span>
                    {p.hasFile ? <CheckIcon style={{ color: "var(--accent)" }} /> : p.inLibrary ? <span className="dot warn" title="missing" /> : <span className="tiny faint">not added</span>}
                  </div>
                ))}
              </div>
            </div>
            <div className="small faint" style={{ padding: "4px 2px" }}>
              Shift-click to fetch several movies at once.
            </div>
          </div>
        )}
        <div className={parts.length > 1 ? "sticky" : ""}>
          <Releases
            state={search}
            relevant={relevant}
            wanted={selArr}
            kind="movie"
            scopeLabel={scopeLabel}
            movieLabel={(t) => titles.get(t) ?? String(t)}
            onGrab={onGrab}
            watchFirstDefault={false}
          />
        </div>
      </div>
    </>
  );
}
