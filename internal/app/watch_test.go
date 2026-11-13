package app

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWatchConcurrentSavesKeepEveryJob(t *testing.T) {
	dir := t.TempDir()
	a := &App{details: map[string]cachedDetail{}}
	w := newWatchFirst(a, dir)
	// Older (smaller) snapshots take longer to write, so without
	// serialisation a stale snapshot would land last.
	orig := writeJobs
	defer func() { writeJobs = orig }()
	writeJobs = func(name string, b []byte, perm os.FileMode) error {
		time.Sleep(time.Duration(50-strings.Count(string(b), `"hash"`)) * 200 * time.Microsecond)
		return os.WriteFile(name, b, perm)
	}
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w.Track(WatchJob{Hash: fmt.Sprintf("h%02d", i), Kind: "tv"})
		}(i)
	}
	wg.Wait()
	if got := len(newWatchFirst(a, dir).Jobs()); got != 40 {
		t.Fatalf("watch.json holds %d jobs, want 40", got)
	}
}
