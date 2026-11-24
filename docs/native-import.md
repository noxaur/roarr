# Native import

`IMPORT_MODE=native` makes roarr the download client and the importer.
Sonarr and Radarr are not necessary. The default is `IMPORT_MODE=arr`:
grabs go to Sonarr/Radarr, as before.

## Flow

```
Get ─► Prowlarr link ─► qBittorrent (category QBIT_CATEGORY)
                               │
          roarr native loop ◄──┘ every 2 s
          1. plan: map torrent files to episodes / main movie file
             skip samples, extras, episodes already in the library
          2. watch-first packs: sequential, rolling file priority
          3. per finished file: before-import hooks
             ─► hardlink | copy | move into TV_ROOT / MOVIES_ROOT
             ─► subtitles next to the video
             ─► library.json ─► after-import hooks
          4. Jellyfin refresh (and Sonarr/Radarr rescan, if they know the title)
```

The code is in two places:

- `internal/importer`: naming, file classes, matching, transfer, path
  mapping, the `library.json` store, the hooks, the guarded delete
  (`delete.go`) and ZIP unpacking (`archive.go`). It makes no network
  calls.
- `internal/app/native.go`: the backend. It adds torrents, runs the loop,
  connects the activity feed and refreshes Jellyfin. The metadata code is
  in `internal/app/native_meta.go`. The library list, the title pages,
  delete, monitoring and the missing search are in
  `internal/app/native_library.go`.

`app.Backend` is the switch. It has two implementations: `arrBackend`
(the old path, not changed) and `*Native`.

## Library layout

| Content | Path |
|---|---|
| Episode | `TV_ROOT/Show (Year)/Season 01/Show - S01E02.ext` |
| Multi-episode file | `.../Season 01/Show - S01E02-E03.ext` |
| Special | `.../Season 00/Show - S00E01.ext` |
| Movie | `MOVIES_ROOT/Movie (Year)/Movie (Year).ext` |
| Subtitle | `<video name>.<lang>[.forced][.sdh].<ext>` |

roarr removes characters that are not permitted in names (`<>:"/\|?*`).
It changes `Title: Sub` to `Title - Sub`. The first import of a title
sets the folder name and year. roarr keeps that name, also when the
metadata changes later. A folder taken over from Sonarr or Radarr, or
found on disk, keeps its own name (`Title.Folder`), and a new episode
goes into the season folder that is already there (`Season 1`,
`Specials`). Renaming them was rejected: Jellyfin would see every file as
new, and the files Sonarr and Radarr placed are still seeding.

## Metadata

| Source | When roarr uses it |
|---|---|
| Sonarr / Radarr | When configured and the title is in them. Release names follow their TVDB episode numbering, so this source matches best. |
| TMDB through Seerr (`/api/v1/tv/{id}`, `/tv/{id}/season/{n}`, `/movie/{id}`) | All other cases. This adds no new dependency: roarr already uses Seerr for discovery. |
| Jellyfin | Not a metadata source. It only knows titles that are already in the library. |

When Sonarr is not configured, show pages come from TMDB through Seerr.
When Radarr is not configured, movie pages come from TMDB through Seerr.
Episodes that roarr imported show as on disk. Episodes that roarr is
downloading show as queued.

## Native library (no Sonarr/Radarr)

When `IMPORT_MODE=native` and Sonarr is not configured, roarr keeps the
show library itself. When Radarr is not configured, it keeps the movie
library itself. The source is `library.json`. The API shapes do not change,
so the UI works as in arr mode. With Sonarr or Radarr configured, that
kind stays in Sonarr or Radarr. Arr mode does not change.

| Item | Native library |
|---|---|
| Library id | The TMDB id of the title. A title with no TMDB id gets 1073741824 + its TVDB id. `/tv/{id}` and `/movie/{id}` use this id. |
| Episode id | `season × 10000 + episode` (S01E02 is 10002). |
| Library list | Every title that roarr tracks: titles you got, with their files. |
| On disk | Videos that roarr imported, and other videos in the title folder (`Season NN/` for shows). |
| Downloading | Episodes of roarr jobs that are not finished. |
| Metadata | TMDB through Seerr. The episode lists of monitored shows load again every 6 hours, and on **Refresh**. If Seerr is not available, roarr uses the stored list, and grabs for tracked titles still work. |

