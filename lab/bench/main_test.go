package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// sse serves the events in order, each after its delay, then holds the
// connection open like a client that never hangs up would see.
func sse(t *testing.T, events ...string) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fl := w.(http.Flusher)
		fl.Flush()
		for _, ev := range events {
			time.Sleep(100 * time.Millisecond)
			fmt.Fprint(w, ev)
			fl.Flush()
		}
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestStreamSearchTimesEachBatch(t *testing.T) {
	u := sse(t,
		`data: {"type":"plan","indexers":[{"id":1}]}`+"\n\n",
		`data: {"type":"batch","batch":{"indexer":"swiftbay","cached":true},"releases":[{"key":"a"},{"key":"b"}]}`+"\n\n",
		": ping\n\n",
		`data: {"type":"batch","batch":{"indexer":"deadpool","error":"timeout"}}`+"\n\n",
		// "a" again: a second indexer has it, so it is not a new result.
		`data: {"type":"batch","batch":{"indexer":"eztvish"},"releases":[{"key":"a"},{"key":"c"},{"key":"d"},{"key":"e"},{"key":"f"},{"key":"g"},{"key":"h"},{"key":"i"},{"key":"j"}]}`+"\n\n",
		`data: {"type":"done","ms":500,"total":10}`+"\n\n",
	)
	a := &arrival{start: time.Now()}
	cached, failed, err := streamSearch(u, a)
	all := time.Since(a.start)
	if err != nil {
		t.Fatal(err)
	}
	var r result
	a.summarize(&r)
	if cached != 1 || failed != 1 || r.Count != 10 {
		t.Errorf("cached=%d failed=%d count=%d, want 1, 1, 10", cached, failed, r.Count)
	}
	// Events land at 100 ms steps: first releases at 200, 90% at 500, done at 600.
	for _, c := range []struct {
		name     string
		got      time.Duration
		min, max int
	}{{"first", r.First, 200, 400}, {"p90", r.P90, 500, 600}, {"done", all, 600, 2000}} {
		if c.got < time.Duration(c.min)*time.Millisecond || c.got >= time.Duration(c.max)*time.Millisecond {
			t.Errorf("%s = %v, want [%d ms, %d ms)", c.name, c.got, c.min, c.max)
		}
	}
}

func TestStreamSearchReportsFailure(t *testing.T) {
	for name, events := range map[string][]string{
		"error event": {`data: {"type":"error","error":"prowlarr not configured"}` + "\n\n"},
		"bad json":    {"data: {\n\n"},
	} {
		if _, _, err := streamSearch(sse(t, events...), &arrival{start: time.Now()}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"unauthorized"}`, 401)
	}))
	defer srv.Close()
	_, _, err := streamSearch(srv.URL, &arrival{start: time.Now()})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("401 answer: err = %v", err)
	}
	// The stream closing without a done event is a failed search, not an empty one.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `data: {"type":"plan"}`+"\n\n")
	}))
	defer srv2.Close()
	if _, _, err := streamSearch(srv2.URL, &arrival{start: time.Now()}); err == nil {
		t.Error("stream without done: no error")
	}
}
