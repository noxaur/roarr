# Verify

`internal/verify` judges whether a finished download is real, watchable
media and what its release name says it is. `internal/app/verify.go` follows
the downloads Sonarr and Radarr queue and acts on the verdict. A native
download is verified when its first file is imported; the reasons are in
[native-import.md](native-import.md#hook-points).

## What each check decides

| Check | Decides | Needs |
| --- | --- | --- |
| payload | The file list holds a video and nothing executable, not only archives, samples or trailers | the file list |
| streams | ffprobe can read the file and it has a video stream | ffprobe |
| duration | The length fits the expected runtime | ffprobe |
| picture | Frames sampled across the file are real, changing images | ffmpeg |
| claims | Resolution, codec, HDR, bit depth, audio and subtitles named in the release exist in the file | ffprobe, and ffmpeg to count subtitle cues |
| language | The audio is in a language the user picked for the title | ffprobe |
| sublanguage | Full subtitles in a language the user needs are there, embedded or as a sidecar | ffprobe, and ffmpeg to count subtitle cues |

- The payload check runs first and alone because it needs no access to the
  files. An executable is a failure even when roarr cannot see the disk.
- One file stands for the download: the first episode of a pack, the largest
  file of a movie. A pack is judged as soon as that episode is complete, so
  a fake is caught long before the rest has downloaded.
- Duration: a known runtime decides alone, by ratio, so a one-minute short
  that runs its length passes. The flat floors (under two minutes is a
  sample) apply only when the runtime is unknown.
- Picture: frames come from one random point in each equal slice of the
  middle of the file, so they never bunch up. Dark scenes are normal; black,
  flat or identical frames throughout are not.
- A subtitle claim fails only when the absence is certain: every track is
  tagged and no subtitle file in the download is left unexplained.

- Language: it fails only when every audio track is tagged and none is a
  picked language. An untagged track is usually the original audio, so it
  says nothing either way, and the check stays silent; warning on it would
  flag most single-track files.
- Subtitle language: forced and empty tracks do not count, since they do
  not subtitle the whole film. It fails only when every track is tagged and
  every subtitle file in the download is matched; a hardsubbed file without
  tracks is not judged, because the burned-in subtitles may be the wanted
  ones.

## Audio and subtitle languages

A title has two lists: audio languages, any of which will do, and subtitle
languages, one of which must be there (empty needs none). The defaults are
`Prefs.Languages` and `Prefs.SubtitleLanguages`. A title's own pick lives
in `languages.json`, a show by TVDB id and a movie by TMDB id, the ids its
page, the *arr queue and a native job all know. Each list is picked or reset
on its own, so `null` keeps the default and `[]` is a pick. The same lists
reject releases whose names tag only other languages before a grab and fail
the file after it, so the replacement search already skips what failed. One
set of lists for both was chosen over a separate verify setting: a language
you would reject in a file is one you would skip in a name.

The two lists stay independent. Making subtitles needed only when the audio
is not in a picked language (anime in Japanese with English subtitles) was
rejected for now: it is one more rule to explain, and picking Japanese audio
plus English subtitles for that show already gets it.

A per-title pick lives in its own file rather than in settings, because the
Admin page saves the whole settings object and would overwrite a pick made
on a title page while Admin was open.

## A failed download is replaced at once

A `fail` blocklists the release, removes the download and anything it
imported, and grabs the next best, with no one asked. The switch that made
this optional (`VERIFY_AUTO_REGRAB`) is gone: a fake or a download in the
wrong language is never what was asked for, and waiting for a person only
delays the right file. A recheck someone starts by hand does not re-grab;
they decide what to do with its verdict.

## A check that could not run is not a pass

Each check, and the verdict, is `pass`, `warn`, `fail` or `skip`. `skip`
means the check could not run: no ffprobe or ffmpeg, the file not visible to
roarr, or a cancelled run.

The verdict is the worst of the checks that ran. If none of them warns or
fails and any check was skipped, the verdict is `skip`, and its summary
names what was not checked. Counting a skipped check as a pass was
rejected: without ffmpeg no frame is looked at, and "looks like real media"
would be a claim nobody tested. A warning or failure still wins over a
skip, because what was checked is known to be wrong.

A `skip` before the download is complete is tried again when it is: the
first episode may still sit in the download client's incomplete folder.

## One probe, shared with transcode

`RunProbe` and `Probe` are the only ffprobe invocation and the only parsed
model of a file. `internal/transcode` imports them rather than keeping its
own, which it once did: the two copies had come to disagree on what a video
stream is, on Dolby Vision and on the duration, and only one of them
honoured `FFMPEG_PATH` / `FFPROBE_PATH`. The model lives here because verify
reads far more of it (tags, languages, dispositions). A third package for
the shared part was rejected as the larger change: the stream type's methods
depend on verify's language tables.

The shared rules, and why each is the stricter of the two that existed:

- Real video is a video stream that is neither flagged `attached_pic` nor a
  still-image codec. Cover art is not always flagged, and re-encoding a
  cover as AV1, or taking it for the picture, is wrong either way.
- Dolby Vision is any stream side data that names it, not one exact label.
  Missing it means a conversion that drops the Dolby layer.
- Duration is the container's, else the longest stream's, from its field or
  Matroska's `DURATION` tag.
