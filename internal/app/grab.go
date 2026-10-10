package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/noxaur/roarr/internal/arr"
	"github.com/noxaur/roarr/internal/httpx"
	"github.com/noxaur/roarr/internal/qbit"
	"github.com/noxaur/roarr/internal/release"
	"github.com/noxaur/roarr/internal/torrent"
)

// GrabRequest asks roarr to download one release.
type GrabRequest struct {
	Kind       string  `json:"kind"`     // tv | movie
	SeriesID   int     `json:"seriesId"` // Sonarr id, 0 when not in the library yet
	TvdbID     int     `json:"tvdbId"`
	MovieID    int     `json:"movieId"` // Radarr id, 0 when not in the library yet
	TmdbID     int     `json:"tmdbId"`
	EpisodeIDs []int   `json:"episodeIds"`
	Release    Release `json:"release"`
	// Force skips the *arr decision engine and hands the release straight to
	// qBittorrent (the *arr still imports it). For "I know what I want".
	Force bool `json:"force"`
	// WatchFirst overrides the setting for this grab.
	WatchFirst *bool `json:"watchFirst,omitempty"`
}

// GrabResult reports what happened.
type GrabResult struct {
	OK         bool     `json:"ok"`
	Via        string   `json:"via,omitempty"` // sonarr | radarr | qbittorrent
	Message    string   `json:"message"`
	Rejections []string `json:"rejections,omitempty"`
	CanForce   bool     `json:"canForce,omitempty"`
	SeriesID   int      `json:"seriesId,omitempty"`
	MovieID    int      `json:"movieId,omitempty"`
	Hash       string   `json:"hash,omitempty"`
	WatchFirst bool     `json:"watchFirst,omitempty"`
}

// Grab downloads a release through Sonarr/Radarr.
func (a *App) Grab(ctx context.Context, g GrabRequest) (GrabResult, error) {
	var res GrabResult
	var err error
	if g.Kind == "movie" {
		res, err = a.grabMovie(ctx, g)
	} else {
		res, err = a.grabEpisodes(ctx, g)
	}
	if err == nil && res.OK {
		a.Library.Refresh(ctx)
		a.forget(g)
	}
	return res, err
}

func (a *App) forget(g GrabRequest) {
	a.detailMu.Lock()
	defer a.detailMu.Unlock()
	if g.SeriesID != 0 {
		delete(a.details, fmt.Sprintf("tv:%d", g.SeriesID))
	}
}

// ensureSeries returns the Sonarr series id, adding the show if needed.
func (a *App) ensureSeries(ctx context.Context, g *GrabRequest) (*SeriesDetail, error) {
	c := a.C()
	if c.Sonarr == nil {
		return nil, fmt.Errorf("sonarr %w", ErrNotConfigured)
	}
	if g.SeriesID == 0 && g.TvdbID != 0 {
		if it, ok := a.Library.FindTvdb(g.TvdbID); ok {
			g.SeriesID = it.ID
		}
	}
	if g.SeriesID == 0 {
		if g.TvdbID == 0 {
			return nil, errors.New("need a TVDB id to add this show to Sonarr")
		}
		profile, root, err := defaults(ctx, c.Sonarr.Client, "1080")
		if err != nil {
			return nil, err
		}
		preview, _ := a.SeriesByTvdb(ctx, g.TvdbID, 0)
		s, err := c.Sonarr.Add(ctx, arr.AddSeries{TvdbID: g.TvdbID, QualityProfileID: profile, RootFolder: root, Monitor: "none", Anime: preview != nil && preview.Anime})
		if err != nil {
			return nil, fmt.Errorf("adding show to Sonarr: %w", err)
		}
		g.SeriesID = s.ID
		slog.Info("added series to sonarr", "title", s.Title, "id", s.ID)
		a.Library.Refresh(ctx)
	}
	return a.Series(ctx, g.SeriesID, true)
}

// defaults picks a quality profile (preferring one matching hint) and the
// first root folder.
func defaults(ctx context.Context, c *arr.Client, hint string) (int, string, error) {
	profiles, err := c.QualityProfiles(ctx)
	if err != nil {
		return 0, "", err
	}
	roots, err := c.RootFolders(ctx)
	if err != nil {
		return 0, "", err
	}
	if len(profiles) == 0 || len(roots) == 0 {
		return 0, "", fmt.Errorf("%s needs at least one quality profile and root folder", c.Service)
	}
	pid := profiles[0].ID
	for _, p := range profiles {
		if strings.Contains(p.Name, hint) {
			pid = p.ID
			break
		}
	}
	return pid, roots[0].Path, nil
}

