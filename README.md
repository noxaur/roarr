# roarr

One fast, quiet UI on top of **Sonarr, Radarr, Prowlarr, Seerr and Jellyfin**.

Search anything that exists, see what your indexers have **within half a
second**, and get exactly as much as you want: one episode, a season, a whole
show, a movie or a whole collection. Let roarr pick the best release, or pick
it yourself. Season packs download episode by episode, and each episode lands
in Jellyfin as soon as it is done.

One Docker image (~17 MB, or ~17 MB + ffmpeg if you want the optional AV1
converter), one port, one volume.

## Quick start

```yaml
# docker-compose.yml
services:
  roarr:
    image: ghcr.io/noxaur/roarr:latest
    restart: unless-stopped
    ports: ["3847:3847"]
    volumes: [roarr-config:/config]
    extra_hosts: ["host.docker.internal:host-gateway"]
    environment:
      PROWLARR_URL: http://prowlarr:9696
      PROWLARR_API_KEY: ...
      SONARR_URL: http://sonarr:8989        # optional: discovered from Prowlarr
      SONARR_API_KEY: ...
      RADARR_URL: http://radarr:7878        # optional: discovered from Prowlarr
      RADARR_API_KEY: ...
      SEERR_URL: http://seerr:5055          # optional
      SEERR_API_KEY: ...
      JELLYFIN_URL: http://jellyfin:8096    # optional
      JELLYFIN_API_KEY: ...
      QBIT_URL: http://qbittorrent:8080     # optional (watch-first)
      QBIT_PASSWORD: ...
volumes:
  roarr-config:
```

```sh
docker compose up -d                          # run
docker compose pull && docker compose up -d   # update
```

Open `http://<host>:3847`. Put roarr on the same Docker network as the rest
of the stack so container names resolve. Apps that are only published on the
host's ports work too: a `localhost` / `127.0.0.1` URL (yours, or one roarr
discovers in Prowlarr or Sonarr) is retried on the Docker host
(`host.docker.internal`, then the default gateway) when nothing answers inside
the container. Release links handed to Sonarr/Radarr are rewritten to the
Prowlarr address *they* use, so the apps do not need to resolve the same names
as roarr. Everything except connection
settings can also be changed in **Settings**, and is saved in `/config`.

### Configuration

| Variable | Default | |
|---|---|---|
| `PROWLARR_URL`, `PROWLARR_API_KEY` | — | **Required.** All searching goes through Prowlarr's indexers. |
| `SONARR_URL`, `SONARR_API_KEY` | from Prowlarr | Shows. Read from Prowlarr's *Apps* if unset. |
| `RADARR_URL`, `RADARR_API_KEY` | from Prowlarr | Movies. Same. |
| `SEERR_URL`, `SEERR_API_KEY` | — | Fast TMDB search with posters, request inbox. Works with Seerr, Jellyseerr and Overseerr. |
| `JELLYFIN_URL`, `JELLYFIN_API_KEY` | — | Sign in with Jellyfin accounts; refresh the library right after each import. |
| `QBIT_URL`, `QBIT_USERNAME`, `QBIT_PASSWORD` | host/user from Sonarr | Watch-first packs and "download anyway". Sonarr hides the password, so set `QBIT_PASSWORD`. |
| `AUTH_MODE` | `jellyfin` if `JELLYFIN_URL` set, else `none` | `jellyfin`, `password` (with `AUTH_PASSWORD`), or `none`. |
| `JELLYFIN_ADMIN_ONLY` | `true` | Only Jellyfin admins may sign in. |
| `WATCH_FIRST` | `true` | Download packs in episode order and import as each finishes. |
| `PREFETCH` | `true` | Start searching a show as soon as its page opens. |
| `SEERR_AUTO_FETCH` | `false` | Grab newly approved Seerr requests automatically. Turn on Seerr's "prevent search" for its Sonarr/Radarr servers so they don't search too. |
| `INDEXER_TIMEOUT` | `30` | Seconds before giving up on a slow indexer. |
| `CACHE_MINUTES` | `20` | How long indexer answers are reused. |
| `PORT` | `3847` | |
| `VERIFY_MEDIA` | `auto` | Check finished downloads with ffprobe/ffmpeg. `auto` is on when `ffprobe` is installed (`FFPROBE_PATH`, `FFMPEG_PATH` override the lookup). |
| `VERIFY_AUTO_REGRAB` | `false` | When a download fails the checks, blocklist it and grab the next best release. |
| `VERIFY_PATH_MAP` | — | `client=roarr` path prefixes, comma-separated, when roarr sees qBittorrent's files under another path (`/downloads=/data/downloads`). |

