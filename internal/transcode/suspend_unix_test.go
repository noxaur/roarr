//go:build !windows

package transcode

import (
	"context"
	"testing"
	"time"
)

// TestExecSuspend holds a real process and lets it go again.
func TestExecSuspend(t *testing.T) {
	var lines []string
	start := time.Now()
	err := ExecRunner{}.Run(context.Background(), 0, "sh", []string{"-c", "sleep 0.2; echo done"}, func(l string) { lines = append(lines, l) }, func(p Proc) {
		if err := p.Suspend(); err != nil {
			t.Error(err)
		}
		time.Sleep(400 * time.Millisecond)
		if err := p.Resume(); err != nil {
			t.Error(err)
		}
	})
	if err != nil || len(lines) != 1 || lines[0] != "done" {
		t.Fatalf("err %v, lines %q", err, lines)
	}
	if time.Since(start) < 600*time.Millisecond {
		t.Error("process ran while suspended")
	}
}

// TestExecCancelSuspended stops a held process on shutdown.
func TestExecCancelSuspended(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	err := ExecRunner{}.Run(ctx, 0, "sleep", []string{"5"}, nil, func(p Proc) {
		_ = p.Suspend()
		cancel()
	})
	if err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
