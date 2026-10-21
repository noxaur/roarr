package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/noxaur/roarr/internal/arr"
	"github.com/noxaur/roarr/internal/httpx"
	"github.com/noxaur/roarr/internal/qbit"
	"github.com/noxaur/roarr/internal/release"
)

// ActivityItem is one download in the unified activity feed.
type ActivityItem struct {
	ID         string    `json:"id"` // "sonarr:<downloadId>" | "radarr:<downloadId>"
	Source     string    `json:"source"`
	QueueIDs   []int     `json:"queueIds"`
	Kind       string    `json:"kind"`
	Title      string    `json:"title"`
	Subtitle   string    `json:"subtitle"`
	Release    string    `json:"release"`
	Poster     string    `json:"poster,omitempty"`
	Href       string    `json:"href,omitempty"`
	Status     string    `json:"status"` // downloading | queued | paused | importing | warning | failed | stalled
	Progress   float64   `json:"progress"`
	Size       int64     `json:"size"`
	SizeLeft   int64     `json:"sizeLeft"`
	Speed      int64     `json:"speed,omitempty"`
	ETA        string    `json:"eta,omitempty"`
	Messages   []string  `json:"messages,omitempty"`
	DownloadID string    `json:"downloadId,omitempty"`
	Client     string    `json:"client,omitempty"`
	Watch      *WatchJob `json:"watch,omitempty"`
}

