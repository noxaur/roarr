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
