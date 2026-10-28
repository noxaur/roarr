//go:build !windows

package transcode

import (
	"path/filepath"
	"strconv"
	"syscall"
)

// setNice lowers the priority of a process and, on Linux, of the threads it
// already started (niceness is per thread there; later threads inherit).
func setNice(pid, nice int) {
	_ = syscall.Setpriority(syscall.PRIO_PROCESS, pid, nice)
	tasks, _ := filepath.Glob("/proc/" + strconv.Itoa(pid) + "/task/*")
	for _, t := range tasks {
		if tid, err := strconv.Atoi(filepath.Base(t)); err == nil && tid != pid {
			_ = syscall.Setpriority(syscall.PRIO_PROCESS, tid, nice)
		}
	}
}
