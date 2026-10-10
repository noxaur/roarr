#!/usr/bin/env python3
"""Watch-first experiment: how should a season pack be downloaded so the
first episode is watchable as early as possible, without slowing the pack?

Three throttled seeders (lab compose profile "swarm") seed a 10-episode pack.
The lab qBittorrent downloads it once per strategy; per-file completion
times are recorded. Stdlib only.

  docker compose -f lab/docker-compose.yml --profile swarm up -d
  python3 lab/swarm/experiment.py [repetitions]
"""
import http.cookiejar, urllib.error, json, os, sys, time, urllib.parse, urllib.request, uuid

LEECH = "http://localhost:8080"
SEEDS = ["http://localhost:8091", "http://localhost:8092", "http://localhost:8093"]
SEED_UP = 2_000_000  # bytes/s per seeder -> ~6 MB/s swarm
HERE = os.path.dirname(__file__)
TORRENT = os.path.join(HERE, "..", "state", "torrents", "Severance.S02.1080p.WEB-DL.DDP5.1.H.264-LABPACK.torrent")
INDEX = json.load(open(os.path.join(HERE, "..", "state", "torrents", "index.json")))
HASH = next(iter(INDEX.values()))["hash"]


class QB:
    def __init__(self, base):
        self.base = base
        self.op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
        self.post("/api/v2/auth/login", {"username": "admin", "password": "adminadmin"})

    def req(self, method, path, data=None, raw=None, ctype=None):
        body = raw if raw is not None else (urllib.parse.urlencode(data).encode() if data is not None else None)
        r = urllib.request.Request(self.base + path, data=body, method=method, headers={"Referer": self.base})
        if ctype:
            r.add_header("Content-Type", ctype)
        with self.op.open(r, timeout=30) as res:
            b = res.read()
            try:
                return json.loads(b)
            except Exception:
                return b

    def post(self, path, data):
        return self.req("POST", path, data)

    def get(self, path, **q):
        return self.req("GET", path + ("?" + urllib.parse.urlencode(q) if q else ""))

    def add(self, torrent, **fields):
        bnd = uuid.uuid4().hex
        parts = []
        for k, v in fields.items():
            parts.append(f'--{bnd}\r\nContent-Disposition: form-data; name="{k}"\r\n\r\n{v}\r\n'.encode())
        parts.append(f'--{bnd}\r\nContent-Disposition: form-data; name="torrents"; filename="t.torrent"\r\nContent-Type: application/x-bittorrent\r\n\r\n'.encode() + torrent + b"\r\n")
        parts.append(f"--{bnd}--\r\n".encode())
        return self.req("POST", "/api/v2/torrents/add", raw=b"".join(parts), ctype=f"multipart/form-data; boundary={bnd}")

    def prefs(self, **p):
        self.post("/api/v2/app/setPreferences", {"json": json.dumps(p)})


def setup_seeds(torrent):
    for i, url in enumerate(SEEDS):
        q = QB(url)
        prefs = dict(up_limit=SEED_UP, limit_lan_peers=True, dht=False, pex=False, lsd=False, max_ratio_enabled=False, queueing_enabled=False)
        if i == 0:
            prefs.update(enable_embedded_tracker=True, embedded_tracker_port=9000)
        q.prefs(**prefs)
        if not q.get("/api/v2/torrents/info", hashes=HASH):
            q.add(torrent, savepath="/seed", skip_checking="true")
    time.sleep(3)
    for url in SEEDS:
        t = QB(url).get("/api/v2/torrents/info", hashes=HASH)
        print("seed", url, t[0]["state"], round(t[0]["progress"], 3))


def episode_order(files):
    return sorted(range(len(files)), key=lambda i: files[i]["name"])


