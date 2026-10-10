# Experiments

All numbers come from the lab in `lab/`:
- Real Sonarr 4.0.20, Radarr 6.4.4, Prowlarr 2.6.5, qBittorrent 5.2.4,
  Jellyfin 12.2 and Seerr 3.5 in Docker on one machine (10 cores, Docker
  Desktop).
- Prowlarr feeds seven fake Torznab indexers (`lab/mockindexer`), each with
  its own latency profile and hostname.

The numbers are directional. Real trackers vary more, but the *ratios*
between strategies are what decided the design.

## 1. Where does search time go?

`go run ./lab/bench -n 5` searches one episode of *The Expanse* (5 different
episodes) with each strategy. Columns: time until the first results are
known, until 90% of the final result count is known, and until the request
completes.

| strategy | first | 90% | all | results | indexer requests |
|---|---|---|---|---|---|
| Sonarr interactive search (`/api/v3/release`, what pulldeck used) | 6.97 s | 6.97 s | 6.97 s | 26 | 6 |
| Sonarr season search | 6.79 s | 6.79 s | 6.79 s | 299 | 6 |
| Prowlarr aggregated `/api/v1/search` | 6.49 s | 6.49 s | 6.49 s | 36 | 7 |
| one Prowlarr JSON search per indexer, in parallel | **0.52 s** | 5.97 s | 7.33 s | 35 | 7 |
| one Prowlarr Torznab call per indexer, in parallel | 0.42 s | 5.50 s | 5.50 s | 36 | 7 |

Medians of 5. Raw runs in the bench output.

- Every "single request" strategy is as slow as the slowest indexer. When the
  hanging indexer hangs, it is much worse: up to 48 s for the Prowlarr
  aggregate, and up to 100 s for Sonarr in an earlier run where it retried.
- Fanning out per indexer makes the first results available **13× sooner**,
  and the hanging indexer only delays its own results.
- JSON and Torznab per-indexer calls are within noise of each other. roarr
  uses the JSON API: it returns parsed IDs, info-hashes and download URLs
  that Prowlarr has already proxied.

### Lab artifact found on the way

The first run put all seven fake indexers on one hostname, and every
per-indexer strategy timed out. Prowlarr rate-limits **per host**, so seven
"indexers" on one host were served one after another. Each indexer now has
its own network alias, as real trackers do.

### Prowlarr serializes queries to the same indexer

Six season queries sent at once to one indexer (median latency 0.35 s):

```
season 4: 0.53 s   season 3: 2.48 s   season 6: 4.61 s
season 1: 6.60 s   season 5: 8.50 s   season 2: 10.62 s
```

Prowlarr runs one query per indexer at a time, about 2 s apart. This shaped
the search engine:

- **roarr keeps its own queue per indexer** and sends one query at a time, so
  it controls the order instead of Prowlarr's arrival order.
- **Priorities.** Queries for what you are looking at come first, then the
  show-wide query, then prefetching of other seasons (seasons with missing
  episodes first). A query already queued at low priority is bumped when a
  page asks for it.
- **Abandoned prefetch is dropped.** If nobody waits for a prefetch query by
  the time its turn comes, it is skipped instead of spending indexer quota.
- **Per-episode queries only for 1–3 episodes.** For more, the season query
  already returns them.

These rules are covered by `internal/search/engine_test.go`.

## 2. End-to-end through roarr

Server-sent events from `/api/search`, The Expanse (6 seasons), 7 indexers:

| | first results | everything |
|---|---|---|
| open the show page (cold) | 0.52 s | ~43 s: 7 queries per indexer at ~2 s each, plus the hanging indexer |
| then click S02E05 | **0.01 s** | cached by the prefetch |
| reopen the show page | 0.01 s | — |

Two follow-ups came out of this:

- **Negative caching.** A failed or timed-out query is remembered for 90 s.
  Before that, the "warm" run still waited the full 30 s timeout for the
  hanging indexer on every search.
- **Auto mode does not wait for everything.** It grabs once every wanted
  episode is covered and the plan has not changed for 2 s, with at least
  2.5 s elapsed. Measured: a three-movie collection decided after 8.6 s,
  and a whole show (62 episodes) after 2.6 s. The slow indexers keep filling
  the cache in the background.

## 3. Watch-first: the download order for a season pack

**Setup.**
- The pack: 10 synthetic 11-minute episodes, 30 MB each, made with ffmpeg
  (see below). They are long enough that Sonarr does not treat them as
  samples.
- Three qBittorrent seeders capped at 2 MB/s each, with qBittorrent's
  embedded tracker.
- The lab qBittorrent downloads the pack once per strategy
  (`lab/swarm/experiment.py`), polling per-file progress every 250 ms.

