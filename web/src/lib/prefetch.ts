import { api, type SeriesDetail } from "./api";
import { cached, put } from "./cache";
import { match } from "./router";

// Everything a page change waits on, fetched ahead: the page's code and its
// data. Links call prefetch() on hover, focus and press.

/** A page's code: loads once, and remembers the module when it has arrived. */
export interface Chunk<M> {
  (): Promise<M>;
  mod?: M;
}

function chunk<M>(load: () => Promise<M>): Chunk<M> {
  let p: Promise<M> | undefined;
  const c: Chunk<M> = () =>
    (p ??= load().then(
      (m) => (c.mod = m),
      (err) => {
        p = undefined; // a failed download can be tried again
        throw err;
      },
    ));
  return c;
}

export const pages = {
  series: chunk(() => import("../pages/Series")),
  movie: chunk(() => import("../pages/Movie")),
  activity: chunk(() => import("../pages/Activity")),
  requests: chunk(() => import("../pages/Requests")),
  settings: chunk(() => import("../pages/Settings")),
};

/** Loads every page's code once the browser is idle, so no click waits on a chunk. */
export function warmPages() {
  const idle = window.requestIdleCallback ?? ((f: () => void) => setTimeout(f, 300));
  idle(() => Object.values(pages).forEach((p) => p().catch(() => {})));
}

// Detail reads younger than this come from the cache without a request.
const FRESH = 15_000;

const normSeries = (x: SeriesDetail): SeriesDetail => ({ ...x, seasons: (x.seasons ?? []).map((s) => ({ ...s, episodes: s.episodes ?? [] })) });

export const keys = {
  series: (id: number) => `tv:${id}`,
  seriesTvdb: (tvdb: number, tmdb?: number) => `tvdb:${tvdb}:${tmdb ?? 0}`,
  movie: (id: number) => `movie:${id}`,
  movieTmdb: (tmdb: number) => `tmdb:${tmdb}`,
};

// A forced read skips both caches (here and in roarr) and stores the answer.
function read<T>(key: string, load: () => Promise<T>, fresh: boolean): Promise<T> {
  return fresh ? load().then((v) => (put(key, v), v)) : cached(key, load, FRESH);
}

export const loadSeries = (id: number, fresh = false) => read(keys.series(id), () => api.series(id, fresh).then(normSeries), fresh);
export const loadSeriesByTvdb = (tvdb: number, tmdb?: number, fresh = false) =>
  read(keys.seriesTvdb(tvdb, tmdb), () => api.seriesByTvdb(tvdb, tmdb).then(normSeries), fresh);
export const loadMovie = (id: number, fresh = false) => read(keys.movie(id), () => api.movie(id), fresh);
export const loadMovieByTmdb = (tmdb: number, fresh = false) => read(keys.movieTmdb(tmdb), () => api.movieByTmdb(tmdb), fresh);

/** Settings change rarely; a minute-old copy is fine for reading preferences. */
export const loadSettings = () => cached("settings", api.settings, 60_000);

/** Starts loading the code and the data that the page at `to` needs. */
export function prefetch(to: string) {
  const u = new URL(to, location.origin);
  const path = u.pathname;
  const quiet = (p: Promise<unknown>) => p.catch(() => {});
  let m: Record<string, string> | null;
  if ((m = match("/tv/tvdb/:tvdb", path))) {
    quiet(pages.series());
    quiet(loadSeriesByTvdb(+m.tvdb, +(u.searchParams.get("tmdb") ?? 0) || undefined));
  } else if ((m = match("/tv/:id", path))) {
    quiet(pages.series());
    quiet(loadSeries(+m.id));
  } else if ((m = match("/movie/tmdb/:tmdb", path))) {
    quiet(pages.movie());
    quiet(loadMovieByTmdb(+m.tmdb));
  } else if ((m = match("/movie/:id", path))) {
    quiet(pages.movie());
    quiet(loadMovie(+m.id));
  } else if (path === "/downloads" || path === "/activity") quiet(pages.activity());
  else if (path === "/requests") quiet(pages.requests());
  else if (path === "/admin" || path === "/settings") {
    quiet(pages.settings());
    quiet(loadSettings());
  }
}
