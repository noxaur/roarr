// Typed client for roarr's API. Types mirror the Go structs in internal/app.

export type Kind = "tv" | "movie";

export interface LibraryItem {
  key: string;
  kind: Kind;
  id: number;
  title: string;
  sortTitle?: string;
  year?: number;
  poster?: string;
  status?: string;
  network?: string;
  monitored: boolean;
  have: number;
  total: number;
  sizeOnDisk?: number;
  tvdbId?: number;
  tmdbId?: number;
  imdbId?: string;
  aliases?: string[];
  anime?: boolean;
}

export interface EpisodeDetail {
  id: number;
  season: number;
  number: number;
  absolute?: number;
  title: string;
  airDate?: string;
  aired: boolean;
  hasFile: boolean;
  monitored: boolean;
  queued?: boolean;
  runtime?: number;
  file?: FileInfo;
}

/** A file on disk, as Sonarr/Radarr describe it. */
export interface FileInfo {
  id: number;
  path: string;
  size: number;
  quality?: string;
  resolution?: number;
  group?: string;
  codec?: string;
  hdr?: string;
  audio?: string;
  languages?: string;
  subtitles?: string;
  added?: string;
}

export interface ActionResult {
  ok: boolean;
  message: string;
  count?: number;
}

export type EpisodeAction = "delete" | "monitor" | "unmonitor" | "search";

export interface SeasonDetail {
  number: number;
  monitored: boolean;
  episodes: EpisodeDetail[];
}

export interface SeriesDetail extends LibraryItem {
  inLibrary: boolean;
  overview?: string;
  fanart?: string;
  genres?: string[];
  runtime?: number;
  seasons: SeasonDetail[];
  synthetic?: boolean;
  path?: string;
  qualityProfileId?: number;
}

export interface CollectionMovie {
  tmdbId: number;
  imdbId?: string;
  title: string;
  year?: number;
  poster?: string;
  runtime?: number;
  inLibrary: boolean;
  libraryId?: number;
  hasFile: boolean;
}

export interface MovieDetail extends LibraryItem {
  inLibrary: boolean;
  hasFile: boolean;
  queued?: boolean;
  overview?: string;
  fanart?: string;
  genres?: string[];
  runtime?: number;
  collection?: { tmdbId: number; title: string; movies: CollectionMovie[] };
  file?: FileInfo;
  path?: string;
  qualityProfileId?: number;
}

export interface DiscoverItem {
  kind: Kind;
  title: string;
  year?: number;
  poster?: string;
  overview?: string;
  tmdbId?: number;
  tvdbId?: number;
  imdbId?: string;
  rating?: number;
  inLibrary: boolean;
  libraryId?: number;
  requested?: boolean;
}

export interface ReleaseInfo {
  title: string;
  year?: number;
  seasons?: number[];
  episodes?: number[];
  absolute?: number[];
  fullSeason?: boolean;
  complete?: boolean;
  resolution?: number;
  source?: string;
  codec?: string;
  hdr?: string;
  audio?: string;
  channels?: string;
  tenBit?: boolean;
  service?: string;
  group?: string;
  languages?: string[];
  multi?: boolean;
  dualAudio?: boolean;
  proper?: boolean;
  repack?: boolean;
  remux?: boolean;
}

export interface Release {
  key: string;
  title: string;
  indexer: string;
  indexerId: number;
  indexers: string[];
  size: number;
  seeders: number;
  leechers: number;
  ageHours: number;
  publishDate: string;
  protocol: string;
  guid: string;
  downloadUrl?: string;
  magnetUrl?: string;
  infoUrl?: string;
  infoHash?: string;
  info: ReleaseInfo;
  kind: string;
  covers?: number[];
  movieTmdb?: number;
  match: boolean;
  score: number;
  rejected: boolean;
  reasons?: string[];
  notes?: string[];
}

export interface IndexerPlan {
  id: number;
  name: string;
  queries: number;
  disabled?: string;
}

export interface Batch {
  indexerId: number;
  indexer: string;
  label: string;
  ms: number;
  cached: boolean;
  error?: string;
}

export interface SearchEvent {
  type: "plan" | "batch" | "done" | "error";
  indexers?: IndexerPlan[];
  batch?: Batch;
  releases?: Release[];
  error?: string;
  ms?: number;
  total?: number;
}