### AV1 converter (optional, off by default)

roarr can convert finished downloads to AV1 with ffmpeg (SVT-AV1; libaom only
if the ffmpeg has no SVT-AV1), in a low-priority background queue.

| Variable | Default | |
|---|---|---|
| `TRANSCODE` | `false` | Turn the converter on. |
| `TRANSCODE_CONCURRENCY` | `1` | Files converted at once (1-8). |
| `TRANSCODE_NICE` | `10` | CPU niceness of ffmpeg (0-19). |
| `TRANSCODE_CRF` | auto | Quality (lower is better, 0-63). Auto: 26 for SD, 28 for 720p, 30 for 1080p, 32 for 1440p, 34 for 4K. |
| `TRANSCODE_PRESET` | `6` | Speed (SVT-AV1 0-13; higher is faster and larger). 6-8 is a good range. |
| `TRANSCODE_FILM_GRAIN` | `0` | Film-grain synthesis level (0 = off, 1-50). Try 8-12 for grainy film sources. |
| `TRANSCODE_MIN_SAVINGS` | `15` | Keep the original unless the result is at least this many percent smaller. |
| `TRANSCODE_PATH_MAP` | — | Only if roarr sees the media under other paths than Sonarr/Radarr: `/tv=/media/tv,/movies=/media/movies`. |

How it works:
- A file is queued when Sonarr or Radarr records its import (their own, or a
  watch-first import). Only imports made after you turn it on are converted;
  nothing from your existing library.
- Video becomes 10-bit AV1. All audio, subtitle and attachment streams,
  chapters and metadata are copied, and HDR colour tags are kept. `.mkv` and
  `.mp4` files only. Files that are already AV1, or that carry Dolby Vision
  (the Dolby layer would be lost), are skipped.
- The result is written next to the original under a temporary name and
  checked with ffprobe: duration within 1 s, same number of video, audio and
  subtitle streams, same chapters, AV1 video. Only then does it replace the
  original with an atomic rename. If anything fails, or the result is not
  `TRANSCODE_MIN_SAVINGS` smaller, the original stays as it was.
- Afterwards roarr asks Sonarr/Radarr to rescan and Jellyfin to refresh.
- `GET /api/transcode` shows the queue, progress and bytes saved.

roarr needs **read/write access to your media folders, at the same paths
Sonarr/Radarr use** (mount them into the roarr container like you do for
Sonarr; with `user:` set to a uid that may write to them). The torrent's own
hard-linked copy is not touched, so seeding goes on, but the old data stays on
disk until that torrent is removed.

The image includes a static ffmpeg, which adds roughly 100-150 MB (estimate).
Leave it out with `docker build --build-arg FFMPEG=false .`; the converter
then reports itself as unavailable.

Bind-mounting a host folder on `/config` instead of a named volume? The image
runs as uid 65532. Add `user: "1000:1000"` (your uid) to the service.

## What it does

**Search anything.** Press <kbd>⌘K</kbd> (or <kbd>/</kbd>) on any page.
Typing filters your library instantly; the whole library is sent to the
browser once, so it needs no requests. Everything else comes from TMDB
through Seerr in about 250 ms, or from Sonarr/Radarr lookups. Titles that are
not in Sonarr/Radarr yet can be browsed and searched, and are added
(unmonitored, no automatic search) the moment you grab something. Movies can
be fetched straight from the search list with <kbd>⌘↵</kbd>.

**See what indexers have, fast.** roarr talks to Prowlarr one indexer at a
time *in parallel* and streams each answer to the page as it arrives. A slow
or dead indexer never blocks the others. Every release is parsed and scored
on arrival. It is labelled as an episode, multi-episode, season pack,
multi-season pack or complete series, and mapped to the exact episodes it
contains.

**One question, one button.** A show page asks *what do you want?*:
missing episodes, a season, the whole show, or the episodes you tick. A movie
page asks the same about its collection. Missing is picked for you, and the
show is already being searched, so the usual case is a single press of
**Get**.

**roarr picks, or you do.** *Best pick* always shows what **Get** will grab:
the cheapest set of releases covering your selection. It prefers one season
pack over ten equal singles, and better singles over a worse pack. Once every
indexer has answered, **Get** grabs exactly that plan. Before then, roarr
waits until the plan is stable, without waiting for the slowest indexer once
every wanted episode is covered. **Choose release myself** opens the full
list, re-ranked for your selection so a 60-episode pack drops below the
single episode you asked for.

