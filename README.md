# roarr

One fast, quiet UI on top of **Sonarr, Radarr, Prowlarr, Seerr and Jellyfin**.

Search anything that exists, see what your indexers have **within half a
second**, and get exactly as much as you want: one episode, a season, a whole
show, a movie or a whole collection. Let roarr pick the best release, or pick
it yourself. Season packs download episode by episode, and each episode lands
in Jellyfin as soon as it is done.

One Docker image (~17 MB), one port, one volume.

## Quick start

```yaml
# docker-compose.yml
services:
  roarr:
    image: ghcr.io/noxaur/roarr:latest
    restart: unless-stopped
    ports: ["3847:3847"]
    volumes: [roarr-config:/config]
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
of the stack so container names resolve. Everything except connection
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

**Grabbing stays with the \*arrs.** Releases are pushed to Sonarr/Radarr
(`/release/push`), so their download clients, categories, history, blocklist
and import pipeline all apply. If the *arr declines a release you explicitly
chose, roarr shows the reason and offers **Download anyway**. That sends it
to qBittorrent in the *arr's category, and the *arr still imports it.

**Requests.** The Seerr inbox: **Approve & get** approves a request and
fetches it in one go, or approve and decline as usual. With `SEERR_AUTO_FETCH`
on, every approved request is fetched automatically with roarr's search and
planner.

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
internal/{arr,seerr,jellyfin,qbit}  upstream clients
lab/               docker lab, mock indexers, benchmarks, experiments
```
