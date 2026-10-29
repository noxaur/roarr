//go:build windows

package transcode

import (
	"errors"
	"os"
)

// Windows has no SIGSTOP: a held job is stopped and queued again instead.
type execProc struct{ p *os.Process }

func (e execProc) Suspend() error { return errors.ErrUnsupported }
func (e execProc) Resume() error  { return errors.ErrUnsupported }
