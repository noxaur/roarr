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
  /** Explicitly tagged audio languages. */
  languages?: string[];
  multi?: boolean;
  dualAudio?: boolean;
  /** Explicitly tagged subtitle languages ("Multi" for multi-sub). */
  subtitles?: string[];
  subbed?: boolean;
  hardSubbed?: boolean;
  special?: boolean;
  proper?: boolean;
  repack?: boolean;
  remux?: boolean;
  extended?: boolean;
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
  transcode: TranscodeSettings;
  verify: VerifySettings;
  import: ImportSettings;
}

/** AV1 conversion of imported files (config.Transcode). */
export interface TranscodeSettings {
  enabled: boolean;
  concurrency: number;
  nice: number;
  /** 0 = automatic, per resolution. */
  crf: number;
  /** SVT-AV1 speed, 1 slowest to 13 fastest. */
  preset: number;
  filmGrain: number;
  minSavings: number;
  pathMap?: string;
  /** Weekly windows in which conversion may run; empty or absent = any time. */
  schedule?: ScheduleWindow[];
}

/** A weekly span of server time (config.Window). */
export interface ScheduleWindow {
  /** Days the window starts on, 0 = Sunday to 6 = Saturday; empty = every day. */
  days?: number[];
  /** "HH:MM". A `to` at or before `from` ends the next day; "24:00" is the end of the day. */
  from: string;
  to: string;
}

/** Checks on finished downloads (config.Verify). */
export interface VerifySettings {
  /** Unset (absent or null) means on when ffprobe is installed. */
  enabled?: boolean | null;
  autoRegrab: boolean;
  pathMap?: string;
}

/** Who downloads and imports grabs (config.Import). */
export interface ImportSettings {
  mode?: "arr" | "native" | string;
  tvRoot?: string;
  movieRoot?: string;
  transfer?: "hardlink" | "copy" | "move" | string;
  pathMap?: string;
  category?: string;
  savePath?: string;
}

export type VerifyLevel = "pass" | "warn" | "fail" | "skip";

export interface VerifyCheck {
  /** payload | streams | duration | picture | resolution | codec | hdr | bitdepth | audio | subtitles */
  name: string;
  level: VerifyLevel;
  reason: string;
}

export interface Verdict {
  level: VerifyLevel;
  summary: string;
  file?: string;
  checks: VerifyCheck[] | null;
  media?: {
    container?: string;
    duration: number;
    width?: number;
    height?: number;
    videoCodec?: string;
    audio?: string[];
    subtitles?: string[];
  };
  at: string;
  tookMs: number;
}

/** One download followed by the verifier (app.VerifyRecord). */
export interface VerifyRecord {
  hash: string;
  source: string;
  kind: Kind;
  seriesId?: number;
  movieId?: number;
  episodeIds?: number[];
  title: string;
  release: string;
  added: string;
  state: "pending" | "checking" | "done";
  verdict?: Verdict;
  dismissed?: boolean;
  rejected?: boolean;
  regrab?: { at: string; auto: boolean; message: string; note?: string; error?: string; result?: AutoResult };
}

export interface VerifyList {
  enabled: boolean;
  items: VerifyRecord[];
  tools?: { ffprobe: string; ffmpeg: string };
  autoRegrab?: boolean;
}

export type TranscodeState = "queued" | "running" | "done" | "skipped" | "failed";

export interface TranscodeJob {
  id: string;
  path: string;
  title?: string;
  kind?: string;
  titleId?: number;
  state: TranscodeState;
  percent: number;
  speed?: string;
  srcBytes?: number;
  outBytes?: number;
  savedBytes?: number;
  crf?: number;
  encoder?: string;
  message?: string;
  /** Running, but held in place by a pause or the schedule. */
  paused?: boolean;
  queuedAt: string;
  startedAt?: string;
  finishedAt?: string;
}

export interface TranscodeStatus {
  enabled: boolean;
  available: boolean;
  encoder?: string;
  error?: string;
  concurrency: number;
  /** Held by hand. */
  paused?: boolean;
  /** Nothing may run now: paused, or outside the schedule. */
  held?: boolean;
  scheduled?: boolean;
  /** When the schedule next opens or closes. */
  nextChange?: string;
  /** The server's time zone, in which the schedule is read; offset in seconds east of UTC. */
  zone?: string;
  zoneOffset?: number;
  counts: Record<TranscodeState, number>;
  bytesSaved: number;
  jobs: TranscodeJob[];
}

/** A torrent roarr downloads and imports itself (app.NativeJob). */
export interface NativeJob {
  hash: string;
  release: string;
  kind: Kind;
  titleKey: string;
  titleName: string;
  href?: string;
  created: string;
  completedAt?: string;
  state: "waiting" | "downloading" | "importing" | "done" | "failed" | string;
  message?: string;
  progress: number;
  size: number;
  files: unknown[] | null;
  skipped?: string[];
}

export interface ImportsStatus {
  mode: string;
  tvRoot: string;
  movieRoot: string;
  transfer: string;
  pathMap: string;
  category: string;
  jobs: NativeJob[];
  files: { titleKey: string; path: string; size: number; release?: string; importedAt: string }[];
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
  verify: () => call<VerifyList>("GET", "/api/verify"),
  verifyRecheck: (hash: string) => call<VerifyRecord>("POST", `/api/verify/${encodeURIComponent(hash)}/recheck`, {}),
  verifyRegrab: (hash: string) => call<VerifyRecord>("POST", `/api/verify/${encodeURIComponent(hash)}/regrab`, {}),
  verifyDismiss: (hash: string) => call<VerifyRecord>("POST", `/api/verify/${encodeURIComponent(hash)}/dismiss`, {}),
  transcode: () => call<TranscodeStatus>("GET", "/api/transcode"),
  transcodePause: (paused: boolean) => call<TranscodeStatus>("POST", `/api/transcode/${paused ? "pause" : "resume"}`, {}),
  imports: () => call<ImportsStatus>("GET", "/api/imports"),
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