def run(q, torrent, strategy):
    q.post("/api/v2/torrents/delete", {"hashes": HASH, "deleteFiles": "true"})
    time.sleep(2)
    q.add(torrent, savepath="/data/downloads/exp", stopped="true", paused="true", category="exp")
    files = []
    for _ in range(80):  # qBittorrent 5 adds asynchronously
        try:
            files = q.get("/api/v2/torrents/files", hash=HASH)
        except urllib.error.HTTPError:
            files = []
        if files:
            break
        time.sleep(0.25)
    order = episode_order(files)
    seq = strategy in ("sequential", "seq+firstlast", "window+seq", "focus+seq")
    if seq:
        q.post("/api/v2/torrents/toggleSequentialDownload", {"hashes": HASH})
    if strategy == "seq+firstlast":
        q.post("/api/v2/torrents/toggleFirstLastPiecePrio", {"hashes": HASH})

    def apply(done):
        todo = [i for i in order if i not in done]
        if strategy.startswith("window"):
            for slot, i in enumerate(todo):
                p = 7 if slot == 0 else 6 if slot == 1 else 1
                q.post("/api/v2/torrents/filePrio", {"hash": HASH, "id": str(i), "priority": str(p)})
        elif strategy.startswith("focus"):
            for slot, i in enumerate(todo):
                q.post("/api/v2/torrents/filePrio", {"hash": HASH, "id": str(i), "priority": "7" if slot == 0 else "0"})

    done = {}
    apply(done)
    start = time.time()
    q.post("/api/v2/torrents/start", {"hashes": HASH})
    while True:
        files = q.get("/api/v2/torrents/files", hash=HASH)
        changed = False
        for i, f in enumerate(files):
            if f["progress"] >= 1 and i not in done:
                done[i] = time.time() - start
                changed = True
        if changed:
            apply(done)
        if len(done) == len(files):
            break
        if time.time() - start > 400:
            print(f"{strategy:14s} stalled: {len(done)}/{len(files)} files after 400s", flush=True)
            q.post("/api/v2/torrents/delete", {"hashes": HASH, "deleteFiles": "true"})
            return [float("inf")] * len(files)
        time.sleep(0.25)
    per = [done[i] for i in order]
    q.post("/api/v2/torrents/delete", {"hashes": HASH, "deleteFiles": "true"})
    return per


def main():
    reps = int(sys.argv[1]) if len(sys.argv) > 1 else 2
    torrent = open(TORRENT, "rb").read()
    setup_seeds(torrent)
    q = QB(LEECH)
    q.prefs(dht=False, pex=False, lsd=False, limit_lan_peers=True, dl_limit=0, queueing_enabled=False)
    strategies = sys.argv[2].split(",") if len(sys.argv) > 2 else ["default", "sequential", "seq+firstlast", "window", "window+seq", "focus+seq"]
    results = {s: [] for s in strategies}
    for r in range(reps):
        for s in strategies:
            per = run(q, torrent, s)
            results[s].append(per)
            print(f"{s:14s} rep{r} E01={per[0]:6.1f}s  E02={per[1]:6.1f}s  all={max(per):6.1f}s  mean-wait={sum(per)/len(per):6.1f}s", flush=True)
    print("\nmedian over reps")
    print(f"{'strategy':14s} {'E01 ready':>10s} {'E02 ready':>10s} {'mean wait':>10s} {'all done':>10s}")
    med = lambda xs: sorted(xs)[len(xs) // 2]
    summary = {}
    for s in strategies:
        e1 = med([p[0] for p in results[s]]); e2 = med([p[1] for p in results[s]])
        mw = med([sum(p) / len(p) for p in results[s]]); al = med([max(p) for p in results[s]])
        summary[s] = dict(e01=e1, e02=e2, mean_wait=mw, all=al)
        print(f"{s:14s} {e1:9.1f}s {e2:9.1f}s {mw:9.1f}s {al:9.1f}s")
    json.dump({"results": results, "summary": summary}, open("/tmp/swarm-results.json", "w"), indent=1, default=str)


if __name__ == "__main__":
    main()