// Activity merges the Sonarr and Radarr queues (one row per download, not
// per episode), live speeds from qBittorrent, and watch-first progress.
func (a *App) Activity(ctx context.Context) ([]ActivityItem, map[string]string) {
	c := a.C()
	var (
		wg          sync.WaitGroup
		tvQ, movieQ []arr.QueueRecord
		errs        = map[string]string{}
		mu          sync.Mutex
	)
	fetch := func(name string, cl *arr.Client, include string, out *[]arr.QueueRecord) {
		defer wg.Done()
		q, err := cl.Queue(ctx, include)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			errs[name] = err.Error()
			return
		}
		*out = q
	}
	if c.Sonarr != nil {
		wg.Add(1)
		go fetch("sonarr", c.Sonarr.Client, "includeSeries,includeEpisode", &tvQ)
	}
	if c.Radarr != nil {
		wg.Add(1)
		go fetch("radarr", c.Radarr.Client, "includeMovie", &movieQ)
	}
	wg.Wait()

	groups := map[string]*ActivityItem{}
	episodes := map[string][][2]int{}
	var order []string
	add := func(source string, r arr.QueueRecord) {
		id := r.DownloadID
		if id == "" {
			id = fmt.Sprintf("q%d", r.ID)
		}
		key := source + ":" + strings.ToLower(id)
		g, ok := groups[key]
		if !ok {
			g = &ActivityItem{ID: key, Source: source, Release: r.Title, DownloadID: strings.ToLower(r.DownloadID), Client: r.DownloadClient, Status: queueStatus(r)}
			if r.Series != nil {
				g.Kind, g.Title, g.Poster, g.Href = "tv", r.Series.Title, arr.Poster(r.Series.Images), fmt.Sprintf("/tv/%d", r.Series.ID)
			} else if r.Movie != nil {
				g.Kind, g.Title, g.Poster, g.Href = "movie", r.Movie.Title, arr.Poster(r.Movie.Images), fmt.Sprintf("/movie/%d", r.Movie.ID)
				if r.Movie.Year > 0 {
					g.Subtitle = fmt.Sprint(r.Movie.Year)
				}
			} else {
				g.Kind, g.Title = source, r.Title
			}
			g.Size, g.SizeLeft, g.ETA = int64(r.Size), int64(r.SizeLeft), r.TimeLeft
			for _, m := range r.StatusMessages {
				g.Messages = append(g.Messages, m.Messages...)
			}
			if r.ErrorMessage != "" {
				g.Messages = append(g.Messages, r.ErrorMessage)
			}
			groups[key] = g
			order = append(order, key)
		}
		g.QueueIDs = append(g.QueueIDs, r.ID)
		if r.Episode != nil {
			episodes[key] = append(episodes[key], [2]int{r.Episode.SeasonNumber, r.Episode.EpisodeNumber})
		}
	}
	for _, r := range tvQ {
		add("sonarr", r)
	}
	for _, r := range movieQ {
		add("radarr", r)
	}

	// Live speed + exact progress from qBittorrent when we can see it.
	var hashes []string
	for _, k := range order {
		if h := groups[k].DownloadID; len(h) == 40 {
			hashes = append(hashes, h)
		}
	}
	live := map[string]qbit.Torrent{}
	if c.QBit != nil && len(hashes) > 0 {
		if ts, err := c.QBit.Torrents(ctx, hashes...); err == nil {
			for _, t := range ts {
				live[strings.ToLower(t.Hash)] = t
			}
		}
	}
	watch := map[string]WatchJob{}
	for _, j := range a.Watch.Jobs() {
		if j.Hash != "" {
			watch[j.Hash] = j
		}
	}

	out := make([]ActivityItem, 0, len(order))
	for _, k := range order {
		g := groups[k]
		if g.Size > 0 {
			g.Progress = 1 - float64(g.SizeLeft)/float64(g.Size)
		}
		if t, ok := live[g.DownloadID]; ok {
			g.Progress, g.Speed = t.Progress, t.DLSpeed
		}
		if j, ok := watch[g.DownloadID]; ok {
			jj := j
			g.Watch = &jj
		}
		if g.Kind == "tv" {
			// Sonarr maps multi-season packs to only part of what they hold,
			// so describe those from the release title instead.
			switch release.Classify(release.Parse(g.Release)) {
			case release.KindMultiSeason, release.KindComplete:
				g.Subtitle = describeRelease(g.Release)
			default:
				g.Subtitle = describeEpisodes(episodes[k], g.Release)
			}
		}
		if g.Size == 0 && g.Watch != nil {
			for _, f := range g.Watch.Files {
				g.Size += f.Size
				g.SizeLeft += int64(float64(f.Size) * (1 - f.Progress))
			}
		}
		out = append(out, *g)
	}
	// Watch-first jobs whose download already left the queue still show
	// until they are done, so the per-episode import trail stays visible.
	for _, j := range a.Watch.Jobs() {
		if j.State == "done" || j.State == "failed" {
			continue
		}
		seen := false
		for _, g := range out {
			if g.DownloadID == j.Hash && j.Hash != "" {
				seen = true
			}
		}
		if !seen {
			jj := j
			it := ActivityItem{ID: "watch:" + JobKey(j), Source: "sonarr", Kind: "tv", Title: j.SeriesTitle, Subtitle: describeRelease(j.Title), Release: j.Title, Status: "downloading", Progress: j.Progress, Speed: j.Speed, Href: fmt.Sprintf("/tv/%d", j.SeriesID), DownloadID: j.Hash, Watch: &jj}
			for _, f := range j.Files {
				it.Size += f.Size
				it.SizeLeft += int64(float64(f.Size) * (1 - f.Progress))
			}
			if it.Poster == "" {
				if li, ok := a.Library.Find(fmt.Sprintf("tv:%d", j.SeriesID)); ok {
					it.Poster = li.Poster
				}
			}
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i].Status) < rank(out[j].Status) })
	return out, errs
}

func rank(s string) int {
	switch s {
	case "downloading":
		return 0
	case "importing":
		return 1
	case "warning", "failed", "stalled":
		return 2
	case "queued":
		return 3
	}
	return 4
}