### Monitoring

A title is monitored until you turn it off. These episodes of a monitored
show are monitored:

- The episodes that you got (the grab monitors them).
- The episodes that you monitor yourself (episode, season).
- Episodes that air after roarr started to track the show. Specials
  (season 0) only when you monitor them.

"Missing" is a monitored episode that aired, is not on disk and is not
downloading. Episodes without an air date are not aired. A movie is
missing when it is monitored and not on disk.

### Delete

| Action | Endpoint | Result |
|---|---|---|
| Episodes, season | `POST /api/tv/{id}/episodes` `{"action":"delete","episodeIds":[...],"removeTorrent":false}` | Deletes the videos of these episodes and the subtitles that roarr placed with them. |
| Movie file | `DELETE /api/movie/{id}/file[?removeTorrent=1]` | Deletes the movie video and its subtitles. |
| Title | `DELETE /api/{kind}/{id}[?deleteFiles=1][&removeTorrent=1]` | Stops tracking the title. Removes its unfinished torrents from qBittorrent, with their data. `deleteFiles=1` deletes all its files. `removeTorrent=1` also removes its finished torrents. |

Safety rules:

- roarr deletes a file only when `library.json` has a record of it.
  Videos placed by hand, by Sonarr or by Radarr stay. The result tells you
  how many stayed.
- The path must be inside `TV_ROOT` (shows) or `MOVIES_ROOT` (movies).
  roarr resolves symbolic links in the folders first. A path with `..`, a
  path outside the folder, a path through a link to another folder, and a
  folder are refused.
- Sidecar files that roarr did not place (for example, subtitles you
  added) stay.
- Folders that are empty after the delete are removed, up to the library
  folder. The library folder itself stays.
- `removeTorrent` removes a torrent only when no other library file came
  from it and it is not downloading.
- A file that holds more than one episode (`S01E01-E02`) is deleted when
  you delete one of its episodes.
- There is no recycle bin. A deleted file is gone.

After a delete, Jellyfin refreshes the title. When the title folder is
gone, Jellyfin scans the whole library. The UI asks to stop monitoring
deleted episodes; if you keep them monitored, the missing search gets them
again.

### Missing search

- **Manual:** **Search** on a title (`POST /api/{kind}/{id}/command/search`)
  searches for its missing items. A movie is searched also when it is not
  monitored. **Search** for selected episodes
  (`POST /api/tv/{id}/episodes` with `"action":"search"`) searches for
  those episodes.
- **Periodic:** `MISSING_SEARCH_HOURS=N` (settings: `import.missingSearchHours`)
  searches all monitored titles every N hours. The first pass starts 10
  minutes after roarr starts. The default is 24; `0` turns it off.
- **New episodes:** a monitored episode that aired and is missing is
  searched from its air date on: every hour for the first two days, every
  4 hours until day 6, every 12 hours until day 14. Then the periodic
  search covers it. Air dates are days and releases come hours after the
  broadcast, so a fixed delay after the air time is not possible. roarr has
  no RSS sync: Prowlarr searches one title at a time, which is cheap enough
  for the few shows that air in a week. The schedule is kept in memory;
  after a restart each recent episode is searched once more, which costs
  less than a stored schedule per episode.
- **Upgrades:** the periodic pass also searches a title whose monitored
  files are below the quality cutoff, at most once a week per title
  (`Title.UpgradeAt`). The cutoff is the first preferred resolution (or
  higher) from a WEB-DL or better source (`release.Prefs.AtCutoff`). Only
  releases at the cutoff are taken, so a file is replaced once, by what you
  asked for, and not step by step through every slightly better release.
  Files of unknown quality (renamed, no release recorded) are left alone.
  Sonarr's quality profiles were not copied: the release preferences
  already say what you want, and a second set of rules would disagree with
  them.

A search uses the same planner as **Get**: Prowlarr, the release scores,
the pack/episode plan. It grabs the best plan. It runs in the background,
one title at a time, with a 5-second pause between titles. One title has at
most one search at a time. Grabs show in the activity feed. The result is
in the log.

