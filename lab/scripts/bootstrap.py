#!/usr/bin/env python3
"""Wire the lab stack together: indexers -> Prowlarr -> Sonarr/Radarr,
qBittorrent as download client, root folders and a starter library.

Idempotent: re-running only adds what is missing. Stdlib only.
"""
import json
import sys
import time
import urllib.error
import urllib.request

PROWLARR = ("http://localhost:9696", "0123456789abcdef0123456789abcd03")
SONARR = ("http://localhost:8989", "0123456789abcdef0123456789abcd01")
RADARR = ("http://localhost:7878", "0123456789abcdef0123456789abcd02")
# addresses as seen from inside the compose network
INTERNAL = {"prowlarr": "http://prowlarr:9696", "sonarr": "http://sonarr:8989", "radarr": "http://radarr:7878"}
INDEXERS = ["swiftbay", "torrentgala", "eztvish", "slowtracker", "flarecf", "deadpool", "nyaaish"]

SERIES = [("Severance", 371980, "standard"), ("The Expanse", 280619, "standard"), ("Breaking Bad", 81189, "standard"), ("Frieren", 424536, "anime")]
MOVIES = [438631, 693134, 603, 604, 605]


def call(svc, method, path, body=None, api="v3"):
    base, key = svc
    url = f"{base}/api/{api}{path}"
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method, headers={"X-Api-Key": key, "Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=120) as r:
            raw = r.read()
            return json.loads(raw) if raw else None
    except urllib.error.HTTPError as e:
        print(f"  ! {method} {path}: {e.code} {e.read()[:400]!r}")
        return None


def set_field(fields, name, value):
    for f in fields:
        if f["name"] == name:
            f["value"] = value
            return
    fields.append({"name": name, "value": value})


def prowlarr():
    print("prowlarr: indexers")
    current = {i["name"]: i for i in call(PROWLARR, "GET", "/indexer", api="v1") or []}
    schema = call(PROWLARR, "GET", "/indexer/schema", api="v1")
    torznab = next(s for s in schema if s["implementation"] == "Torznab")
    for name in INDEXERS:
        want = f"http://{name}:9117/{name}"
        if name in current:
            ix = current[name]
            base = next((f.get("value") for f in ix["fields"] if f["name"] == "baseUrl"), "")
            if base != want:
                set_field(ix["fields"], "baseUrl", want)
                call(PROWLARR, "PUT", f"/indexer/{ix['id']}?forceSave=true", ix, api="v1")
                print("  ~", name, "->", want)
            continue
        body = json.loads(json.dumps(torznab))
        body.update({"name": name, "enable": True, "appProfileId": 1, "priority": 25})
        set_field(body["fields"], "baseUrl", want)
        set_field(body["fields"], "apiPath", "/api")
        set_field(body["fields"], "apiKey", "lab")
        call(PROWLARR, "POST", "/indexer?forceSave=true", body, api="v1")
        print("  +", name)

    print("prowlarr: applications")
    apps = {a["name"] for a in call(PROWLARR, "GET", "/applications", api="v1") or []}
    app_schema = call(PROWLARR, "GET", "/applications/schema", api="v1")
    for impl, svc, cats in (("Sonarr", SONARR, [5000, 5030, 5040, 5045, 5070]), ("Radarr", RADARR, [2000, 2030, 2040, 2045])):
        if impl in apps:
            continue
        body = json.loads(json.dumps(next(s for s in app_schema if s["implementation"] == impl)))
        body.update({"name": impl, "syncLevel": "fullSync"})
        set_field(body["fields"], "prowlarrUrl", INTERNAL["prowlarr"])
        set_field(body["fields"], "baseUrl", INTERNAL[impl.lower()])
        set_field(body["fields"], "apiKey", svc[1])
        set_field(body["fields"], "syncCategories", cats)
        if impl == "Sonarr":
            set_field(body["fields"], "animeSyncCategories", [5070])
        call(PROWLARR, "POST", "/applications?forceSave=true", body, api="v1")
        print("  +", impl)
    call(PROWLARR, "POST", "/command", {"name": "ApplicationIndexerSync"}, api="v1")


def arr(svc, name, root, category, cat_field):
    print(f"{name}: root folder + download client")
    if not any(r["path"] == root for r in call(svc, "GET", "/rootfolder") or []):
        call(svc, "POST", "/rootfolder", {"path": root})
    if not call(svc, "GET", "/downloadclient"):
        schema = call(svc, "GET", "/downloadclient/schema")
        body = json.loads(json.dumps(next(s for s in schema if s["implementation"] == "QBittorrent")))
        body.update({"name": "qBittorrent", "enable": True, "priority": 1})
        set_field(body["fields"], "host", "qbittorrent")
        set_field(body["fields"], "port", 8080)
        set_field(body["fields"], "username", "admin")
        set_field(body["fields"], "password", "adminadmin")
        set_field(body["fields"], cat_field, category)
        call(svc, "POST", "/downloadclient?forceSave=true", body)


def library():
    profiles = call(SONARR, "GET", "/qualityprofile")
    profile = next((p["id"] for p in profiles if p["name"] == "HD-1080p"), profiles[0]["id"])
    have = {s["tvdbId"] for s in call(SONARR, "GET", "/series") or []}
    for title, tvdb, kind in SERIES:
        if tvdb in have:
            continue
        hits = call(SONARR, "GET", f"/series/lookup?term=tvdb:{tvdb}")
        if not hits:
            print("  ! lookup failed", title)
            continue
        body = hits[0]
        body.update({"qualityProfileId": profile, "rootFolderPath": "/data/media/tv", "monitored": True, "seasonFolder": True, "seriesType": kind,
                     "addOptions": {"monitor": "all", "searchForMissingEpisodes": False, "searchForCutoffUnmetEpisodes": False}})
        r = call(SONARR, "POST", "/series", body)
        print("  + series", r and r["title"])

    profiles = call(RADARR, "GET", "/qualityprofile")
    profile = next((p["id"] for p in profiles if p["name"] == "HD-1080p"), profiles[0]["id"])
    have = {m["tmdbId"] for m in call(RADARR, "GET", "/movie") or []}
    for tmdb in MOVIES[:3]:  # leave the rest for "add from search" flows
        if tmdb in have:
            continue
        hit = call(RADARR, "GET", f"/movie/lookup/tmdb?tmdbId={tmdb}")
        if not hit:
            continue
        hit.update({"qualityProfileId": profile, "rootFolderPath": "/data/media/movies", "monitored": True, "minimumAvailability": "released",
                    "addOptions": {"searchForMovie": False, "monitor": "movieOnly"}})
        r = call(RADARR, "POST", "/movie", hit)
        print("  + movie", r and r["title"])


def wait():
    for svc, api in ((PROWLARR, "v1"), (SONARR, "v3"), (RADARR, "v3")):
        for _ in range(90):
            try:
                if call(svc, "GET", "/system/status", api=api):
                    break
            except Exception:
                pass
            time.sleep(2)
        else:
            sys.exit(f"{svc[0]} never came up")


if __name__ == "__main__":
    wait()
    arr(SONARR, "sonarr", "/data/media/tv", "tv-sonarr", "tvCategory")
    arr(RADARR, "radarr", "/data/media/movies", "radarr", "movieCategory")
    prowlarr()
    library()
    time.sleep(5)
    print("sonarr indexers:", [i["name"] for i in call(SONARR, "GET", "/indexer") or []])
    print("radarr indexers:", [i["name"] for i in call(RADARR, "GET", "/indexer") or []])
