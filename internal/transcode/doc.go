// Package transcode converts finished downloads to AV1 with ffmpeg.
//
// Files are queued, encoded to a temporary file next to the original,
// checked with ffprobe and only then swapped in: the original is moved aside,
// the result is linked into its place without overwriting, and the original
// is deleted last. A swap cut short by a crash is repaired at the next start.
// The original is never touched before the result has been verified, and is
// kept when the result is not clearly smaller. docs/transcode.md has the
// reasons.
package transcode
