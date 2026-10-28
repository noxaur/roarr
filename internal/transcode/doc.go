// Package transcode converts finished downloads to AV1 with ffmpeg.
//
// Files are queued, encoded to a temporary file next to the original,
// checked with ffprobe and only then swapped in with an atomic rename. The
// original is never touched before the result has been verified, and is kept
// when the result is not clearly smaller.
package transcode