// realEpisodes maps synthetic IDs (show was not in Sonarr) to real ones and
// recomputes which episodes the release covers.
func realEpisodes(d *SeriesDetail, g GrabRequest) (wanted, covers []int) {
	byNum := map[[2]int]int{}
	for _, s := range d.Seasons {
		for _, e := range s.Episodes {
			byNum[[2]int{e.Season, e.Number}] = e.ID
		}
	}
	for _, id := range g.EpisodeIDs {
		if id < 0 {
			n := -id
			if real, ok := byNum[[2]int{n / 10000, n % 10000}]; ok {
				wanted = append(wanted, real)
			}
		} else {
			wanted = append(wanted, id)
		}
	}
	covers = release.Covers(release.Parse(g.Release.Title), showForMatching(d))
	return wanted, covers
}

func (a *App) grabEpisodes(ctx context.Context, g GrabRequest) (GrabResult, error) {
	d, err := a.ensureSeries(ctx, &g)
	if err != nil {
		return GrabResult{}, err
	}
	c := a.C()
	wanted, covers := realEpisodes(d, g)
	// Monitor everything the release brings that is not on disk yet, so the
	// decision engine does not refuse it as "not monitored".
	hasFile := map[int]bool{}
	for _, s := range d.Seasons {
		for _, e := range s.Episodes {
			hasFile[e.ID] = e.HasFile
		}
	}
	toMonitor := []int{}
	seen := map[int]bool{}
	seasons := map[int]bool{}
	for _, id := range append(append([]int{}, covers...), wanted...) {
		if !hasFile[id] && !seen[id] {
			seen[id] = true
			toMonitor = append(toMonitor, id)
		}
	}
	for _, s := range d.Seasons {
		for _, e := range s.Episodes {
			for _, id := range toMonitor {
				if id == e.ID {
					seasons[e.Season] = true
				}
			}
		}
	}
	if err := c.Sonarr.MonitorEpisodes(ctx, toMonitor, true); err != nil {
		slog.Warn("could not monitor episodes", "err", err)
	}
	var seasonList []int
	for s := range seasons {
		seasonList = append(seasonList, s)
	}
	if err := c.Sonarr.EnsureSeasonsMonitored(ctx, d.ID, seasonList); err != nil {
		slog.Warn("could not monitor seasons", "err", err)
	}

	res := GrabResult{SeriesID: d.ID}
	watch := a.Settings.Get().Behaviour.WatchFirst
	if g.WatchFirst != nil {
		watch = *g.WatchFirst
	}
	isPack := len(covers) > 1 && g.Release.Protocol != "usenet"
	// Sonarr's decision engine refuses multi-season / complete-series packs
	// outright. With qBittorrent available roarr takes them directly and
	// imports every episode itself.
	info := release.Parse(g.Release.Title)
	multiSeason := len(info.Seasons) > 1 || info.Complete
	if multiSeason && !g.Force && c.QBit != nil && g.Release.Protocol != "usenet" {
		g.Force = true
		watch = true
	}

	if g.Force {
		hash, err := a.directAdd(ctx, c.Sonarr.Client, "tvCategory", g.Release, watch && isPack)
		if err != nil {
			return GrabResult{}, err
		}
		res.OK, res.Via, res.Hash = true, "qbittorrent", hash
		res.Message = "Sent straight to qBittorrent; Sonarr will import it."
	} else {
		push := pushFor(g.Release)
		push.SeriesID = d.ID
		push.TvdbID = d.TvdbID
		push.IndexerID = a.arrIndexerID(ctx, c.Sonarr.Client, g.Release.IndexerID)
		decisions, err := c.Sonarr.Push(ctx, push)
		if err != nil {
			return GrabResult{}, err
		}
		if ok, rej := approved(decisions); !ok {
			res.Message = "Sonarr declined this release"
			res.Rejections = rej
			res.CanForce = c.QBit != nil && g.Release.Protocol != "usenet"
			return res, nil
		}
		if !confirmGrab(ctx, c.Sonarr.Client, g.Release.Title) {
			res.Message = "Sonarr accepted the release but its download client did not take it (see Sonarr → System → Logs)"
			res.CanForce = c.QBit != nil && g.Release.Protocol != "usenet"
			return res, nil
		}
		res.OK, res.Via, res.Hash = true, "sonarr", strings.ToLower(g.Release.InfoHash)
		res.Message = "Sonarr is downloading it."
	}
	if watch && isPack && c.QBit != nil {
		a.Watch.Track(WatchJob{Hash: res.Hash, Title: g.Release.Title, Kind: "tv", SeriesID: d.ID, TvdbID: d.TvdbID, SeriesTitle: d.Title, Wanted: wanted, Covers: covers, AddedPaused: g.Force})
		res.WatchFirst = true
		res.Message += " Episodes will be imported one by one as they finish."
	}
	return res, nil
}

