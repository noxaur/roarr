package transcode

import (
	"context"
	"fmt"

	"github.com/noxaur/roarr/internal/verify"
)

// Stream is one ffprobe stream, as internal/verify parses it.
type Stream = verify.Stream

// Info is a probed file with its size on disk.
type Info struct {
	*verify.Probe
	Size int64
}

// MainVideo returns the real video streams.
func (i Info) MainVideo() []Stream {
	var out []Stream
	for _, s := range i.Streams {
		if s.IsPicture() {
			out = append(out, s)
		}
	}
	return out
}

// Probe runs ffprobe on a file.
func Probe(ctx context.Context, r Runner, ffprobe, path string) (Info, error) {
	p, err := verify.RunProbe(ctx, r, ffprobe, path)
	if err != nil {
		return Info{}, fmt.Errorf("ffprobe: %w", err)
	}
	return Info{Probe: p}, nil
}
