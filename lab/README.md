# lab

A disposable, fully local stack for developing and measuring roarr:
real Sonarr, Radarr, Prowlarr and qBittorrent (plus optional Jellyfin and Seerr)
wired to **fake indexers** (`mockindexer`). No real trackers are contacted.

```sh
sh lab/scripts/seed-config.sh                     # API keys, auth-free configs
docker compose -f lab/docker-compose.yml up -d --build
python3 lab/scripts/bootstrap.py                  # indexers, apps, clients, library
# optional: Jellyfin + Seerr
docker compose -f lab/docker-compose.yml --profile full up -d
python3 lab/scripts/bootstrap_full.py > /tmp/full.env   # prints their API keys
```

roarr runs at http://localhost:3847 (service `roarr` in the compose file).

| Service | URL | Credentials |
|---|---|---|
| Sonarr | :8989 | API key `…abcd01` |
| Radarr | :7878 | API key `…abcd02` |
| Prowlarr | :9696 | API key `…abcd03` |
| qBittorrent | :8080 | admin / adminadmin |
| Jellyfin | :8096 | admin / adminadmin |
| Seerr | :5055 | via Jellyfin login |

## mockindexer

Seven Torznab indexers with different behaviour, each on its own hostname,
because Prowlarr rate-limits per indexer. Sharing one host would serialize
them all and make every fan-out strategy look bad.

| name | median latency | quirks |
|---|---|---|
| swiftbay | 0.35 s | ID search |
| eztvish | 0.9 s | ID search |
| nyaaish | 0.7 s | anime fansub naming, text only |
| torrentgala | 1.2 s | text search only |
| flarecf | 2.8 s | 15% HTTP 503, 50 results max |
| slowtracker | 5.5 s | ID search |
| deadpool | 1.5 s | hangs 45 s on 35% of requests |

Releases are generated deterministically for a small catalogue of real
TVDB/TMDB ids: episodes in many qualities, season packs, multi-season and
complete packs, and movies. `/stats` lists every request it received, which is
how the benchmarks count upstream cost. `/speed/0` turns off all latency.

Real torrents listed in `state/torrents/index.json` (created by
`swarm/maketorrent.py`) are served by every indexer, for end-to-end download
and import tests.

## Experiments

- `go run ./lab/bench`: search strategies compared (first result, 90%,
  complete, upstream requests).
- `swarm/experiment.py`: download-order strategies for season packs on a
  local throttled swarm (3 seeders). Generate the pack first with ffmpeg; see
  docs/experiments.md for the exact commands.

Results: [../docs/experiments.md](../docs/experiments.md).