func (a *App) grabMovie(ctx context.Context, g GrabRequest) (GrabResult, error) {
	c := a.C()
	if c.Radarr == nil {
		return GrabResult{}, fmt.Errorf("radarr %w", ErrNotConfigured)
	}
	if g.MovieID == 0 && g.TmdbID != 0 {
		if it, ok := a.Library.FindTmdbMovie(g.TmdbID); ok {
			g.MovieID = it.ID
		}
	}
	if g.MovieID == 0 {
		if g.TmdbID == 0 {
			return GrabResult{}, errors.New("need a TMDB id to add this movie to Radarr")
		}
		profile, root, err := defaults(ctx, c.Radarr.Client, "1080")
		if err != nil {
			return GrabResult{}, err
		}
		m, err := c.Radarr.AddMovie(ctx, g.TmdbID, profile, root)
		if err != nil {
			return GrabResult{}, fmt.Errorf("adding movie to Radarr: %w", err)
		}
		g.MovieID = m.ID
		a.Library.Refresh(ctx)
	} else if err := c.Radarr.Monitor(ctx, g.MovieID); err != nil {
		slog.Warn("could not monitor movie", "err", err)
	}
	res := GrabResult{MovieID: g.MovieID}
	if g.Force {
		hash, err := a.directAdd(ctx, c.Radarr.Client, "movieCategory", g.Release, false)
		if err != nil {
			return GrabResult{}, err
		}
		res.OK, res.Via, res.Hash, res.Message = true, "qbittorrent", hash, "Sent straight to qBittorrent; Radarr will import it."
		return res, nil
	}
	push := pushFor(g.Release)
	push.MovieID = g.MovieID
	push.TmdbID = g.TmdbID
	push.IndexerID = a.arrIndexerID(ctx, c.Radarr.Client, g.Release.IndexerID)
	decisions, err := c.Radarr.Push(ctx, push)
	if err != nil {
		return GrabResult{}, err
	}
	if ok, rej := approved(decisions); !ok {
		res.Message = "Radarr declined this release"
		res.Rejections = rej
		res.CanForce = c.QBit != nil && g.Release.Protocol != "usenet"
		return res, nil
	}
	if !confirmGrab(ctx, c.Radarr.Client, g.Release.Title) {
		res.Message = "Radarr accepted the release but its download client did not take it (see Radarr → System → Logs)"
		res.CanForce = c.QBit != nil && g.Release.Protocol != "usenet"
		return res, nil
	}
	res.OK, res.Via, res.Hash, res.Message = true, "radarr", strings.ToLower(g.Release.InfoHash), "Radarr is downloading it."
	return res, nil
}

// confirmGrab waits briefly for the *arr to record a "grabbed" history
// event. An approved push only means the decision engine liked it; the
// event appears once the download client actually accepted it.
func confirmGrab(ctx context.Context, c *arr.Client, title string) bool {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if ok, err := c.RecentlyGrabbed(ctx, title, time.Minute); err == nil && ok {
			return true
		} else if err != nil {
			return true // cannot verify; do not report a false failure
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(400 * time.Millisecond):
		}
	}
	return false
}

func pushFor(r Release) arr.PushRelease {
	p := arr.PushRelease{
		Title: r.Title, DownloadURL: r.DownloadURL, MagnetURL: r.MagnetURL, InfoURL: r.InfoURL, GUID: r.GUID,
		Protocol: r.Protocol, PublishDate: r.PublishDate, Size: r.Size, Indexer: r.Indexer, InfoHash: r.InfoHash,
	}
	if p.Protocol == "" {
		p.Protocol = "torrent"
	}
	if p.PublishDate.IsZero() {
		p.PublishDate = time.Now().UTC()
	}
	s, l := r.Seeders, r.Leechers
	p.Seeders, p.Leechers = &s, &l
	if p.DownloadURL == "" && p.MagnetURL != "" {
		p.DownloadURL = p.MagnetURL
	}
	return p
}