## Failed downloads and the blocklist

When a download fails because of the release, roarr replaces it: the
release goes on the blocklist (`/config/blocklist.json`), the torrent and
its data leave qBittorrent, and the planner picks again for the episodes
(or the movie) that are still missing. The new download shows in the
activity feed; the replaced one leaves it. When nothing else is found, the
failed row stays with the reason.

| Failure | When |
|---|---|
| Dead | No peer sends the metadata for 30 minutes, or the torrent never shows in qBittorrent for 10 minutes. |
| Stalled | qBittorrent reports `stalledDL` and no data arrived for 1 hour. |
| Fake | Verification fails (see [Hook points](#hook-points)). The download stops at once; the rest of a pack is not downloaded. |
| Nothing usable | Nothing in the torrent or its archives matches the title. |

Failures that are not the release's fault (the file cannot be placed, the
torrent was removed by hand) fail the row without a replacement. Timeouts
count from the time roarr runs at the earliest, so a download does not
fail for time roarr was stopped.

- The blocklist matches a search result by info-hash or by its normalized
  name, so the same release from another indexer, or re-hashed by a
  private tracker, is rejected too. Blocklisted results show as rejected
  with the reason. Entries do not expire; remove them under Admin → Import
  (`DELETE /api/blocklist/{id}`).
- It is separate from `verify.json`: verify records are per download and
  are pruned after 30 days, and most failures have no verdict.
- **Remove and blocklist** in the activity feed adds an entry too.
  **Re-grab** on a verdict deletes the files the download placed, then
  replaces it the same way.
- Replacements run one at a time on their own loop, so a burst of dead
  torrents does not start a burst of searches. A goroutine per failure was
  rejected for that reason.

## Archives

A release with no video but with ZIP files is unpacked:

1. roarr downloads the ZIP files only. Other files (`.nfo`, `.sfv`) are
   skipped.
2. When the torrent is complete, roarr unpacks each ZIP file into
   `.roarr-unpacked-<hash>/` next to it.
3. roarr matches and imports the unpacked files as usual.
4. When the job is done, roarr removes the unpack folder. The library keeps
   its own hardlink or copy.

Limits:

- roarr unpacks only videos and subtitles.
- Entries with absolute names, `..`, drive letters, and links are skipped.
  Every file is written inside the unpack folder.
- The unpacked size can be at most 2 × the archive size + 64 MiB. A larger
  archive (for example, a ZIP bomb) fails the job, and roarr removes what
  it unpacked. roarr counts the bytes it writes; it does not trust the
  size in the archive.
- At most 5000 entries per archive.
- The unpack runs in the import loop. Other imports wait until it is done.
- **RAR, 7-Zip and split ZIP (`.z01`) are not supported.** roarr has no
  dependency outside the Go standard library, which has no RAR reader. Such
  a job fails with "nothing in this download matches ... (roarr cannot
  unpack RAR or 7-Zip archives)". Use a release without archives, or arr
  mode with an unpacker.

## Matching rules

- **Video file names:** `S01E02`, `S01E02E03`, `1x02` and anime absolute
  numbers. The parser is `internal/release`.
- **Season from a folder:** if the file name has only `E05`, `Episode 5`
  or `05 - Title`, roarr uses the season from the parent folder
  (`Season 2/`). If no folder gives a season, roarr uses the pack name.
- **Absolute numbers:** roarr calculates them from the episode order
  when the metadata has none (TMDB has none). It does this only when the
  episode list is complete. If one TMDB season cannot be loaded, roarr
  uses no TMDB episode list. If the list can still miss a season, files
  named only by an absolute number match nothing.
- **Episodes not in the metadata:** a file that names such an episode
  (for example, a new episode that TMDB does not list yet) is still
  placed. roarr keeps all episode numbers from the name. The placement
  has the flag `unverified`.
- **Two files for one episode:** roarr keeps the larger file. A
  multi-episode file is dropped only when every one of its episodes has a
  larger copy.
