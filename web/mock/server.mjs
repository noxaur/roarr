// A stand-in for roarr's API so the UI can be developed and screenshotted
// without a *arr stack:  node mock/server.mjs  then  ROARR_API=http://localhost:3999 npm run dev
import { createServer } from "node:http";

const PORT = +(process.env.PORT || 3999);

const shows = [
  { id: 1, title: "Severance", year: 2022, network: "Apple TV+", status: "continuing", seasons: [10, 10], have: [10, 6], queued: [[], [7]] },
  { id: 2, title: "The Expanse", year: 2015, network: "Amazon", status: "ended", seasons: [10, 13, 13, 13, 10, 6], have: [10, 13, 13, 13, 10, 6], queued: [] },
  { id: 3, title: "Sousou no Frieren", year: 2023, network: "NTV", status: "continuing", anime: true, seasons: [28], have: [20], queued: [[21]] },
  { id: 4, title: "Pluribus", year: 2025, network: "Apple TV+", status: "continuing", seasons: [9], have: [4], queued: [[]] },
  { id: 5, title: "Slow Horses", year: 2022, network: "Apple TV+", status: "continuing", seasons: [6, 6, 6, 6, 6], have: [6, 6, 6, 0, 0], queued: [], monitored: false },
];
const movies = [
  { id: 11, title: "Dune: Part Two", year: 2024, hasFile: true, size: 14.2e9, tmdbId: 693134 },
  { id: 12, title: "The French Dispatch", year: 2021, hasFile: false, tmdbId: 542178 },
  { id: 13, title: "Blade Runner 2049", year: 2017, hasFile: true, size: 9.8e9, tmdbId: 335984 },
  { id: 14, title: "Perfect Days", year: 2023, hasFile: false, tmdbId: 976893, queued: true },
];

let epId = 1000;
const series = shows.map((s) => {
  const seasons = s.seasons.map((count, si) => ({
    number: si + 1,
    monitored: true,
    episodes: Array.from({ length: count }, (_, i) => {
      const n = i + 1;
      const hasFile = n <= s.have[si];
      const queued = (s.queued[si] ?? []).includes(n);
      const aired = !(si === s.seasons.length - 1 && n > count - 2 && s.status === "continuing");
      return {
        id: epId++,
        season: si + 1,
        number: n,
        absolute: s.anime ? n : undefined,
        title: `Episode ${n}`,
        airDate: `2025-0${1 + (si % 8)}-${String(1 + ((n * 7) % 27)).padStart(2, "0")}`,
        aired,
        hasFile,
        monitored: s.monitored !== false,
        queued,
        runtime: 48,
        file: hasFile
          ? { id: epId, path: `/tv/${s.title}/Season ${String(si + 1).padStart(2, "0")}/${s.title} - S${String(si + 1).padStart(2, "0")}E${String(n).padStart(2, "0")}.mkv`, size: 1.3e9 + n * 2e7, quality: "WEBDL-1080p", resolution: 1080, group: "NTb", codec: "x265", audio: "EAC3", languages: "English" }
          : undefined,
      };
    }),
  }));
  const all = seasons.flatMap((x) => x.episodes).filter((e) => e.aired);
  return {
    key: `tv:${s.id}`,
    kind: "tv",
    id: s.id,
    title: s.title,
    year: s.year,
    network: s.network,
    status: s.status,
    monitored: s.monitored !== false,
    anime: s.anime,
    have: all.filter((e) => e.hasFile).length,
    total: all.length,
    sizeOnDisk: all.filter((e) => e.hasFile).reduce((n, e) => n + e.file.size, 0),
    tvdbId: 70000 + s.id,
    inLibrary: true,
    runtime: 48,
    seasons,
    path: `/tv/${s.title}`,
    qualityProfileId: 1,
  };
});
const movieDetails = movies.map((m) => ({
  key: `movie:${m.id}`,
  kind: "movie",
  id: m.id,
  title: m.title,
  year: m.year,
  monitored: true,
  have: m.hasFile ? 1 : 0,
  total: 1,
  sizeOnDisk: m.size ?? 0,
  tmdbId: m.tmdbId,
  inLibrary: true,
  hasFile: m.hasFile,
  queued: m.queued,
  runtime: 118,
  file: m.hasFile ? { id: m.id, path: `/movies/${m.title} (${m.year})/${m.title}.mkv`, size: m.size, quality: "Bluray-2160p", resolution: 2160, group: "FraMeSToR", codec: "x265", hdr: "DV", audio: "TrueHD Atmos", languages: "English", subtitles: "English, French" } : undefined,
  path: `/movies/${m.title} (${m.year})`,
  qualityProfileId: 1,
}));

