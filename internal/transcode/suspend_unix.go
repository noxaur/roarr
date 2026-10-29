//go:build !windows

package transcode

import (
	"os"
	"syscall"
)

type execProc struct{ p *os.Process }

// Suspend stops the process where it is; Resume lets it carry on.
func (e execProc) Suspend() error { return e.p.Signal(syscall.SIGSTOP) }
func (e execProc) Resume() error  { return e.p.Signal(syscall.SIGCONT) }