- **Samples:** `sample` in the name, or a `Sample/` folder.
- **Extras:** a file in a folder such as `Extras/` or `Featurettes/`, or a
  file whose name ends in a marker such as `-trailer`, `.Featurette` or
  `Deleted Scenes`. The same word earlier in the name is part of the
  title, and a name with an episode number is never an extra. roarr does
  not download or import extras.
- **Movies:** roarr imports the largest video that is not a sample. It
  skips the other videos as extras.
- **Subtitles** belong to a video when one of these is true:
  - The subtitle name starts with the video name.
  - The subtitle is in a folder with the video name
    (`Subs/<video>/2_English.srt`).
  - The subtitle names the same episode.
  - The download has only one video.

  roarr reads the language from ISO codes or English language names. It
  reads the flags `forced` and `sdh`/`cc`/`hi`.

## Duplicates and upgrades

Before roarr places a file, it looks for existing files with the same
episodes. It looks in two places: `library.json` and the season folder.
Files that a person, Sonarr or Radarr put there also count.

| Situation | Result |
|---|---|
| Same file (same hardlink) | Skipped: "already in the library". |
| New release is better (resolution, then source, then proper/repack) | Replaced. roarr removes the old video. Its sidecar files (for example, subtitles that you added) stay. They get the new video name only when the new video holds the same episodes; otherwise they keep their old name. A new subtitle replaces an old one with the same name only after the new one is in place. |
| New release is equal or worse | Skipped, unless you selected this release for these episodes. |
| Quality of the existing file is not known (a renamed file) | Skipped, unless you selected the release. |
| The existing file is a multi-episode file that also holds other episodes | Skipped. roarr never deletes episodes that you would lose. |

During a replacement, roarr first moves the old video aside. If the new
video cannot be placed, roarr puts the old video back.

A subtitle that finishes after its video is placed on a later pass, but
only if that video is still the one from the same release. If another
release replaced the video in the meantime, roarr discards the subtitle. If a
subtitle transfer fails, roarr tries again for about one minute.

## Taking over from Sonarr and Radarr

`POST /api/takeover` (Admin → Import → **Take over**) moves a library to
roarr in one background pass, and `GET /api/takeover` shows what it would
move, what roarr still needs, and the progress. It refuses to start while
a needed check fails:

| Check | Needed |
|---|---|
| qBittorrent is connected | yes |
| Seerr is connected (metadata once Sonarr and Radarr are gone) | yes |
| The TV and movie folders are writable | when Sonarr has shows / Radarr has movies |
| Every Sonarr and Radarr root folder is inside `TV_ROOT` / `MOVIES_ROOT` as roarr sees it | the same |
| roarr sees qBittorrent's download folder (`QBIT_PATH_MAP`) | yes |
| A download can be hardlinked into the library | no: imports copy instead |
| Jellyfin is connected | no |

The pass:

1. Every Sonarr series becomes a title with its ids, name, folder,
   alternate titles, episode list (TVDB numbering, from Sonarr) and
   monitoring: the series flag, and each episode in `Monitor` or `Ignore`.
   Each episode file gets a record, so roarr can upgrade and delete it.
   Its rank comes from the release it was grabbed as (`sceneName`), or from
   Sonarr's quality name, and the record's episodes (not the file name) say
   what a replacement would take away. A show without a TMDB id (Sonarr v3)
   gets one through a Seerr search checked against its TVDB id; one that
   has none is listed, because roarr cannot learn of its new episodes.
2. Every Radarr movie becomes a title the same way.
3. Title folders in `TV_ROOT` and `MOVIES_ROOT` that no title owns are
   identified through Seerr: by a `[tmdbid-N]` tag in the name, or by a
   search whose result has the same name and year. A folder without a year
   needs a name only one TMDB title has. Their videos get
   records. Shows found this way monitor only episodes that air from now
   on, so the takeover does not download a backlog nobody asked for.
4. roarr sets `IMPORT_MODE=native` and `import.standalone`.

Paths: Sonarr and Radarr name paths as their containers see them. roarr
tries the path as it is, then the AV1 and verify path maps (`*arr path =
roarr path`), then the path relative to the *arr root folder under
`TV_ROOT` / `MOVIES_ROOT`. When Sonarr or Radarr cannot list its root
folders, the takeover stops and roarr keeps using them. A title whose folder roarr cannot see is
skipped with the reason, and so is a folder Seerr cannot identify. A title
roarr already tracks keeps its monitoring, and files roarr placed keep
their records, so running it again changes nothing.