const library = { items: [...series, ...movieDetails].map(({ seasons: _s, file: _f, ...rest }) => rest), errors: {} };

const activity = {
  items: [
    { id: "a1", source: "sonarr", kind: "tv", title: "Severance", subtitle: "S02E07", downloadId: "c0ffee07", release: "Severance.S02E07.1080p.ATVP.WEB-DL.DDP5.1.Atmos.H.264-FLUX", href: "/tv/1", status: "downloading", progress: 0.62, size: 2.9e9, sizeLeft: 1.1e9, speed: 11.2e6, eta: "2m" },
    { id: "a2", source: "sonarr", kind: "tv", title: "Sousou no Frieren", subtitle: "S01E21", downloadId: "f00d0021", release: "[SubsPlease] Sousou no Frieren - 21 (1080p) [A1B2C3D4].mkv", href: "/tv/3", status: "queued", progress: 0, size: 0, sizeLeft: 0 },
    { id: "a3", source: "radarr", kind: "movie", title: "Perfect Days", subtitle: "2023", downloadId: "da150014", release: "Perfect.Days.2023.1080p.BluRay.x264-GROUP", href: "/movie/14", status: "warning", progress: 0.1, size: 8e9, sizeLeft: 7.2e9, messages: ["No seeders for 2 days. qBittorrent reports the torrent as stalled."] },
    { id: "a4", source: "sonarr", kind: "tv", title: "Pluribus", subtitle: "S01E05", downloadId: "b1005005", release: "Pluribus.S01E05.720p.HDTV.x264-SYNCOPY", href: "/tv/4", status: "failed", progress: 1, size: 1.1e9, sizeLeft: 0, messages: ["Import failed: sample file detected, no video file found in the download."] },
  ],
  errors: {},
};

const settings = {
  settings: {
    sonarr: { url: "http://sonarr:8989", apiKey: "xxx" },
    radarr: { url: "http://radarr:7878", apiKey: "xxx" },
    prowlarr: { url: "http://prowlarr:9696", apiKey: "xxx" },
    seerr: { url: "", apiKey: "" },
    jellyfin: { url: "", apiKey: "" },
    qbittorrent: { url: "http://qbit:8080", username: "admin", password: "x" },
    authMode: "none",
    jellyfinAdminOnly: true,
    prefs: { resolutions: [1080, 2160, 720], videoCodecs: ["av1", "x265", "x264"], audioCodecs: ["Opus", "DDP", "AAC"], size: "balanced", allowRemux: false, allowHdr: true, maxGbPerHour: 12, minSeeders: 1, language: "English", preferDualAudio: false, preferredGroups: null, blockedTerms: null },
    behaviour: { watchFirst: true, prefetch: true, indexerTimeout: 30, cacheMinutes: 20, seerrAutoFetch: false },
    transcode: { enabled: true, concurrency: 1, nice: 10, crf: 0, preset: 8, filmGrain: 0, minSavings: 15, pathMap: "/tv=/media/tv,/movies=/media/movies", schedule: [{ days: [1, 2, 3, 4, 5], from: "22:00", to: "07:00" }, { days: [6, 0], from: "00:00", to: "24:00" }] },
    verify: { autoRegrab: false, pathMap: "/downloads=/data/downloads" },
    import: { mode: "arr", transfer: "hardlink", category: "roarr" },
  },
  locked: { PROWLARR_URL: true, PROWLARR_API_KEY: true, TRANSCODE_NICE: true },
};

