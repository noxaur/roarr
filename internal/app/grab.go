package app

import (
	"context"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/noxaur/roarr/internal/arr"
	"github.com/noxaur/roarr/internal/config"
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

// Grab downloads a release through the configured Backend. With the arr
// backend a release the *arr declines is always pushed past its decision
// engine; roarr never gives up on a grab the user asked for. With the
// native backend it goes straight to qBittorrent and roarr imports it.
func (a *App) Grab(ctx context.Context, g GrabRequest) (GrabResult, error) {
	var res GrabResult
	var err error
	b := a.Backend()
	if g.Kind == "movie" {
		res, err = b.GrabMovie(ctx, g)
	} else {
		res, err = b.GrabEpisodes(ctx, g)
	}
	switch {
	case err != nil:
		slog.Warn("grab failed", "kind", g.Kind, "title", g.Release.Title, "backend", b.Name(), "force", g.Force, "err", err)
	case !res.OK:
		slog.Warn("grab declined", "kind", g.Kind, "title", g.Release.Title, "message", res.Message, "rejections", res.Rejections)
	default:
		slog.Info("grab ok", "kind", g.Kind, "title", g.Release.Title, "via", res.Via, "hash", res.Hash, "message", res.Message)
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
		// Sonarr fills in a new show's episodes in a background refresh.
		// Matching the release before that sees no episodes, so a pack
		// would get no watch-first job and nothing would track it.
		id := g.SeriesID
		return awaitEpisodes(ctx, func(ctx context.Context) (*SeriesDetail, error) { return a.Series(ctx, id, true) })
	}
	return a.Series(ctx, g.SeriesID, true)
}

// How long a grab waits for Sonarr to list a just-added show's episodes.
var (
	episodeWait = 30 * time.Second
	episodePoll = time.Second
)

// awaitEpisodes fetches the series until it lists episodes or episodeWait
// runs out, and returns the last fetch either way.
func awaitEpisodes(ctx context.Context, fetch func(context.Context) (*SeriesDetail, error)) (*SeriesDetail, error) {
	deadline := time.Now().Add(episodeWait)
	for {
		d, err := fetch(ctx)
		if err != nil || hasEpisodes(d) || !time.Now().Before(deadline) {
			return d, err
		}
		select {
		case <-ctx.Done():
			return d, nil
		case <-time.After(episodePoll):
		}
	}
}

func hasEpisodes(d *SeriesDetail) bool {
	for _, s := range d.Seasons {
		if len(s.Episodes) > 0 {
			return true
		}
	}
	return false
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
		// Sonarr never queues these, so the watch-first job is the only
		// thing that tracks and imports it, even when no episode matched.
		isPack = true
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
		a.pushTarget(ctx, c.Sonarr.Client, "Sonarr", g.Release, &push)
		decisions, err := c.Sonarr.Push(ctx, push)
		if err != nil {
			return GrabResult{}, err
		}
		bypassed := false
		if ok, _ := approved(decisions); !ok {
			decisions, err = a.withAnyQuality(ctx, c.Sonarr.Client,
				func(ctx context.Context) (int, error) {
					s, err := c.Sonarr.Series(ctx, d.ID)
					return s.QualityProfile, err
				},
				func(ctx context.Context, id int) error { return c.Sonarr.SetQualityProfile(ctx, d.ID, id) },
				func(ctx context.Context) ([]arr.PushDecision, error) { return c.Sonarr.Push(ctx, push) })
			if err != nil {
				return GrabResult{}, err
			}
			bypassed = true
		}
		forced := false
		if ok, rej := approved(decisions); !ok {
			// Not a quality problem (release profile, blocklist, file
			// already on disk, ...): grab it the way Sonarr's own "grab
			// anyway" does.
			q, extra := sonarrSearchFor(d, covers, wanted)
			if q == nil {
				return a.declined(res, "Sonarr declined this release", rej, g, c), nil
			}
			if err := a.grabAnyway(ctx, c.Sonarr.Client, q, extra, g.Release); err != nil {
				return a.declined(res, "Sonarr declined this release and could not be made to take it ("+err.Error()+")", rej, g, c), nil
			}
			forced = true
		}
		if !forced && !confirmGrab(ctx, c.Sonarr.Client, g.Release.Title) {
			res.Message = "Sonarr accepted the release but its download client did not take it (see Sonarr → System → Logs)"
			res.CanForce = c.QBit != nil && g.Release.Protocol != "usenet"
			return res, nil
		}
		res.OK, res.Via, res.Hash = true, "sonarr", strings.ToLower(g.Release.InfoHash)
		res.Message = "Sonarr is downloading it."
		switch {
		case forced:
			res.Message = "Sonarr is downloading it (all Sonarr checks skipped)."
		case bypassed:
			res.Message = "Sonarr is downloading it (quality profile bypassed)."
		}
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
	a.pushTarget(ctx, c.Radarr.Client, "Radarr", g.Release, &push)
	decisions, err := c.Radarr.Push(ctx, push)
	if err != nil {
		return GrabResult{}, err
	}
	bypassed := false
	if ok, _ := approved(decisions); !ok {
		decisions, err = a.withAnyQuality(ctx, c.Radarr.Client,
			func(ctx context.Context) (int, error) {
				m, err := c.Radarr.Movie(ctx, g.MovieID)
				return m.QualityProfileID, err
			},
			func(ctx context.Context, id int) error { return c.Radarr.SetQualityProfile(ctx, g.MovieID, id) },
			func(ctx context.Context) ([]arr.PushDecision, error) { return c.Radarr.Push(ctx, push) })
		if err != nil {
			return GrabResult{}, err
		}
		bypassed = true
	}
	forced := false
	if ok, rej := approved(decisions); !ok {
		q := url.Values{"movieId": {strconv.Itoa(g.MovieID)}}
		if err := a.grabAnyway(ctx, c.Radarr.Client, q, map[string]any{"movieId": g.MovieID}, g.Release); err != nil {
			return a.declined(res, "Radarr declined this release and could not be made to take it ("+err.Error()+")", rej, g, c), nil
		}
		forced = true
	}
	if !forced && !confirmGrab(ctx, c.Radarr.Client, g.Release.Title) {
		res.Message = "Radarr accepted the release but its download client did not take it (see Radarr → System → Logs)"
		res.CanForce = c.QBit != nil && g.Release.Protocol != "usenet"
		return res, nil
	}
	res.OK, res.Via, res.Hash, res.Message = true, "radarr", strings.ToLower(g.Release.InfoHash), "Radarr is downloading it."
	switch {
	case forced:
		res.Message = "Radarr is downloading it (all Radarr checks skipped)."
	case bypassed:
		res.Message = "Radarr is downloading it (quality profile bypassed)."
	}
	return res, nil
}

func (a *App) declined(res GrabResult, msg string, rej []string, g GrabRequest, c *Clients) GrabResult {
	res.Message = msg
	res.Rejections = rej
	res.CanForce = c.QBit != nil && g.Release.Protocol != "usenet"
	return res
}

// sonarrSearchFor picks the narrowest Sonarr interactive search that will
// return the release: the episode for a single episode, the (first) season
// for packs.
func sonarrSearchFor(d *SeriesDetail, covers, wanted []int) (url.Values, map[string]any) {
	ids := covers
	if len(ids) == 0 {
		ids = wanted
	}
	if len(ids) == 0 {
		return nil, nil
	}
	extra := map[string]any{"seriesId": d.ID, "episodeIds": ids}
	if len(ids) == 1 {
		return url.Values{"episodeId": {strconv.Itoa(ids[0])}}, extra
	}
	season := -1
	for _, s := range d.Seasons {
		for _, e := range s.Episodes {
			for _, id := range ids {
				if id == e.ID && (season < 0 || e.Season < season) {
					season = e.Season
				}
			}
		}
	}
	if season < 0 {
		return nil, nil
	}
	return url.Values{"seriesId": {strconv.Itoa(d.ID)}, "seasonNumber": {strconv.Itoa(season)}}, extra
}

// grabAnyway is the last bypass step. A pushed release always goes through
// the decision engine, but a release from the *arr's own interactive search
// can be grabbed from its cache regardless of rejections — what the "grab
// anyway" button in Sonarr/Radarr does. So roarr runs that search, finds the
// same release among the results and grabs it from the cache.
func (a *App) grabAnyway(ctx context.Context, c *arr.Client, q url.Values, extra map[string]any, r Release) error {
	results, err := c.InteractiveSearch(ctx, q)
	if err != nil {
		return err
	}
	hit, ok := arr.MatchCached(results, r.GUID, r.InfoHash, r.Title, r.Size)
	if !ok {
		return fmt.Errorf("%s's own indexers did not return this release", c.Service)
	}
	slog.Info("grabbing release past the decision engine", "service", c.Service, "title", hit.Title, "rejections", hit.Rejections)
	return c.GrabCached(ctx, hit, extra)
}

// withAnyQuality runs push with the title moved to the permissive bypass
// profile and moves it back afterwards. The *arr hands an approved push to
// its download client inside the push request, and imports do not check
// the profile's allowed qualities, so the original profile is back in place
// before anything else looks at it.
func (a *App) withAnyQuality(ctx context.Context, c *arr.Client, current func(context.Context) (int, error), set func(context.Context, int) error, push func(context.Context) ([]arr.PushDecision, error)) ([]arr.PushDecision, error) {
	a.bypassMu.Lock()
	defer a.bypassMu.Unlock()
	orig, err := current(ctx)
	if err != nil {
		return nil, err
	}
	anyID, err := c.EnsureBypassProfile(ctx)
	if err != nil {
		return nil, err
	}
	if orig != anyID {
		if err := set(ctx, anyID); err != nil {
			return nil, fmt.Errorf("switching to the %q profile: %w", arr.BypassProfileName, err)
		}
		defer func() {
			rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
			defer cancel()
			if err := set(rctx, orig); err != nil {
				slog.Error("could not restore quality profile", "service", c.Service, "profile", orig, "err", err)
			}
		}()
	}
	return push(ctx)
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

// pushTarget fills the indexer id and download link of a push for one *arr.
//
// The indexer id maps the Prowlarr indexer to the matching indexer inside
// Sonarr/Radarr (synced indexers point at {prowlarr}/{id}/), so seed ratio
// and priority settings apply to pushed releases.
//
// The download link is Prowlarr's proxy link, built from the address roarr
// used to reach Prowlarr. The *arr may know Prowlarr under another name
// (roarr on a different Docker network, or via localhost), so the link's
// origin is swapped for the one the *arr itself uses for Prowlarr.
func (a *App) pushTarget(ctx context.Context, c *arr.Client, appName string, r Release, p *arr.PushRelease) {
	var arrProwlarr string
	if ixs, err := c.Indexers(ctx); err == nil {
		for _, ix := range ixs {
			base := ix.FieldString("baseUrl")
			m := prowlarrIDRe.FindStringSubmatch(base)
			if m == nil {
				continue
			}
			if r.IndexerID != 0 && m[1] == strconv.Itoa(r.IndexerID) {
				p.IndexerID = ix.ID
				arrProwlarr = base
				break
			}
			if arrProwlarr == "" {
				arrProwlarr = base
			}
		}
	}
	pc := a.C().Prowlarr
	if pc == nil {
		return
	}
	if arrProwlarr == "" {
		if apps, err := pc.Applications(ctx); err == nil {
			for _, ap := range apps {
				if ap.Implementation == appName {
					arrProwlarr = ap.FieldString("prowlarrUrl")
					break
				}
			}
		}
	}
	p.DownloadURL = swapOrigin(p.DownloadURL, pc.Base, arrProwlarr)
}

// swapOrigin replaces link's scheme and host with to's when link points at
// the Prowlarr roarr talks to (from).
func swapOrigin(link, from, to string) string {
	if link == "" || to == "" {
		return link
	}
	lu, err1 := url.Parse(link)
	fu, err2 := url.Parse(from)
	tu, err3 := url.Parse(config.NormalizeURL(to))
	if err1 != nil || err2 != nil || err3 != nil || tu.Host == "" {
		return link
	}
	if !strings.EqualFold(lu.Host, fu.Host) || strings.EqualFold(lu.Host, tu.Host) {
		return link
	}
	lu.Scheme, lu.Host = tu.Scheme, tu.Host
	return lu.String()
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
	if category == "" {
		slog.Warn("no enabled qBittorrent download client in the *arr: the torrent is added without its category and will not be imported automatically", "service", src.Service)
	}
	return a.addToQBit(ctx, qbit.Add{Category: category, Paused: paused}, r)
}

// addToQBit fetches the release (or uses its magnet) and adds it to
// qBittorrent with add's category, save path and paused state. It returns
// the info-hash once qBittorrent lists the torrent. A torrent qBittorrent
// already has is reused.
func (a *App) addToQBit(ctx context.Context, add qbit.Add, r Release) (string, error) {
	c := a.C()
	if c.QBit == nil {
		return "", fmt.Errorf("qbittorrent %w (set QBIT_URL / QBIT_PASSWORD)", ErrNotConfigured)
	}
	category, paused := add.Category, add.Paused
	hash := strings.ToLower(r.InfoHash)
	link := r.DownloadURL
	if link == "" {
		link = r.MagnetURL
	}
	if link == "" {
		return "", errors.New("this release has no download link")
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
			h, err := torrent.InfoHash(body)
			if err != nil {
				// An indexer error page or a login redirect, not a torrent.
				return "", fmt.Errorf("the download link did not return a torrent file (%v)", err)
			}
			add.Torrent, hash = body, h
		}
	}
	slog.Info("adding torrent to qbittorrent", "qbit", c.QBit.Base, "category", category, "hash", hash, "paused", paused, "file", len(add.Torrent) > 0)
	if err := c.QBit.AddTorrent(ctx, add); err != nil {
		if !errors.Is(err, qbit.ErrRejected) || hash == "" {
			return "", err
		}
		// Already in qBittorrent (an earlier grab): reuse it.
		ts, terr := c.QBit.Torrents(ctx, hash)
		if terr != nil || len(ts) == 0 {
			return "", err
		}
		if category != "" && ts[0].Category != category {
			_ = c.QBit.SetCategory(ctx, hash, category)
		}
		if !paused {
			_ = c.QBit.Start(ctx, hash)
		}
		return hash, nil
	}
	// Adding is asynchronous for magnets and links; make sure it landed.
	if hash != "" {
		deadline := time.Now().Add(6 * time.Second)
		for {
			if ts, err := c.QBit.Torrents(ctx, hash); err != nil || len(ts) > 0 {
				break
			}
			if time.Now().After(deadline) {
				return "", errors.New("qBittorrent accepted the torrent but it never showed up in its list")
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(300 * time.Millisecond):
			}
		}
	}
	return hash, nil
}

var btihRe = regexp.MustCompile(`(?i)xt=urn:btih:([0-9a-z]{32,40})`)

// magnetHash returns the lowercase hex info-hash of a magnet link, whether it
// carries the 40 character hex form or the 32 character base32 form.
func magnetHash(m string) string {
	s := btihRe.FindStringSubmatch(m)
	if s == nil {
		return ""
	}
	h := s[1]
	switch len(h) {
	case 40:
		if _, err := hex.DecodeString(h); err == nil {
			return strings.ToLower(h)
		}
	case 32:
		if b, err := base32.StdEncoding.DecodeString(strings.ToUpper(h)); err == nil {
			return hex.EncodeToString(b)
		}
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