`standalone` makes roarr drop its Sonarr and Radarr clients in native
mode, and stop finding them through Prowlarr. Every code path that asks
"is Sonarr there?" then uses the native library, without a branch of its
own. Clearing `SONARR_URL` instead was rejected: Prowlarr's app links would
bring Sonarr back. The takeover still reads the configured or discovered
Sonarr and Radarr, so it can run again. Switching the import mode back to
`arr` uses Sonarr and Radarr again; `library.json` stays.

## Seerr requests

When roarr runs the library, Seerr has no Sonarr or Radarr to send a
request to, so roarr grabs approved requests itself (`SEERR_AUTO_FETCH` is
on, whatever the setting). Seerr marks a request available when its
Jellyfin scan finds the title, and roarr asks Jellyfin to refresh after
each import. Setting the status through Seerr's media API was rejected: it
is per title, not per season, so a show with one requested season would
show as complete.

## Path mapping and hardlinks

qBittorrent reports paths from its own container. `QBIT_PATH_MAP`
changes them to paths that roarr can read, for example
`/downloads=/data/torrents`. The longest matching prefix applies. Windows
paths (`D:\Torrents=/mnt/t`) also work. When qBittorrent keeps unfinished
torrents in a separate folder, roarr also looks in that folder
(`download_path`).

`VERIFY_PATH_MAP`, `TRANSCODE_PATH_MAP` and the remote path mappings read
from Sonarr and Radarr go through the same mapper (`importer.PathMap`), so
these rules hold for all of them. Three hand-written mappers had drifted:
one took the first match instead of the longest, and two missed a drive
letter in another case.

A hardlink is only possible when the source and the destination are in
one filesystem and in one Docker bind mount. If the link fails (`EXDEV`,
`EPERM` and similar errors), roarr copies the file. roarr writes a copy to
a hidden temporary file and then renames it. Thus, Jellyfin never sees
part of a file.

## Hook points

The importer runs hooks around each file (`importer.Hooks`): before-import
hooks can refuse or postpone it, after-import hooks see it once it is in
`library.json`. `App.importHooks` registers two at startup, before the
import loop starts. They give native imports what arr mode gets from
polling Sonarr and Radarr, which see nothing of a native import.

