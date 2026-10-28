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
  mapping, the `library.json` store and the hooks. It makes no network
  calls.
- `internal/app/native.go`: the backend. It adds torrents, runs the loop,
  connects the activity feed and refreshes Jellyfin. The metadata code is
  in `internal/app/native_meta.go`.

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
- **Two files for one episode:** roarr keeps the larger file.
- **Samples:** `sample` in the name, or a `Sample/` folder.
- **Extras:** featurettes, trailers, behind the scenes, deleted scenes,
  interviews and similar names, or folders such as `Extras/` and
  `Featurettes/`. roarr does not download or import them.
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

Other features (download verification, AV1 transcoding) can register
hooks with `App.Hooks`. Do this one time, at startup, after `app.New`:

```go
a.Hooks.BeforeImport(func(ctx context.Context, c importer.Candidate) error {
	// c.Source: the download file, c.Dest: its library path, c.Hash: the torrent.
	if !verified(c.Source) {
		return importer.ErrRetryLater // keep the file pending; roarr tries again
	}
	return nil // or an error: the file fails with that message
})

a.Hooks.AfterImport(func(ctx context.Context, e importer.Event) {
	// e.Path: the library file, e.Subtitles, e.Method, e.Replaced, e.Title...
	go transcode(e.Path) // the loop waits for this function: return quickly
})
```

- Before-import hooks run in sequence. The first error stops the import.
- After-import hooks run after the file is in `library.json`. They run
  before the Jellyfin refresh. A hook that changes the file (for example,
  a transcode to a new extension) must call
  `a.Native.Store().Put/Forget` to keep the store correct. When the
  hook's work is done, it must ask Jellyfin to refresh.
- roarr recovers from a panic in a hook and writes it to the log.
- The hooks apply only to native imports. In arr mode, Sonarr and Radarr
  import outside roarr, so roarr cannot see the final files.

## Endpoints

| Endpoint | Change |
|---|---|
| `POST /api/grab` | The same request. In native mode the result has `"via": "roarr"`. |
| `GET /api/activity` | Adds rows with `"source": "roarr"` and `id` `roarr:<hash>`. TV rows have a `watch` object for each episode, in the same format as watch-first jobs. |
| `DELETE /api/activity/roarr:<hash>` | Removes the torrent and its data from qBittorrent and forgets the job. Library files stay, because they are hardlinks or copies. |
| `GET /api/imports` | New. Shows the native settings, all jobs with the placement and errors of each file, the tracked titles and the imported files. `?title=tv:tmdb:1399` shows the files of one title. |
| `GET /api/status` | Adds `importMode`. |
| `GET /api/tv/tvdb/{tvdb}?tmdb=`, `GET /api/movie/tmdb/{tmdb}` | When Sonarr/Radarr are not configured, these use TMDB through Seerr. Before, they returned an error. |
| `PUT /api/settings` | When the request has no `import` object, the stored import settings stay. The current UI does not know these settings yet. |

## Limitations

Native mode does not do these things yet:

- **Library list and title management.** `/api/library`, `/api/tv/{id}`,
  the episode/season menus (delete, monitor, replace) and **Manage** still
  need Sonarr/Radarr. If you do not have them, you can open and get titles
  from search, but they do not show in the library list. To delete a
  file, delete it on disk.
- **Usenet.** Native mode handles torrents only. Use `arr` mode for usenet.
- **Seerr requests.** **Approve** still makes Seerr send the request to
  its Sonarr/Radarr. `SEERR_AUTO_FETCH` grabs through roarr (native),
  but Seerr does not see the result as available until Jellyfin has it.
- **Monitoring.** roarr does not search for new episodes or upgrades by
  itself. It does not have an RSS sync, a calendar or a schedule.
- **Seeding.** roarr does not remove torrents after seeding. Set ratio
  and time limits in qBittorrent. With `IMPORT_TRANSFER=move`, the torrent
  cannot seed the files that roarr moved.
- **Archives.** roarr does not extract RAR or ZIP releases. They match
  nothing, and the job fails with "nothing in this download matches".
- **Multi-part movies** (`CD1`/`CD2`): roarr imports only the largest
  part.
- **Numbering differences.** TMDB episode order can be different from
  scene/TVDB numbering. This applies to some anime and to shows with
  changed season splits. With TMDB-only metadata, such files can go to the
  wrong episode. Configure Sonarr for those shows, or check
  `GET /api/imports`.
- **Quality comparison** uses only the release name (resolution, source,
  proper/repack). It does not read the media (codec, bitrate, audio) and
  does not use custom formats.
- **Season packs that finish while roarr is stopped:** roarr imports them
  at the next start. Torrents that you add to qBittorrent outside roarr
  are ignored, also when they are in the `roarr` category.
- **The settings UI** does not show the import settings yet. Use the
  environment variables, or `settings.json` → `import`.