// ---- Download checks --------------------------------------------------------

const epOf = (showId, season, n) => series.find((x) => x.id === showId).seasons[season - 1].episodes[n - 1].id;
const ago = (min) => new Date(Date.now() - min * 60e3).toISOString();
const verdict = (level, summary, checks, extra = {}) => ({ level, summary, checks, at: ago(3), tookMs: 4200, ...extra });
const pass = (name, reason) => ({ name, level: "pass", reason });

const verifyRecs = [
  {
    hash: "c0ffee06", source: "sonarr", kind: "tv", seriesId: 1, episodeIds: [epOf(1, 2, 6)], title: "Severance",
    release: "Severance.S02E06.1080p.WEB.H264-SUCCESSORS", added: ago(90), state: "done",
    verdict: verdict("fail", "frames are black or one static image", [
      pass("payload", "one video file, 1.4 GB"),
      { name: "picture", level: "fail", reason: "frames are black or one static image (9 of 9 samples)" },
      { name: "duration", level: "warn", reason: "runs 12 min, the episode runs 48 min" },
      pass("resolution", "1920x1080 as claimed"),
    ]),
  },
  {
    hash: "c0ffee07", source: "sonarr", kind: "tv", seriesId: 1, episodeIds: [epOf(1, 2, 7)], title: "Severance",
    release: "Severance.S02E07.1080p.ATVP.WEB-DL.DDP5.1.Atmos.H.264-FLUX", added: ago(20), state: "done",
    verdict: verdict("warn", "claims Atmos, the audio is plain EAC3 5.1", [
      pass("payload", "one video file"),
      pass("picture", "real, changing frames"),
      { name: "audio", level: "warn", reason: "claims Atmos, the audio is plain EAC3 5.1" },
    ]),
  },
  {
    hash: "f00d0021", source: "sonarr", kind: "tv", seriesId: 3, episodeIds: [epOf(3, 1, 21)], title: "Sousou no Frieren",
    release: "[SubsPlease] Sousou no Frieren - 21 (1080p) [A1B2C3D4].mkv", added: ago(5), state: "pending",
  },
  {
    hash: "b1ade213", source: "radarr", kind: "movie", movieId: 13, title: "Blade Runner 2049",
    release: "Blade.Runner.2049.2017.2160p.UHD.BluRay.x265.HDR.TrueHD.Atmos-FraMeSToR", added: ago(60 * 30), state: "done",
    verdict: verdict("warn", "no English subtitles, the release claims them", [
      pass("payload", "one video file, 9.8 GB"),
      pass("resolution", "3840x1600 as claimed"),
      { name: "subtitles", level: "warn", reason: "no English subtitles, the release claims them" },
      { name: "hdr", level: "warn", reason: "claims HDR, no HDR metadata found" },
    ]),
  },
  {
    hash: "e7a50103", source: "sonarr", kind: "tv", seriesId: 2, episodeIds: [epOf(2, 1, 3)], title: "The Expanse",
    release: "The.Expanse.S01E03.720p.HDTV.x264-KILLERS", added: ago(60 * 48), state: "done", dismissed: true,
    verdict: verdict("fail", "claims 1080p, the file is 1280x720", [{ name: "resolution", level: "fail", reason: "claims 1080p, the file is 1280x720" }]),
  },
  {
    hash: "d00e0011", source: "radarr", kind: "movie", movieId: 11, title: "Dune: Part Two",
    release: "Dune.Part.Two.2024.2160p.WEB-DL.DDP5.1.Atmos.DV.HDR.H.265-FLUX", added: ago(60 * 6), state: "done", rejected: true,
    verdict: verdict("fail", "an executable is in the download", [{ name: "payload", level: "fail", reason: "an executable is in the download: Dune.Part.Two.2024.exe" }]),
    regrab: { at: ago(60 * 5), auto: true, message: "", error: "no release left that passes your rules" },
  },
];
const findRec = (h) => verifyRecs.find((r) => r.hash === h.toLowerCase());

