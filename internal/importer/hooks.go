package importer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Hook points for features that run around an import, such as download
// verification (before) and transcoding (after). Register them once at
// startup through App.Hooks; they apply to every native import.
//
//	a.Hooks.BeforeImport(func(ctx context.Context, c importer.Candidate) error {
//		if !verified(c.Source) {
//			return importer.ErrRetryLater // keep the file pending
//		}
//		return nil
//	})
//	a.Hooks.AfterImport(func(ctx context.Context, e importer.Event) {
//		go transcode(e.Path) // do slow work in your own goroutine
//	})

// ErrRetryLater, returned (or wrapped) by a before-import hook, leaves the
// file pending; roarr tries the import again on a later pass. Any other
// error fails the file with that message.
var ErrRetryLater = errors.New("not ready to import yet")

// Candidate is a file about to be imported.
type Candidate struct {
	Title    Title  `json:"title"`
	Season   int    `json:"season,omitempty"`
	Episodes []int  `json:"episodes,omitempty"`
	Source   string `json:"source"` // download file, as roarr sees it
	Dest     string `json:"dest"`   // library path it will get
	Release  string `json:"release,omitempty"`
	Hash     string `json:"hash,omitempty"`
	// Replacing lists library files that will be removed for this one.
	Replacing []string `json:"replacing,omitempty"`
}

// Event describes a finished import.
type Event struct {
	Candidate
	Path      string    `json:"path"` // final library path of the video
	Subtitles []string  `json:"subtitles,omitempty"`
	Method    string    `json:"method"` // hardlink | copy | move
	Replaced  []string  `json:"replaced,omitempty"`
	At        time.Time `json:"at"`
}

// BeforeHook can veto or postpone an import.
type BeforeHook func(ctx context.Context, c Candidate) error

// AfterHook runs after a file is in the library and recorded, before
// Jellyfin is asked to refresh. It runs on the import loop: return quickly
// and start long work (transcoding) in a goroutine of your own.
type AfterHook func(ctx context.Context, e Event)

// Hooks holds registered hooks. The zero value is ready to use.
type Hooks struct {
	mu     sync.RWMutex
	before []BeforeHook
	after  []AfterHook
}

// BeforeImport registers a hook that runs before each file is placed.
func (h *Hooks) BeforeImport(fn BeforeHook) {
	h.mu.Lock()
	h.before = append(h.before, fn)
	h.mu.Unlock()
}

// AfterImport registers a hook that runs after each file is placed.
func (h *Hooks) AfterImport(fn AfterHook) {
	h.mu.Lock()
	h.after = append(h.after, fn)
	h.mu.Unlock()
}

func (h *Hooks) runBefore(ctx context.Context, c Candidate) (err error) {
	h.mu.RLock()
	hs := append([]BeforeHook(nil), h.before...)
	h.mu.RUnlock()
	for _, fn := range hs {
		if err := safeBefore(ctx, fn, c); err != nil {
			return err
		}
	}
	return nil
}

func safeBefore(ctx context.Context, fn BeforeHook, c Candidate) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("before-import hook panicked: %v", r)
		}
	}()
	return fn(ctx, c)
}

func (h *Hooks) runAfter(ctx context.Context, e Event) {
	h.mu.RLock()
	hs := append([]AfterHook(nil), h.after...)
	h.mu.RUnlock()
	for _, fn := range hs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("after-import hook panicked", "path", e.Path, "panic", r)
				}
			}()
			fn(ctx, e)
		}()
	}
}