- **Verification, before the import.** The first file of a download that
  reaches the importer is verified, and its verdict counts for the whole
  download, as in arr mode. A `fail` keeps every file of the download out
  of the library, stops the download, and replaces it with another release
  (see [Failed downloads](#failed-downloads-and-the-blocklist)). `warn` and `skip` do not hold a file back, and
  nothing is checked when verification is off. A dismissed verdict lets a
  later import of the download through.
  - Arr mode cannot hold a file back, because Sonarr and Radarr import it
    themselves. Native mode can, so the fake never reaches Jellyfin.
  - Only the file that is imported is checked, not the torrent's file
    list. roarr imports only the videos it planned, so an executable in
    the torrent never reaches the library. The importer already knows the
    path, so `VERIFY_PATH_MAP` is not necessary.
  - The check runs in the import loop, like a copy or an unpack. A check
    in the background, with the file kept pending until it is done, was
    rejected: it adds state for a wait of one check per download.
- **Conversion, after the import.** The library file goes to the same
  transcode queue as an import from the Sonarr/Radarr history, so the
  same rules decide if it is converted. The converter keeps the path, so
  `library.json` stays correct.

Rules for a hook:

- Before-import hooks run in sequence. The first error stops the import.
  An error that wraps `importer.ErrRetryLater` keeps the file pending. One
  that wraps `importer.ErrBadRelease` (`importer.BadRelease`) fails the
  whole download, which is then replaced.
- After-import hooks run on the import loop, before the Jellyfin refresh,
  and must return quickly. A hook that moves or renames the file must
  update the store (`Store().Put/Forget`).
- roarr recovers from a panic in a hook and writes it to the log.

## Endpoints

| Endpoint | Change |
|---|---|
| `POST /api/grab` | The same request. In native mode the result has `"via": "roarr"`. |
| `GET /api/activity` | Adds rows with `"source": "roarr"` and `id` `roarr:<hash>`. TV rows have a `watch` object for each episode, in the same format as watch-first jobs. |
| `DELETE /api/activity/roarr:<hash>` | Removes the torrent and its data from qBittorrent and forgets the job. Library files stay, because they are hardlinks or copies. |
| `GET /api/imports` | New. Shows the native settings, all jobs with the placement and errors of each file, the tracked titles, the imported files, the blocklist and the takeover progress. `?title=tv:tmdb:1399` shows the files of one title. |
| `GET /api/status` | Adds `importMode`, `standalone`, `seerrAutoFetch` and `library` (`{"tv":"roarr"\|"sonarr","movie":"roarr"\|"radarr"}`: who keeps each kind). |
| `GET /api/blocklist`, `DELETE /api/blocklist/{id}` | New. The blocklist. |
| `GET /api/takeover`, `POST /api/takeover` | New. See [Taking over](#taking-over-from-sonarr-and-radarr). |
| `GET /api/tv/tvdb/{tvdb}?tmdb=`, `GET /api/movie/tmdb/{tmdb}` | When Sonarr/Radarr are not configured, these use TMDB through Seerr. Before, they returned an error. |
| `PUT /api/settings` | When the request has no `import` object, the stored import settings stay. |
| `GET /api/library`, `GET /api/tv/{id}`, `GET /api/movie/{id}` | Without Sonarr/Radarr: titles from `library.json` (see [Native library](#native-library-no-sonarrradarr)). An unknown id gives 404. |
| `POST /api/tv/{id}/episodes`, `PATCH /api/{kind}/{id}`, `POST /api/{kind}/{id}/command/{refresh\|search}`, `DELETE /api/movie/{id}/file`, `DELETE /api/{kind}/{id}` | Without Sonarr/Radarr: delete, monitor, search and remove in the native library. New option `removeTorrent`. A quality profile change gives an error. |
| `POST /api/grab` | In the native library, `seriesId`/`movieId` in the result is the library id of the title. |

## Limitations

Native mode does not do these things yet:

- **Quality profiles.** The native library has no quality profiles.
  Release choice uses the release preferences in Settings. A profile change
  gives an error.
- **Mixed setups.** In native mode with Sonarr (or Radarr) configured and
  no takeover, that kind of title stays in Sonarr (or Radarr). Titles that
  roarr imported but that Sonarr/Radarr do not know are not in the library
  list.
- **Usenet.** Native mode handles torrents only. Use `arr` mode for usenet.
- **Seerr requests** that were approved more than 7 days ago are not
  fetched. The missing search still gets their monitored episodes.
- **Monitoring.** roarr has no RSS sync and no calendar.
- **Seeding.** roarr does not remove torrents after seeding. Set ratio
  and time limits in qBittorrent. With `IMPORT_TRANSFER=move`, the torrent
  cannot seed the files that roarr moved.
- **Archives.** roarr unpacks ZIP only. RAR and 7-Zip releases fail (see
  [Archives](#archives)).
- **Multi-part movies** (`CD1`/`CD2`): roarr imports only the largest
  part.
- **Numbering differences.** TMDB episode order can be different from
  scene/TVDB numbering (some anime, changed season splits). A refresh
  keeps the stored numbers when TMDB lacks an episode that has a file, and
  then adds only episodes that air after the last stored one, numbered as
  TMDB numbers them. A show first grabbed with TMDB-only metadata has no
  TVDB numbers to keep, so its files can still go to the wrong episode;
  check `GET /api/imports`.
- **Several root folders.** A takeover keeps only titles inside `TV_ROOT`
  and `MOVIES_ROOT`. Titles in other Sonarr or Radarr root folders are
  skipped.
- **Quality comparison** uses only the release name (resolution, source,
  proper/repack). It does not read the media (codec, bitrate, audio) and
  does not use custom formats.
- **Season packs that finish while roarr is stopped:** roarr imports them
  at the next start. Torrents that you add to qBittorrent outside roarr
  are ignored, also when they are in the `roarr` category.
