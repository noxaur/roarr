#!/usr/bin/env python3
"""Finish Jellyfin's first-run wizard, create an API key and libraries, then
set up Seerr against it with Sonarr/Radarr. Prints the env lines roarr needs.
Run after bootstrap.py with `--profile full` services up. Stdlib only."""
import http.cookiejar
import json
import time
import urllib.error
import urllib.request

JF = "http://localhost:8096"
SEERR = "http://localhost:5055"
AUTH = 'MediaBrowser Client="lab", Device="lab", DeviceId="lab-bootstrap", Version="1.0"'


def req(base, method, path, body=None, headers=None, opener=None):
    data = json.dumps(body).encode() if body is not None else None
    h = {"Content-Type": "application/json", **(headers or {})}
    r = urllib.request.Request(base + path, data=data, method=method, headers=h)
    try:
        with (opener.open(r, timeout=60) if opener else urllib.request.urlopen(r, timeout=60)) as res:
            raw = res.read()
            return json.loads(raw) if raw else None
    except urllib.error.HTTPError as e:
        print(f"  ! {method} {path}: {e.code} {e.read()[:300]!r}")
        return None


def jellyfin():
    info = req(JF, "GET", "/System/Info/Public")
    if not info.get("StartupWizardCompleted", info.get("startupWizardCompleted")):
        req(JF, "POST", "/Startup/Configuration", {"UICulture": "en-US", "MetadataCountryCode": "US", "PreferredMetadataLanguage": "en"})
        req(JF, "GET", "/Startup/User")
        req(JF, "POST", "/Startup/User", {"Name": "admin", "Password": "adminadmin"})
        req(JF, "POST", "/Startup/RemoteAccess", {"EnableRemoteAccess": True, "EnableAutomaticPortMapping": False})
        req(JF, "POST", "/Startup/Complete")
        time.sleep(2)
    login = req(JF, "POST", "/Users/AuthenticateByName", {"Username": "admin", "Pw": "adminadmin"}, {"Authorization": AUTH})
    tok = {"Authorization": AUTH + f', Token="{login["AccessToken"]}"'}
    folders = req(JF, "GET", "/Library/VirtualFolders", headers=tok) or []
    names = {f["Name"] for f in folders}
    for name, kind, path in (("Shows", "tvshows", "/media/tv"), ("Movies", "movies", "/media/movies")):
        if name not in names:
            req(JF, "POST", f"/Library/VirtualFolders?name={name}&collectionType={kind}&paths={path}&refreshLibrary=false", {"LibraryOptions": {}}, tok)
    keys = (req(JF, "GET", "/Auth/Keys", headers=tok) or {}).get("Items", [])
    key = next((k["AccessToken"] for k in keys if k.get("AppName") == "roarr"), None)
    if not key:
        req(JF, "POST", "/Auth/Keys?app=roarr", headers=tok)
        keys = req(JF, "GET", "/Auth/Keys", headers=tok)["Items"]
        key = next(k["AccessToken"] for k in keys if k.get("AppName") == "roarr")
    return key


def seerr():
    jar = http.cookiejar.CookieJar()
    op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
    body = {"username": "admin", "password": "adminadmin", "hostname": "jellyfin", "port": 8096, "useSsl": False, "urlBase": "", "email": "admin@lab.local", "serverType": 2}
    if req(SEERR, "POST", "/api/v1/auth/jellyfin", body, opener=op) is None:
        body.pop("serverType")
        req(SEERR, "POST", "/api/v1/auth/jellyfin", body, opener=op)
    for kind, port, key, root in (("sonarr", 8989, "0123456789abcdef0123456789abcd01", "/data/media/tv"), ("radarr", 7878, "0123456789abcdef0123456789abcd02", "/data/media/movies")):
        if req(SEERR, "GET", f"/api/v1/settings/{kind}", opener=op):
            continue
        profiles = req(SEERR, "POST", f"/api/v1/settings/{kind}/test", {"hostname": kind, "port": port, "apiKey": key, "useSsl": False, "baseUrl": ""}, opener=op) or {}
        prof = next((p for p in profiles.get("profiles", []) if "1080" in p["name"]), (profiles.get("profiles") or [{"id": 1, "name": "Any"}])[0])
        s = {"name": kind.title(), "hostname": kind, "port": port, "apiKey": key, "useSsl": False, "baseUrl": "", "activeProfileId": prof["id"], "activeProfileName": prof["name"],
             "activeDirectory": root, "is4k": False, "isDefault": True, "externalUrl": "", "syncEnabled": False, "preventSearch": True, "tagRequests": False, "tags": []}
        if kind == "sonarr":
            s.update({"seriesType": "standard", "animeSeriesType": "anime", "enableSeasonFolders": True, "activeAnimeProfileId": prof["id"], "activeAnimeDirectory": root, "activeLanguageProfileId": 1})
        else:
            s.update({"minimumAvailability": "released"})
        req(SEERR, "POST", f"/api/v1/settings/{kind}", s, opener=op)
    req(SEERR, "POST", "/api/v1/settings/initialize", {}, opener=op)
    main = req(SEERR, "GET", "/api/v1/settings/main", opener=op) or {}
    return main.get("apiKey")


if __name__ == "__main__":
    jk = jellyfin()
    sk = seerr()
    print(f"JELLYFIN_URL=http://jellyfin:8096\nJELLYFIN_API_KEY={jk}\nSEERR_URL=http://seerr:5055\nSEERR_API_KEY={sk}")