export interface GrabResult {
  ok: boolean;
  via?: string;
  message: string;
  rejections?: string[];
  canForce?: boolean;
  seriesId?: number;
  movieId?: number;
  hash?: string;
  watchFirst?: boolean;
}

export interface WatchFile {
  index: number;
  name: string;
  season: number;
  episode: number;
  size: number;
  progress: number;
  priority: number;
  skipped?: boolean;
  imported?: boolean;
  error?: string;
}

export interface WatchJob {
  hash: string;
  title: string;
  seriesTitle: string;
  seriesId: number;
  state: string;
  message?: string;
  files: WatchFile[];
  progress: number;
  speed: number;
}

export interface ActivityItem {
  id: string;
  source: string;
  kind: string;
  title: string;
  subtitle: string;
  release: string;
  poster?: string;
  href?: string;
  status: string;
  progress: number;
  size: number;
  sizeLeft: number;
  speed?: number;
  eta?: string;
  messages?: string[];
  downloadId?: string;
  watch?: WatchJob;
}

export interface RequestItem {
  id: number;
  status: string;
  kind: Kind;
  title: string;
  year?: number;
  poster?: string;
  tmdbId: number;
  tvdbId?: number;
  seasons?: number[];
  requestedBy: string;
  createdAt: string;
  available: string;
  inLibrary: boolean;
  libraryId?: number;
}

export interface ServiceStatus {
  configured: boolean;
  ok: boolean;
  version?: string;
  error?: string;
  url?: string;
  ms?: number;
}

export interface Prefs {
  resolutions: number[];
  videoCodecs: string[] | null;
  audioCodecs: string[] | null;
  size: "smaller" | "balanced" | "ignore" | "larger";
  allowRemux: boolean;
  allowHdr: boolean;
  maxGbPerHour: number;
  minSeeders: number;
  language: string;
  preferDualAudio: boolean;
  preferredGroups: string[] | null;
  blockedTerms: string[] | null;
}

export interface ServiceConfig {
  url: string;
  apiKey?: string;
  username?: string;
  password?: string;
}

export interface Settings {
  sonarr: ServiceConfig;
  radarr: ServiceConfig;
  prowlarr: ServiceConfig;
  seerr: ServiceConfig;
  jellyfin: ServiceConfig;
  qbittorrent: ServiceConfig;
  authMode: string;
  authPassword?: string;
  jellyfinAdminOnly: boolean;
  prefs: Prefs;
  behaviour: {
    watchFirst: boolean;
    prefetch: boolean;
    indexerTimeout: number;
    cacheMinutes: number;
    seerrAutoFetch: boolean;
  };
}

export interface Plan {
  picks: { key: string; fills: number[]; extras: number }[] | null;
  missing: number[] | null;
}

export interface AutoResult {
  grabs: { release: string; fills: number; result: GrabResult; error?: string }[] | null;
  missing?: number[];
  waitedMs: number;
  message: string;
}

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
  }
}

let onUnauthorized: () => void = () => {};
export function setUnauthorizedHandler(f: () => void) {
  onUnauthorized = f;
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: { "X-Roarr": "1", ...(body !== undefined ? { "Content-Type": "application/json" } : {}) },
    body: body !== undefined ? JSON.stringify(body) : undefined,
    credentials: "same-origin",
  });
  if (res.status === 401 && !path.startsWith("/api/login")) onUnauthorized();
  if (!res.ok) {
    let msg = res.statusText;
    try {
      const j = await res.json();
      if (j.error) msg = j.error;
    } catch {
      /* not json: a proxy error page */
      if (res.status === 502 || res.status === 503 || res.status === 504 || res.status >= 520) {
        msg = `The proxy in front of roarr got no answer (HTTP ${res.status}). The request may still be running: check Activity before trying again.`;
      }
    }
    throw new ApiError(res.status, msg);
  }
  return res.json() as Promise<T>;
}