func queueStatus(r arr.QueueRecord) string {
	st := strings.ToLower(r.Status)
	tds := strings.ToLower(r.TrackedDownloadStatus)
	state := strings.ToLower(r.TrackedDownloadState)
	switch {
	case tds == "error" || st == "failed" || state == "failed" || state == "failedpending":
		return "failed"
	case state == "importpending" || state == "importing" || state == "importblocked":
		if tds == "warning" {
			return "warning"
		}
		return "importing"
	case tds == "warning":
		return "warning"
	case st == "paused":
		return "paused"
	case st == "queued" || st == "delay":
		return "queued"
	case st == "downloading":
		return "downloading"
	case st == "completed":
		return "importing"
	}
	return st
}

func describeEpisodes(eps [][2]int, title string) string {
	if len(eps) == 0 {
		return describeRelease(title)
	}
	sort.Slice(eps, func(i, j int) bool {
		if eps[i][0] != eps[j][0] {
			return eps[i][0] < eps[j][0]
		}
		return eps[i][1] < eps[j][1]
	})
	if len(eps) == 1 {
		return fmt.Sprintf("S%02dE%02d", eps[0][0], eps[0][1])
	}
	seasons := map[int]bool{}
	for _, e := range eps {
		seasons[e[0]] = true
	}
	if len(seasons) == 1 {
		return fmt.Sprintf("Season %d · %d episodes", eps[0][0], len(eps))
	}
	return fmt.Sprintf("%d seasons · %d episodes", len(seasons), len(eps))
}

func describeRelease(title string) string {
	i := release.Parse(title)
	switch release.Classify(i) {
	case release.KindComplete:
		return "Complete series"
	case release.KindMultiSeason:
		return fmt.Sprintf("Seasons %d–%d", i.Seasons[0], i.Seasons[len(i.Seasons)-1])
	case release.KindSeason:
		return fmt.Sprintf("Season %d", i.Seasons[0])
	case release.KindEpisode:
		if len(i.Seasons) > 0 && len(i.Episodes) > 0 {
			return fmt.Sprintf("S%02dE%02d", i.Seasons[0], i.Episodes[0])
		}
	}
	return ""
}

// RemoveActivity removes a download from the *arr queue and the client. It
// always ends with the download gone from roarr's own list, even when the
// *arr no longer knows it or the torrent is already deleted.
func (a *App) RemoveActivity(ctx context.Context, id string, blocklist bool) error {
	c := a.C()
	items, _ := a.Activity(ctx)
	for _, it := range items {
		if it.ID != id {
			continue
		}
		if len(it.QueueIDs) > 0 {
			var cl *arr.Client
			switch it.Source {
			case "sonarr":
				if c.Sonarr != nil {
					cl = c.Sonarr.Client
				}
			case "radarr":
				if c.Radarr != nil {
					cl = c.Radarr.Client
				}
			}
			if cl == nil {
				return fmt.Errorf("%s %w", it.Source, ErrNotConfigured)
			}
			// One delete per queue record; the first removes the torrent.
			for i, qid := range it.QueueIDs {
				err := cl.RemoveQueue(ctx, qid, i == 0, blocklist && i == 0)
				var he *httpx.Error
				if err != nil && i == 0 && !(errors.As(err, &he) && he.Status == http.StatusNotFound) {
					return err
				}
			}
		}
		// Rows with no queue entry (watch-first jobs the *arr never queued)
		// and torrents the *arr left behind: delete from the client directly.
		if it.DownloadID != "" && c.QBit != nil {
			if err := c.QBit.Delete(ctx, it.DownloadID, true); err != nil {
				slog.Warn("could not delete torrent from qbittorrent", "hash", it.DownloadID, "err", err)
			}
		}
		if it.Watch != nil {
			a.Watch.Remove(JobKey(*it.Watch))
		}
		if it.DownloadID != "" {
			a.Watch.Remove(it.DownloadID)
		}
		return nil
	}
	// Not in the feed any more (finished or failed job): forget it anyway.
	if strings.HasPrefix(id, "watch:") {
		a.Watch.Remove(strings.TrimPrefix(id, "watch:"))
		return nil
	}
	return fmt.Errorf("download %s not found", id)
}
