package transcode

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Runner runs external programs. Tests replace it.
type Runner interface {
	// Output runs a program and returns its stdout.
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
	// Run runs a program, calling line for each stdout line, at the given
	// niceness (0 = unchanged).
	Run(ctx context.Context, nice int, name string, args []string, line func(string)) error
}

// ExecRunner runs real programs.
type ExecRunner struct{}

// Output implements Runner.
func (ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("%w: %s", err, tail(stderr.String()))
	}
	return out, nil
}

// Run implements Runner.
func (ExecRunner) Run(ctx context.Context, nice int, name string, args []string, line func(string)) error {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	if nice > 0 {
		setNice(cmd.Process.Pid, nice)
	}
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		if line != nil {
			line(sc.Text())
		}
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: %s", err, tail(stderr.String()))
	}
	return nil
}

// tail keeps the end of a program's error output.
func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 600 {
		s = "…" + s[len(s)-600:]
	}
	return s
}