// ---- AV1 conversion -----------------------------------------------------------

const t0 = Date.now();
let transcodePaused = false;
const epPath = (showId, season, n) => series.find((x) => x.id === showId).seasons[season - 1].episodes[n - 1].file.path;
function transcodeStatus() {
  const running = Math.min(99, ((Date.now() - t0) / 1000) % 100);
  const jobs = [
    { id: "t1", path: epPath(1, 1, 3), title: "Severance S01E03", kind: "tv", titleId: 1, state: "running", paused: transcodePaused, percent: running, speed: "1.8x", srcBytes: 1.36e9, crf: 30, encoder: "libsvtav1", queuedAt: ago(14), startedAt: ago(9) },
    { id: "t2", path: epPath(1, 1, 4), title: "Severance S01E04", kind: "tv", titleId: 1, state: "queued", percent: 0, queuedAt: ago(14) },
    { id: "t3", path: movieDetails[2].file.path, title: "Blade Runner 2049", kind: "movie", titleId: 13, state: "queued", percent: 0, queuedAt: ago(10) },
    { id: "t4", path: epPath(2, 1, 1), title: "The Expanse S01E01", kind: "tv", titleId: 2, state: "done", percent: 100, srcBytes: 1.32e9, outBytes: 0.61e9, savedBytes: 0.71e9, crf: 30, encoder: "libsvtav1", queuedAt: ago(200), finishedAt: ago(150) },
    { id: "t5", path: epPath(2, 1, 2), title: "The Expanse S01E02", kind: "tv", titleId: 2, state: "skipped", percent: 100, srcBytes: 1.34e9, message: "saved 6%, less than 15%", queuedAt: ago(200), finishedAt: ago(120) },
    { id: "t6", path: epPath(5, 1, 1), title: "Slow Horses S01E01", kind: "tv", titleId: 5, state: "failed", percent: 41, message: "ffmpeg exited 1: Invalid data found when processing input", queuedAt: ago(300), finishedAt: ago(280) },
  ];
  const counts = { queued: 0, running: 0, done: 0, skipped: 0, failed: 0 };
  for (const j of jobs) counts[j.state]++;
  const scheduled = (settings.settings.transcode.schedule ?? []).length > 0;
  const nextChange = scheduled ? new Date(Math.ceil(Date.now() / 3.6e6) * 3.6e6 + 2 * 3.6e6).toISOString() : undefined;
  return {
    enabled: settings.settings.transcode.enabled,
    available: true,
    encoder: "libsvtav1",
    concurrency: settings.settings.transcode.concurrency,
    paused: transcodePaused,
    held: transcodePaused,
    scheduled,
    nextChange,
    zone: "CET",
    zoneOffset: 3600,
    counts,
    bytesSaved: 0.71e9 + 41.2e9,
    jobs,
  };
}

// ---- Native import --------------------------------------------------------------

function importsStatus() {
  const im = settings.settings.import;
  return {
    mode: im.mode || "arr", tvRoot: im.tvRoot || "", movieRoot: im.movieRoot || "", transfer: im.transfer || "hardlink", pathMap: im.pathMap || "", category: im.category || "roarr",
    jobs: [
      { hash: "aa01", release: "Severance.S02E07.1080p.ATVP.WEB-DL.DDP5.1.Atmos.H.264-FLUX", kind: "tv", titleKey: "tv:tvdb:70001", titleName: "Severance", href: "/tv/1", created: ago(20), state: "downloading", progress: 0.62, size: 2.9e9, files: [] },
      { hash: "aa02", release: "Pluribus.S01E04.1080p.WEB.H264-SYNCOPY", kind: "tv", titleKey: "tv:tvdb:70004", titleName: "Pluribus", href: "/tv/4", created: ago(200), state: "done", progress: 1, size: 1.2e9, files: [] },
      { hash: "aa03", release: "Perfect.Days.2023.1080p.BluRay.x264-GROUP", kind: "movie", titleKey: "movie:tmdb:976893", titleName: "Perfect Days", created: ago(400), state: "failed", message: "no file matches the movie: 3 samples skipped", progress: 1, size: 8e9, files: [] },
    ],
    files: [{ titleKey: "tv:tvdb:70004", path: "/media/tv/Pluribus/Season 01/Pluribus - S01E04.mkv", size: 1.2e9, importedAt: ago(190) }],
  };
}

