package app

import (
	"errors"
	"strconv"
	"testing"

	"github.com/noxaur/roarr/internal/arr"
)

func TestMapPath(t *testing.T) {
	spec := "/tv=/media/tv, /movies/=/media/films"
	for in, want := range map[string]string{
		"/tv/Show/S01E01.mkv":    "/media/tv/Show/S01E01.mkv",
		"/movies/A (2020)/a.mkv": "/media/films/A (2020)/a.mkv",
		"/tvshows/x.mkv":         "/tvshows/x.mkv",
		"/other/x.mkv":           "/other/x.mkv",
	} {
		if got := mapPath(spec, in); got != want {
			t.Errorf("mapPath(%q) = %q, want %q", in, got, want)
		}
	}
	if mapPath("", "/a/b") != "/a/b" {
		t.Error("empty map must not change paths")
	}
}

// historyOf serves ids (newest first) in pages like the *arr history.
func historyOf(ids ...int) func(int) ([]arr.Import, error) {
	return func(page int) ([]arr.Import, error) {
		lo, hi := (page-1)*importPageSize, page*importPageSize
		if lo > len(ids) {
			return nil, nil
		}
		var out []arr.Import
		for _, id := range ids[lo:min(hi, len(ids))] {
			out = append(out, arr.Import{ID: id, Path: "/m/" + strconv.Itoa(id) + ".mkv"})
		}
		return out, nil
	}
}

func TestNewImportsEmptyHistoryInitializesCursor(t *testing.T) {
	fresh, next, err := newImports(historyOf(), 0, false)
	if err != nil || len(fresh) != 0 || next != 0 {
		t.Fatalf("first poll: %v %d %v", fresh, next, err)
	}
	// Cursor 0 is now "seen": the first download after it is queued.
	fresh, next, _ = newImports(historyOf(1), 0, true)
	if len(fresh) != 1 || fresh[0].ID != 1 || next != 1 {
		t.Errorf("first download not queued: %v %d", fresh, next)
	}
}

func TestNewImportsFirstRunDoesNotBackfill(t *testing.T) {
	fresh, next, _ := newImports(historyOf(9, 8, 7), 0, false)
	if len(fresh) != 0 || next != 9 {
		t.Errorf("got %v, %d", fresh, next)
	}
}

func TestNewImportsPagesThroughBacklog(t *testing.T) {
	var ids []int
	for id := 650; id > 0; id-- {
		ids = append(ids, id)
	}
	fresh, next, err := newImports(historyOf(ids...), 100, true)
	if err != nil || len(fresh) != 550 || next != 650 {
		t.Fatalf("got %d fresh, next %d, err %v", len(fresh), next, err)
	}
	if fresh[0].ID != 101 || fresh[len(fresh)-1].ID != 650 {
		t.Errorf("not oldest first: %d..%d", fresh[0].ID, fresh[len(fresh)-1].ID)
	}
}

func TestNewImportsErrorKeepsCursor(t *testing.T) {
	calls := 0
	fetch := func(page int) ([]arr.Import, error) {
		if calls++; calls == 2 {
			return nil, errors.New("down")
		}
		return historyOf(func() []int {
			var ids []int
			for id := 500; id > 0; id-- {
				ids = append(ids, id)
			}
			return ids
		}()...)(page)
	}
	fresh, next, err := newImports(fetch, 10, true)
	if err == nil || len(fresh) != 0 || next != 10 {
		t.Errorf("got %v %d %v", fresh, next, err)
	}
}