func approved(ds []arr.PushDecision) (bool, []string) {
	var rej []string
	for _, d := range ds {
		if d.Approved || (!d.Rejected && d.DownloadAllowed) {
			return true, nil
		}
		rej = append(rej, d.Rejections...)
	}
	if len(rej) == 0 {
		rej = []string{"no reason given"}
	}
	return false, rej
}

var prowlarrIDRe = regexp.MustCompile(`/(\d+)/?(?:api)?/?$`)

// arrIndexerID maps a Prowlarr indexer id to the matching indexer inside
// Sonarr/Radarr (synced indexers point at {prowlarr}/{id}/), so seed ratio
// and priority settings apply to pushed releases.
func (a *App) arrIndexerID(ctx context.Context, c *arr.Client, prowlarrID int) int {
	if prowlarrID == 0 {
		return 0
	}
	ixs, err := c.Indexers(ctx)
	if err != nil {
		return 0
	}
	for _, ix := range ixs {
		if m := prowlarrIDRe.FindStringSubmatch(ix.FieldString("baseUrl")); m != nil && m[1] == fmt.Sprint(prowlarrID) {
			return ix.ID
		}
	}
	return 0
}

// directAdd fetches the release and adds it to qBittorrent in the *arr's
// category, so the *arr picks it up for import. For packs it starts paused
// so watch-first priorities are in place before the first byte arrives.
func (a *App) directAdd(ctx context.Context, src *arr.Client, catField string, r Release, paused bool) (string, error) {
	c := a.C()
	if c.QBit == nil {
		return "", fmt.Errorf("qbittorrent %w (set QBIT_URL / QBIT_PASSWORD)", ErrNotConfigured)
	}
	category := ""
	if dcs, err := src.DownloadClients(ctx); err == nil {
		for _, d := range dcs {
			if d.Implementation == "QBittorrent" && d.Enable {
				category = d.FieldString(catField)
				break
			}
		}
	}
	add := qbit.Add{Category: category, Paused: paused}
	hash := strings.ToLower(r.InfoHash)
	link := r.DownloadURL
	if link == "" {
		link = r.MagnetURL
	}
	if strings.HasPrefix(link, "magnet:") {
		add.URL = link
		if h := magnetHash(link); h != "" {
			hash = h
		}
	} else {
		body, magnet, err := fetchTorrent(ctx, link)
		if err != nil {
			return "", err
		}
		if magnet != "" {
			add.URL = magnet
			if h := magnetHash(magnet); h != "" {
				hash = h
			}
		} else {
			add.Torrent = body
			if h, err := torrent.InfoHash(body); err == nil {
				hash = h
			}
		}
	}
	if err := c.QBit.AddTorrent(ctx, add); err != nil {
		return "", err
	}
	return hash, nil
}

var btihRe = regexp.MustCompile(`(?i)xt=urn:btih:([0-9a-f]{40})`)

func magnetHash(m string) string {
	if s := btihRe.FindStringSubmatch(m); s != nil {
		return strings.ToLower(s[1])
	}
	return ""
}

// fetchTorrent downloads a .torrent (through Prowlarr's proxy link). Some
// indexers answer with a redirect to a magnet link instead.
func fetchTorrent(ctx context.Context, link string) ([]byte, string, error) {
	var magnet string
	cl := &http.Client{
		Transport: httpx.Transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme == "magnet" {
				magnet = req.URL.String()
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", httpx.UserAgent)
	res, err := cl.Do(req)
	if err != nil {
		if magnet != "" {
			return nil, magnet, nil
		}
		return nil, "", fmt.Errorf("fetching torrent: %w", err)
	}
	defer res.Body.Close()
	if loc := res.Header.Get("Location"); strings.HasPrefix(loc, "magnet:") {
		return nil, loc, nil
	}
	if magnet != "" {
		return nil, magnet, nil
	}
	if res.StatusCode >= 300 {
		return nil, "", fmt.Errorf("fetching torrent: HTTP %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 20<<20))
	return b, "", err
}
