import { useEffect, useMemo, useState } from "react";
import { api, type DiscoverItem, type LibraryItem } from "../lib/api";
import { useRouter } from "../lib/router";
import { useStore } from "../lib/store";
import { PosterCard } from "../components/Poster";
import { cls, fold } from "../lib/format";

type Filter = "all" | "tv" | "movie" | "missing";

export function Home() {
  const { search } = useRouter();
  const q = search.get("q") ?? "";
  return q ? <Results q={q} /> : <Library />;
}

function hrefFor(it: LibraryItem) {
  return it.kind === "tv" ? `/tv/${it.id}` : `/movie/${it.id}`;
}

function missingPill(it: LibraryItem) {
  const missing = it.total - it.have;
  if (it.kind === "movie") return it.have ? null : <span className="pill warn">missing</span>;
  if (missing > 0) return <span className="pill warn">{missing} missing</span>;
  return null;
}

function Library() {
  const { library, libraryLoaded, libraryErrors, activity } = useStore();
  const [filter, setFilter] = useState<Filter>(() => (localStorage.getItem("roarr.filter") as Filter) || "all");
  const set = (f: Filter) => {
    localStorage.setItem("roarr.filter", f);
    setFilter(f);
  };
  const items = useMemo(
    () =>
      library.filter((it) => {
        switch (filter) {
          case "tv":
            return it.kind === "tv";
          case "movie":
            return it.kind === "movie";
          case "missing":
            return it.have < it.total;
        }
        return true;
      }),
    [library, filter],
  );
  const downloading = activity.filter((a) => a.status !== "failed").slice(0, 8);

  return (
    <>
      {Object.entries(libraryErrors).map(([svc, err]) => (
        <div key={svc} className="notice warn" style={{ marginBottom: 14 }}>
          {svc}: {err}
        </div>
      ))}
      {downloading.length > 0 && (
        <>
          <div className="section-title">Downloading</div>
          <div className="grid">
            {downloading.map((a) => (
              <PosterCard
                key={a.id}
                to={a.href ?? "/activity"}
                poster={a.poster}
                title={a.title}
                sub={a.subtitle || a.status}
                progress={a.progress}
                corner={<span className="pill accent">{Math.floor(a.progress * 100)}%</span>}
              />
            ))}
          </div>
        </>
      )}
      <div className="section-title">
        Library <span className="faint">{items.length}</span>
        <span className="spacer" />
        <div className="row" style={{ textTransform: "none", letterSpacing: 0 }}>
          {(["all", "tv", "movie", "missing"] as Filter[]).map((f) => (
            <button key={f} className={cls("chip", filter === f && "on")} onClick={() => set(f)}>
              {{ all: "All", tv: "Shows", movie: "Movies", missing: "Missing" }[f]}
            </button>
          ))}
        </div>
      </div>
      {!libraryLoaded ? (
        <div className="grid">
          {Array.from({ length: 12 }, (_, i) => (
            <div key={i} className="skeleton" style={{ aspectRatio: "2/3" }} />
          ))}
        </div>
      ) : items.length === 0 ? (
        <div className="empty">
          {library.length === 0 ? "Your library is empty. Search above to find something." : "Nothing matches this filter."}
        </div>
      ) : (
        <div className="grid">
          {items.map((it) => (
            <PosterCard
              key={it.key}
              to={hrefFor(it)}
              poster={it.poster}
              title={it.title}
              sub={[it.year, it.kind === "tv" ? `${it.have}/${it.total} eps` : it.have ? "on disk" : null].filter(Boolean).join(" · ")}
              corner={missingPill(it)}
            />
          ))}
        </div>
      )}
    </>
  );
}

/** Ranks library hits: exact > prefix > word prefix > substring. */
function rankLibrary(items: LibraryItem[], q: string): LibraryItem[] {
  const f = fold(q);
  if (!f) return [];
  const scored: [number, LibraryItem][] = [];
  for (const it of items) {
    let best = 0;
    for (const t of [it.title, ...(it.aliases ?? [])]) {
      const ft = fold(t);
      let s = 0;
      if (ft === f) s = 100;
      else if (ft.startsWith(f)) s = 80;
      else if ((" " + ft).includes(" " + f)) s = 60;
      else if (ft.includes(f)) s = 40;
      else if (f.split(" ").every((w) => ft.includes(w))) s = 25;
      best = Math.max(best, s);
    }
    if (best > 0) scored.push([best, it]);
  }
  scored.sort((a, b) => b[0] - a[0] || a[1].title.localeCompare(b[1].title));
  return scored.map((x) => x[1]);
}

function Results({ q }: { q: string }) {
  const { library } = useStore();
  const local = useMemo(() => rankLibrary(library, q), [library, q]);
  const [remote, setRemote] = useState<DiscoverItem[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    setLoading(true);
    setError(null);
    let alive = true;
    const t = setTimeout(() => {
      api
        .discover(q)
        .then((r) => alive && setRemote(r.results))
        .catch((e) => alive && setError(String(e.message ?? e)))
        .finally(() => alive && setLoading(false));
    }, 220);
    return () => {
      alive = false;
      clearTimeout(t);
    };
  }, [q]);

  const libKeys = new Set(local.map((l) => `${l.kind}:${l.id}`));
  const others = (remote ?? []).filter((r) => !(r.inLibrary && libKeys.has(`${r.kind}:${r.libraryId}`)));

  return (
    <>
      {local.length > 0 && (
        <>
          <div className="section-title">In your library</div>
          <div className="grid">
            {local.slice(0, 24).map((it) => (
              <PosterCard key={it.key} to={hrefFor(it)} poster={it.poster} title={it.title} sub={[it.year, it.kind === "tv" ? "Show" : "Movie"].filter(Boolean).join(" · ")} corner={missingPill(it)} />
            ))}
          </div>
        </>
      )}
      <div className="section-title">
        Everything else {loading && <span className="spinner" />}
      </div>
      {error && <div className="notice bad">{error}</div>}
      {!loading && remote && others.length === 0 && !error && <div className="empty">No other matches for “{q}”.</div>}
      <div className="grid">
        {others.map((r) => (
          <PosterCard
            key={`${r.kind}:${r.tmdbId ?? r.tvdbId}`}
            to={discoverHref(r)}
            poster={r.poster}
            title={r.title}
            sub={[r.year, r.kind === "tv" ? "Show" : "Movie"].filter(Boolean).join(" · ")}
            corner={r.inLibrary ? <span className="pill accent">in library</span> : r.requested ? <span className="pill">requested</span> : null}
          />
        ))}
      </div>
    </>
  );
}

export function discoverHref(r: DiscoverItem): string {
  if (r.inLibrary && r.libraryId) return r.kind === "tv" ? `/tv/${r.libraryId}` : `/movie/${r.libraryId}`;
  if (r.kind === "movie") return `/movie/tmdb/${r.tmdbId}`;
  return `/tv/tvdb/${r.tvdbId ?? 0}${r.tmdbId ? `?tmdb=${r.tmdbId}` : ""}`;
}