const readBody = (req) =>
  new Promise((resolve) => {
    let body = "";
    req.on("data", (c) => (body += c));
    req.on("end", () => resolve(body ? JSON.parse(body) : {}));
  });

// Release titles that exercise the parser's language and subtitle fields.
const TITLES = [
  ["Severance.S02E07.1080p.ATVP.WEB-DL.DDP5.1.Atmos.H.264-FLUX", 2.9e9, 412, 20, "AlphaRatio"],
  ["Severance.S02E07.2160p.ATVP.WEB-DL.DV.HDR10.DDP5.1.Atmos.H.265-FLUX", 6.1e9, 188, 20, "AlphaRatio"],
  ["Severance.S02E07.720p.WEB.h264-ETHEL", 0.9e9, 940, 21, "1337x"],
  ["Severance.S02E07.MULTi.VFF.1080p.WEB.H264-FW", 2.2e9, 61, 18, "YGG"],
  ["Severance.S02E07.FRENCH.1080p.WEB.H264-FW", 2.1e9, 44, 18, "YGG"],
  ["Severance.S02E07.VOSTFR.1080p.WEB.H264-FW", 2.0e9, 30, 18, "YGG"],
  ["Severance.S02E07.GERMAN.DL.1080p.WEB.x264-WvF", 2.4e9, 25, 16, "TorrentLeech"],
  ["Severance.S02E07.1080p.WEB.H264.ENGSUB.KORSUB-HC", 1.8e9, 12, 10, "1337x"],
  ["Severance.S02.1080p.ATVP.WEB-DL.DDP5.1.Atmos.H.264-FLUX", 28e9, 120, 3, "AlphaRatio"],
  ["Severance.S02.2160p.ATVP.WEB-DL.DV.HDR.DDP5.1.Atmos.H.265-FLUX", 58e9, 55, 3, "TorrentLeech"],
  ["Severance.S01-S02.COMPLETE.1080p.WEB-DL.x265.10bit.Multi-Subs-QxR", 24e9, 212, 1, "1337x"],
  ["Severance.S02E07.1080p.HDTV.x264-SYNCOPY", 1.4e9, 3, 22, "1337x"],
  ["Severance.S02E07.2160p.WEB-DL.REMUX.TrueHD.Atmos.7.1-NOGRP", 19e9, 9, 19, "TorrentLeech"],
  ["Severance.S02E07.HDCAM.x264-BAD", 0.7e9, 2, 24, "1337x"],
  ["Severance.S02E07.1080p.WEB.AV1.Opus-SAMPA", 0.8e9, 77, 14, "1337x"],
];

