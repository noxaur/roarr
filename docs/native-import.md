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
metadata changes later.

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
| Metadata | TMDB through Seerr. The episode list loads again after 6 hours, or on **Refresh**. If Seerr is not available, roarr uses the stored list. |

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
  minutes after roarr starts. `0` (the default) turns it off.

A search uses the same planner as **Get**: Prowlarr, the release scores,
the pack/episode plan. It grabs the best plan. It runs in the background,
one title at a time, with a 5-second pause between titles. One title has at
most one search at a time. Grabs show in the activity feed. The result is
in the log.

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

## Path mapping and hardlinks

qBittorrent reports paths from its own container. `QBIT_PATH_MAP`
changes them to paths that roarr can read, for example
`/downloads=/data/torrents`. The longest matching prefix applies. Windows
paths (`D:\Torrents=/mnt/t`) also work. When qBittorrent keeps unfinished
torrents in a separate folder, roarr also looks in that folder
(`download_path`).

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
  of the library. The file fails like any other import that cannot be
  done: after about one minute of tries, with the verdict as its message
  in the activity feed. `warn` and `skip` do not hold a file back, and
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
  An error that wraps `importer.ErrRetryLater` keeps the file pending.
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
| `GET /api/imports` | New. Shows the native settings, all jobs with the placement and errors of each file, the tracked titles and the imported files. `?title=tv:tmdb:1399` shows the files of one title. |
| `GET /api/status` | Adds `importMode`. |
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
- **Mixed setups.** In native mode with Sonarr (or Radarr) configured, that
  kind of title stays in Sonarr (or Radarr). Titles that roarr imported but
  that Sonarr/Radarr do not know are not in the library list.
- **UI text.** The UI still names Sonarr/Radarr in some menus and dialogs
  (for example, "Remove from Sonarr", "recycle bin"). In the native
  library these actions go to roarr. The UI has no `removeTorrent` option
  yet; use the API.
- **Usenet.** Native mode handles torrents only. Use `arr` mode for usenet.
- **Seerr requests.** **Approve** still makes Seerr send the request to
  its Sonarr/Radarr. `SEERR_AUTO_FETCH` grabs through roarr (native),
  but Seerr does not see the result as available until Jellyfin has it.
- **Monitoring.** The missing search is periodic or manual. roarr has no
  RSS sync, no calendar and no upgrade search (cutoff). When a grabbed
  release fails, the next pass can grab the same release again.
- **Seeding.** roarr does not remove torrents after seeding. Set ratio
  and time limits in qBittorrent. With `IMPORT_TRANSFER=move`, the torrent
  cannot seed the files that roarr moved.
- **Archives.** roarr unpacks ZIP only. RAR and 7-Zip releases fail (see
  [Archives](#archives)).
- **Multi-part movies** (`CD1`/`CD2`): roarr imports only the largest
  part.
- **Numbering differences.** TMDB episode order can be different from
  scene/TVDB numbering. This applies to some anime and to shows with
  changed season splits. With TMDB-only metadata, such files can go to the
  wrong episode. Configure Sonarr for those shows, or check
  `GET /api/imports`.
- **Re-grab after a failed verification.** Re-grab removes the download
  through Sonarr/Radarr, so it does not work for a native download. Remove
  the download in the activity feed and get another release.
- **Quality comparison** uses only the release name (resolution, source,
  proper/repack). It does not read the media (codec, bitrate, audio) and
  does not use custom formats.
- **Season packs that finish while roarr is stopped:** roarr imports them
  at the next start. Torrents that you add to qBittorrent outside roarr
  are ignored, also when they are in the `roarr` category.