**Watch-first packs.** When a pack goes to qBittorrent, roarr:
1. Downloads its files in episode order: a rolling priority window plus
   sequential pieces.
2. Skips episodes you already have.
3. Imports each finished episode into Sonarr right away, by copy/hardlink so
   the torrent keeps seeding.
4. Has Jellyfin refresh that show.

Episode 1 is watchable long before the pack is done. Sonarr's own import at
the end recognises the episodes that are already imported.

**Manage what you already have.** Every episode shows the file on disk
(quality, codec, size, release group) and a bookmark for monitoring. The
`⋯` menu on an episode or season can:
- **Replace** it: delete the current file and get roarr's best pick, or a
  release you choose.
- **Delete** the file, optionally unmonitoring it so Sonarr leaves it alone.
- Hand the search to Sonarr instead.

Tick episodes (shift-click ticks a run, <kbd>Esc</kbd> clears) and a bar
offers the same for all of them at once. **Manage** on a show or movie
toggles monitoring, moves it to another quality profile, rescans the disk,
or removes it from Sonarr/Radarr with or without its files. Movies get the
same replace and delete controls. Everything destructive asks first.

**Grabbing stays with the \*arrs.** Releases are pushed to Sonarr/Radarr
(`/release/push`), so their download clients, categories, history, blocklist
and import pipeline all apply. If the *arr declines a release you explicitly
chose, roarr shows the reason and offers **Download anyway**. That sends it
to qBittorrent in the *arr's category, and the *arr still imports it.

**Requests.** The Seerr inbox: **Approve & get** approves a request and
fetches it in one go, or approve and decline as usual. With `SEERR_AUTO_FETCH`
on, every approved request is fetched automatically with roarr's search and
planner.

**Fakes are caught.** Each download is checked once its files are on disk;
a pack is checked as soon as its first episode is complete. roarr:
1. Rejects payloads with executables, only archives, or only a sample or
   trailer, and lengths far from the episode or movie runtime.
2. Samples 7 frames across the file and fails black, flat or one static
   slate.
3. Compares the release name with the streams: resolution, codec, HDR,
   10-bit, audio languages (MULTi, GERMAN DL, VFF) and subtitles (ENG SUBS,
   VOSTFR), including subtitle tracks that are empty.

A failed release is hidden from roarr's picks. `GET /api/verify` lists
verdicts; `POST /api/verify/{hash}/regrab` deletes the download and anything
it imported and grabs the next best. Needs ffmpeg in the container and the
download folder mounted.

## Why it is fast (measured)

Measurements come from `lab/`: real Sonarr 4, Radarr 6, Prowlarr 2 and
qBittorrent 5 in Docker, with 7 fake Torznab indexers that have realistic
latency, including a slow one and one that hangs 35% of the time. Full
write-up in [docs/experiments.md](docs/experiments.md).

| Episode search, 7 indexers (median of 5) | first results | everything |
|---|---|---|
| Sonarr interactive search (what pulldeck used) | 6.97 s | 6.97 s |
| Prowlarr aggregated search | 6.49 s | 6.49 s (48 s when an indexer hangs) |
| **roarr** | **0.52 s** | streamed as indexers answer |
| **roarr**, after opening the show page (prefetched) | **0.01 s** | cached |

Main tricks:
- One request per indexer, streamed to the browser over SSE.
- A priority queue per indexer: Prowlarr serializes requests to an indexer
  2 s apart, so the query for what you clicked jumps ahead of background
  prefetch.
- Show-wide prefetch when a page opens.
- Shared in-flight requests and a 20-minute result cache.
- Failed indexers are remembered for 90 s instead of costing their full
  timeout on every search.

## Development

```sh
make lab        # real *arr stack + fake indexers on localhost (needs Docker)
make build      # web UI + binary -> bin/roarr
make test
make bench      # search strategy benchmark against the lab
make swarm      # watch-first download strategy experiment
```

The UI is React + Vite (`web/`), built into `internal/webui/dist` and embedded
in the Go binary. `cd web && ROARR_API=http://localhost:3847 npm run dev`
gives hot reload against a running roarr.

```
cmd/roarr          entry point
internal/release   title parser, scoring, coverage, planner
internal/search    per-indexer prioritised fan-out + cache
internal/app       library, titles, search sessions, grab, watch-first, activity
internal/server    HTTP API, SSE, auth, static UI
internal/transcode AV1 conversion queue (ffmpeg, ffprobe, verify)
internal/{arr,seerr,jellyfin,qbit}  upstream clients
lab/               docker lab, mock indexers, benchmarks, experiments
```