function parseInfo(t) {
  const res = /2160p/.test(t) ? 2160 : /1080p/.test(t) ? 1080 : /720p/.test(t) ? 720 : 0;
  const source = /REMUX/i.test(t) ? "Remux" : /WEB-DL/i.test(t) ? "WEB-DL" : /WEB/i.test(t) ? "WEB" : /HDTV/i.test(t) ? "HDTV" : /HDCAM/i.test(t) ? "CAM" : "";
  const codec = /x265|H\.265|HEVC/i.test(t) ? "x265" : /AV1/i.test(t) ? "av1" : /x264|H\.264|h264/i.test(t) ? "x264" : "";
  const hdr = /\bDV\b/.test(t) ? "DV" : /HDR10/.test(t) ? "HDR10" : /HDR/.test(t) ? "HDR" : "";
  const audio = /Atmos/i.test(t) ? "Atmos" : /TrueHD/i.test(t) ? "TrueHD" : /DDP/i.test(t) ? "DDP" : /Opus/i.test(t) ? "Opus" : /AAC/i.test(t) ? "AAC" : "";
  const channels = (t.match(/([1-9])\.([01])/) || [])[0];
  const languages = [];
  if (/FRENCH|VFF/.test(t)) languages.push("French");
  if (/GERMAN/.test(t)) languages.push("German");
  const multi = /MULTi/i.test(t);
  const subtitles = [];
  if (/VOSTFR/.test(t)) subtitles.push("French");
  if (/ENGSUB/.test(t)) subtitles.push("English");
  if (/KORSUB/.test(t)) subtitles.push("Korean");
  if (/Multi-Subs/i.test(t)) subtitles.push("Multi");
  const sm = t.match(/S(\d{2})(?:E(\d{2}))?/);
  const seasons = sm ? [+sm[1]] : [];
  if (/S01-S02/.test(t)) seasons.push(1, 2);
  const episodes = sm && sm[2] ? [+sm[2]] : [];
  const group = (t.match(/-([A-Za-z0-9]+)$/) || [])[1];
  return { title: "Severance", seasons: [...new Set(seasons)], episodes, fullSeason: !episodes.length, resolution: res, source, codec, hdr, audio, channels, tenBit: /10bit/.test(t), group, languages, multi, dualAudio: /\bDL\b/.test(t), subtitles, subbed: subtitles.length > 0, hardSubbed: /-HC\b|KORSUB/.test(t), remux: /REMUX/i.test(t) };
}

function releasesFor(target) {
  const s = series.find((x) => x.id === target) ?? series[0];
  const all = s.seasons.flatMap((x) => x.episodes);
  const wanted = all.find((e) => e.aired && !e.hasFile && !e.queued) ?? all.find((e) => e.queued) ?? all[0];
  const season2 = s.seasons[1]?.episodes.map((e) => e.id) ?? [];
  return TITLES.map(([title, size, seeders, ageH, indexer], i) => {
    const info = parseInfo(title);
    let score = 0;
    const reasons = [];
    const notes = [];
    if (info.resolution === 1080) (score += 40), notes.push("+40 1080p");
    else if (info.resolution === 2160) (score += 28), notes.push("+28 2160p");
    else if (info.resolution === 720) (score += 16), notes.push("+16 720p");
    if (info.source === "WEB-DL") (score += 17), notes.push("+17 WEB-DL");
    else if (info.source === "WEB") (score += 15), notes.push("+15 WEB");
    else if (info.source === "HDTV") (score += 6), notes.push("+6 HDTV");
    if (info.codec === "x265") (score += 8), notes.push("+8 x265");
    if (info.codec === "av1") (score += 12), notes.push("+12 av1");
    const seedPts = Math.min(20, (20 * Math.log2(1 + seeders)) / Math.log2(201));
    score += seedPts;
    notes.push(`+${seedPts.toFixed(0)} seeders`);
    if (info.hardSubbed) (score -= 20), notes.push("-20 hardsubs");
    if (info.source === "CAM") reasons.push("cam/telesync");
    if (info.source === "Remux") reasons.push("remux");
    if (info.languages.length && !info.multi) reasons.push(info.languages.join("/") + " audio");
    if (seeders < 1) reasons.push("0 seeders");
    const kind = info.seasons.length > 1 ? "multi-season" : info.fullSeason ? "season" : "episode";
    const covers = kind === "episode" ? [wanted.id] : kind === "season" ? season2 : s.seasons.flatMap((x) => x.episodes.map((e) => e.id));
    return {
      key: `r${i}`,
      title,
      indexer,
      indexerId: 1 + (i % 4),
      indexers: [indexer],
      size,
      seeders,
      leechers: Math.round(seeders / 7),
      ageHours: ageH,
      publishDate: new Date(Date.now() - ageH * 3600e3).toISOString(),
      protocol: "torrent",
      guid: `guid-${i}`,
      infoUrl: "https://example.invalid/" + i,
      info,
      kind,
      covers,
      match: true,
      score: Math.round(score * 10) / 10,
      rejected: reasons.length > 0,
      reasons: reasons.length ? reasons : undefined,
      notes,
    };
  });
}