export const api = {
  me: () => call<{ authMode: string; authenticated: boolean; user?: string; version: string }>("GET", "/api/me"),
  login: (username: string, password: string) => call<{ ok: boolean }>("POST", "/api/login", { username, password }),
  logout: () => call<{ ok: boolean }>("POST", "/api/logout", {}),
  status: (fresh = false) => call<{ services: Record<string, ServiceStatus>; version: string }>("GET", `/api/status${fresh ? "?fresh=1" : ""}`),
  library: (refresh = false) => call<{ items: LibraryItem[]; errors: Record<string, string> }>("GET", `/api/library${refresh ? "?refresh=1" : ""}`),
  discover: (q: string) => call<{ results: DiscoverItem[] }>("GET", `/api/discover?q=${encodeURIComponent(q)}`),
  series: (id: number, fresh = false) => call<SeriesDetail>("GET", `/api/tv/${id}${fresh ? "?fresh=1" : ""}`),
  seriesByTvdb: (tvdb: number, tmdb?: number) => call<SeriesDetail>("GET", `/api/tv/tvdb/${tvdb}${tmdb ? `?tmdb=${tmdb}` : ""}`),
  movie: (id: number) => call<MovieDetail>("GET", `/api/movie/${id}`),
  movieByTmdb: (tmdb: number) => call<MovieDetail>("GET", `/api/movie/tmdb/${tmdb}`),
  plan: (wanted: number[], options: { key: string; score: number; rejected: boolean; covers: number[] }[]) =>
    call<Plan>("POST", "/api/plan", { wanted, options }),
  grab: (body: {
    kind: Kind;
    seriesId?: number;
    tvdbId?: number;
    movieId?: number;
    tmdbId?: number;
    episodeIds?: number[];
    release: Release;
    force?: boolean;
    watchFirst?: boolean;
  }) => call<GrabResult>("POST", "/api/grab", body),
  auto: (body: { kind: Kind; seriesId?: number; tvdbId?: number; tmdbId?: number; episodeIds?: number[]; seasons?: number[]; movies?: number[] }) =>
    call<AutoResult>("POST", "/api/auto", body),
  activity: () => call<{ items: ActivityItem[]; errors: Record<string, string> }>("GET", "/api/activity"),
  removeActivity: (id: string, blocklist: boolean) =>
    call<{ ok: boolean }>("DELETE", `/api/activity/${encodeURIComponent(id)}${blocklist ? "?blocklist=1" : ""}`),
  requests: (filter: string) => call<{ items: RequestItem[] }>("GET", `/api/requests?filter=${filter}`),
  requestAction: (id: number, action: "approve" | "decline") => call<{ ok: boolean }>("POST", `/api/requests/${id}/${action}`, {}),
  episodes: (seriesId: number, action: EpisodeAction, episodeIds: number[]) =>
    call<ActionResult>("POST", `/api/tv/${seriesId}/episodes`, { action, episodeIds }),
  patchTitle: (kind: Kind, id: number, patch: { monitored?: boolean; qualityProfileId?: number; season?: number }) =>
    call<{ ok: boolean }>("PATCH", `/api/${kind}/${id}`, patch),
  removeTitle: (kind: Kind, id: number, deleteFiles: boolean) =>
    call<{ ok: boolean }>("DELETE", `/api/${kind}/${id}${deleteFiles ? "?deleteFiles=1" : ""}`),
  titleCommand: (kind: Kind, id: number, name: "refresh" | "search") => call<ActionResult>("POST", `/api/${kind}/${id}/command/${name}`, {}),
  deleteMovieFile: (id: number) => call<ActionResult>("DELETE", `/api/movie/${id}/file`),
  profiles: (kind: Kind) => call<{ profiles: { id: number; name: string }[] }>("GET", `/api/profiles/${kind}`),
  settings: () => call<{ settings: Settings; locked: Record<string, boolean> }>("GET", "/api/settings"),
  saveSettings: (s: Settings) => call<{ settings: Settings; locked: Record<string, boolean> }>("PUT", "/api/settings", s),
};

/** Opens a streaming search. Returns a function that cancels it. */
export function streamSearch(params: Record<string, string>, onEvent: (e: SearchEvent) => void): () => void {
  const qs = new URLSearchParams(params).toString();
  const es = new EventSource(`/api/search?${qs}`);
  let closed = false;
  es.onmessage = (m) => {
    const ev = JSON.parse(m.data) as SearchEvent;
    onEvent(ev);
    if (ev.type === "done" || ev.type === "error") {
      closed = true;
      es.close();
    }
  };
  es.onerror = () => {
    if (!closed) onEvent({ type: "error", error: "search connection lost" });
    closed = true;
    es.close();
  };
  return () => {
    closed = true;
    es.close();
  };
}