| strategy | episode 1 ready | episode 2 ready | mean wait per episode | whole pack |
|---|---|---|---|---|
| qBittorrent default (rarest first) | 104.6 s | 100.7 s | 101.4 s | 104.6 s |
| sequential | 47.9 / 61.5 s | 36.0 / 61.5 s | 83.5 / 76.0 s | 132.7 / 102.7 s |
| sequential + first/last piece | 52.5 s | 37.4 s | 81.3 s | 129.5 s |
| rolling priority window (next file 7, the one after 6, rest 1) | 13.9 / 33.0 s | 27.3 / 27.0 s | 66.1 / 70.6 s | 111.5 / 93.2 s |
| **window + sequential** | **11.1 / 21.3 s** | 35.0 / 30.4 s | 72.5 / 63.1 s | 121.2 / 107.5 s |
| focus (only the next file enabled, others skipped) | stalled | | | |

Two repetitions where shown (`a / b`). The swarm is small and noisy, so
read these as ranges.

- With the default order, **episode 1 finishes when everything finishes**.
  That is the problem watch-first solves.
- Sequential alone helps less than expected. Pieces are requested in order,
  but slow peers hold the early pieces, so the first file still trails.
- The priority window gets the first episode out about **5× sooner**, at
  roughly the same total time.
- Adding sequential within the window gave the fastest first episode in both
  runs, for about 10% more total time. Getting episode 1 first is the
  stated goal, so roarr uses **window + sequential** (`watchSequential` in
  `internal/app/watch.go`).
- **Never temporarily disable the other files.** Once all *wanted* files are
  complete, the client counts as a seed. Seeders drop the connection, and
  nothing reconnects until the next tracker announce. The "focus" strategy
  sat stalled for 400 s. roarr only uses priority 0 for files it will never
  need: samples, and episodes already in the library.

### End to end (roarr → qBittorrent → Sonarr → Jellyfin)

The same pack grabbed through roarr ("download anyway", because Sonarr
refused it as too small), with watch-first:

```
  6.1s  E01 99%                             sonarr: ··········
 19.8s  E01 99% E02 87% E03 done            sonarr: ··■·······   E03 imported
 28.8s  E04 done                            sonarr: ··■■······
 38.0s  E01 done                            sonarr: ■·■■······
 41.1s  E02 done                            sonarr: ■■■■······
 ...
 77.5s  99.8%                               sonarr: ■■■■■■■■■·
done:   10/10 in Sonarr, hardlinked (same inode, no extra disk), 10/10 in Jellyfin
```

Each finished episode went into Sonarr as a hardlink within about 1.5 s,
while the torrent kept downloading, and Jellyfin refreshed the show.
E01/E02 waited about 30 s on their last pieces while E03 completed: a slow
seeder held them. roarr imports whatever completes, in any order.

## 4. Things the *arr APIs taught us (and how roarr handles them)

- **An approved `/release/push` is not a grab.** Sonarr approved a release
  and then failed to hand it to qBittorrent (a magnet without trackers). It
  only logged that failure. roarr confirms through the "grabbed" history
  event before it reports success.
- **Sonarr declines multi-season and complete-series packs on push**
  ("Multi-season releases are not supported"). With qBittorrent configured,
  roarr adds those straight to qBittorrent in Sonarr's category and imports
  every episode itself through watch-first. Otherwise it shows Sonarr's
  reason.
- **`/manualimport` with `seriesId` scans the series' library folder, not
  the download.** It returned HTTP 500 while the folder did not exist yet.
  roarr looks the download up by id. When Sonarr is not tracking the
  download yet, it scans the torrent folder instead.
- **Sonarr hides the qBittorrent password** (`********`) in
  `/downloadclient`. The host and user can be discovered from Sonarr, but
  the password must be configured.
- **qBittorrent 5 adds torrents asynchronously** (`/torrents/files` 404s for
  a moment), and **a stopped magnet never fetches its metadata**. roarr
  retries the first, and starts magnets right away for the second.
- **Incomplete files are named `*.mkv.!qB`** when "append extension" is on.
  Watch-first strips the suffix when it maps files to episodes.
- **Auto mode re-plans around declines.** If Sonarr refuses a pick (size
  limits, already has it), the episodes it would have filled are re-planned
  with the next best releases.

## Reproduce

```sh
make lab                                   # stack + bootstrap
go run ./lab/bench -n 5                    # section 1

# section 3: build the pack, torrent it, seed it, run the strategies
cd lab/state/swarm/payload && N=Severance.S02.1080p.WEB-DL.DDP5.1.H.264-LABPACK && mkdir -p $N
for i in 01 02 03 04 05 06 07 08 09 10; do
  docker run --rm -v "$PWD/$N":/out lscr.io/linuxserver/ffmpeg -loglevel error \
    -f lavfi -i testsrc=size=320x180:rate=25 -f lavfi -i sine=frequency=440:sample_rate=22050 \
    -t 660 -c:v libx264 -preset ultrafast -b:v 330k -c:a aac -b:a 32k \
    /out/Severance.S02E$i.1080p.WEB-DL.DDP5.1.H.264-LABPACK.mkv
done; cd -
uv run --with torf python lab/swarm/maketorrent.py
docker compose -f lab/docker-compose.yml --profile swarm up -d
python3 lab/swarm/experiment.py 2
```