function movieReleases(tmdb) {
  const m = movieDetails.find((x) => x.tmdbId === tmdb) ?? movieDetails[0];
  const base = m.title.replace(/[:']/g, "").replace(/\s+/g, ".");
  const list = [
    [`${base}.${m.year}.1080p.WEB-DL.DDP5.1.H.264-NTb`, 6.2e9, 300, 400, "AlphaRatio", 82],
    [`${base}.${m.year}.2160p.UHD.BluRay.x265.HDR.DTS-HD.MA.5.1-SWTYBLZ`, 22e9, 90, 380, "TorrentLeech", 70],
    [`${base}.${m.year}.FRENCH.1080p.BluRay.x264-LOST`, 7e9, 40, 300, "YGG", 0],
    [`${base}.${m.year}.1080p.BluRay.x264.MULTi.VFF.ENGLISH-FW`, 8e9, 60, 290, "YGG", 75],
    [`${base}.${m.year}.720p.WEBRip.x264.AAC-YTS.MX`, 1.1e9, 1200, 500, "1337x", 55],
  ];
  return list.map(([title, size, seeders, ageH, indexer, score], i) => {
    const info = parseInfo(title);
    info.title = m.title;
    info.year = m.year;
    info.seasons = [];
    info.episodes = [];
    info.fullSeason = false;
    return {
      key: `m${i}`,
      title,
      indexer,
      indexerId: 1 + (i % 4),
      indexers: [indexer],
      size,
      seeders,
      leechers: 3,
      ageHours: ageH,
      publishDate: new Date().toISOString(),
      protocol: "torrent",
      guid: `mg-${i}`,
      info,
      kind: "movie",
      movieTmdb: m.tmdbId,
      match: true,
      score,
      rejected: score === 0,
      reasons: score === 0 ? ["French audio"] : undefined,
      notes: ["+40 1080p", "+17 WEB-DL"],
    };
  });
}

const json = (res, body, status = 200) => {
  res.writeHead(status, { "Content-Type": "application/json" });
  res.end(JSON.stringify(body));
};

// MOCK_LATENCY=300 makes every answer wait like a real Sonarr does, to test
// how the UI feels on a slow network.
const LATENCY = +(process.env.MOCK_LATENCY || 0);

createServer((req, res) => (LATENCY ? setTimeout(() => handle(req, res), LATENCY) : handle(req, res))).listen(PORT, () =>
  console.log(`mock roarr API on http://localhost:${PORT}`),
);

function handle(req, res) {
  const u = new URL(req.url, "http://x");
  const p = u.pathname;
  let m;
  if (p === "/api/me") return json(res, { authMode: "none", authenticated: true, version: "mock" });
  if (p === "/api/library") return json(res, library);
  if (p === "/api/activity") return json(res, activity);
  if (p === "/api/settings" && req.method === "PUT") {
    // Like the Go server: the body replaces the whole object.
    readBody(req).then((next) => {
      settings.settings = next;
      json(res, settings);
    });
    return;
  }
  if (p === "/api/settings") return json(res, settings);
  if (p === "/api/verify") return json(res, { enabled: true, items: verifyRecs, tools: { ffprobe: "/usr/bin/ffprobe", ffmpeg: "/usr/bin/ffmpeg" }, autoRegrab: !!settings.settings.verify?.autoRegrab });
  if ((m = p.match(/^\/api\/verify\/([^/]+)(?:\/(recheck|regrab|dismiss))?$/))) {
    const r = findRec(m[1]);
    if (!r) return json(res, { error: "download not verified" }, 404);
    if (!m[2]) return json(res, r);
    if (m[2] === "dismiss") {
      r.dismissed = true;
      r.rejected = false;
      return json(res, r);
    }
    setTimeout(() => {
      if (m[2] === "recheck") {
        if (r.verdict) r.verdict.at = new Date().toISOString();
        r.state = "done";
        return json(res, r);
      }
      r.rejected = true;
      r.dismissed = false;
      if (r.hash === "d00e0011") {
        r.regrab = { at: new Date().toISOString(), auto: false, message: "", error: "no release left that passes your rules" };
        return json(res, { error: r.regrab.error }, 424);
      }
      r.regrab = { at: new Date().toISOString(), auto: false, message: "Grabbed 1 release for 1 episode.", note: "Deleted 1 imported file(s)." };
      json(res, r);
    }, 900);
    return;
  }
  if (p === "/api/transcode") return json(res, transcodeStatus());
  if (p === "/api/transcode/pause" || p === "/api/transcode/resume") {
    transcodePaused = p.endsWith("pause");
    return json(res, transcodeStatus());
  }
  if (p === "/api/imports") return json(res, importsStatus());
  if (p === "/api/status") return json(res, { version: "mock", services: { prowlarr: { configured: true, ok: true, version: "2.0", ms: 12 }, sonarr: { configured: true, ok: true, version: "4.0", ms: 20 }, radarr: { configured: true, ok: true, version: "6.0", ms: 18 }, seerr: { configured: false, ok: false }, jellyfin: { configured: false, ok: false }, qbittorrent: { configured: true, ok: false, error: "connection refused" } } });
  if (p === "/api/requests") return json(res, { error: "seerr not configured" }, 400);
  if (p.startsWith("/api/profiles")) return json(res, { profiles: [{ id: 1, name: "HD-1080p" }, { id: 2, name: "Ultra-HD" }] });
  if (p === "/api/discover") return json(res, { results: [{ kind: "movie", title: "Dune", year: 2021, tmdbId: 438631, inLibrary: false }, { kind: "tv", title: "Severance", year: 2022, tmdbId: 95396, tvdbId: 70001, inLibrary: true, libraryId: 1 }] });
  if ((m = p.match(/^\/api\/tv\/(\d+)$/))) return json(res, series.find((s) => s.id === +m[1]) ?? { error: "not found" });
  if ((m = p.match(/^\/api\/movie\/(\d+)$/))) return json(res, movieDetails.find((s) => s.id === +m[1]) ?? { error: "not found" });
  if (p === "/api/plan") {
    let body = "";
    req.on("data", (c) => (body += c));
    req.on("end", () => {
      const { wanted, options } = JSON.parse(body);
      const best = options.filter((o) => !o.rejected).sort((a, b) => b.score - a.score)[0];
      json(res, { picks: best ? [{ key: best.key, fills: wanted, extras: 0 }] : null, missing: best ? [] : wanted });
    });
    return;
  }
  if (p === "/api/search") {
    res.writeHead(200, { "Content-Type": "text/event-stream", "Cache-Control": "no-cache", Connection: "keep-alive" });
    const send = (o) => res.write(`data: ${JSON.stringify(o)}\n\n`);
    const kind = u.searchParams.get("kind");
    const all = kind === "movie" ? movieReleases(+u.searchParams.get("tmdb")) : releasesFor(+u.searchParams.get("id"));
    const names = ["AlphaRatio", "1337x", "YGG", "TorrentLeech"];
    send({ type: "plan", indexers: names.map((n, i) => ({ id: i + 1, name: n, queries: 1 })) });
    names.forEach((n, i) => {
      setTimeout(() => {
        send({ type: "batch", batch: { indexerId: i + 1, indexer: n, label: "q", ms: 300 * (i + 1), cached: false }, releases: all.filter((r) => r.indexer === n) });
        if (i === names.length - 1) {
          send({ type: "done", ms: 1200, total: all.length });
          res.end();
        }
      }, 250 * (i + 1));
    });
    return;
  }
  if (req.method !== "GET") return json(res, { ok: true, message: "mock: done" });
  json(res, { error: "mock: no route " + p }, 404);
}
