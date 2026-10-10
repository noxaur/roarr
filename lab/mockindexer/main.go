// Command mockindexer is a Torznab server that imitates a handful of public
// indexers with realistic latency, failure modes and release naming. It exists
// so roarr's search path can be benchmarked against real Prowlarr/Sonarr
// without touching real trackers.
//
// Every indexer is served under /{name}/api. Request counts and timings are
// exposed on /stats so experiments can see how many upstream queries each
// search strategy really costs.
package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"hash/fnv"
	"log"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// profile describes how one fake indexer behaves.
type profile struct {
	Name       string  `json:"name"`
	MedianMS   float64 `json:"median_ms"`
	Sigma      float64 `json:"sigma"`       // lognormal spread
	FailRate   float64 `json:"fail_rate"`   // returns HTTP 503
	HangRate   float64 `json:"hang_rate"`   // never answers within HangFor
	HangFor    float64 `json:"hang_s"`      // seconds
	Coverage   float64 `json:"coverage"`    // share of the catalogue it carries
	IDSearch   bool    `json:"id_search"`   // advertises tvdbid/imdbid/tmdbid
	Anime      bool    `json:"anime"`       // carries fansub style names
	MaxResults int     `json:"max_results"` // per page
}

var defaultProfiles = []profile{
	{Name: "swiftbay", MedianMS: 350, Sigma: 0.35, Coverage: 0.7, IDSearch: true, MaxResults: 100},
	{Name: "torrentgala", MedianMS: 1200, Sigma: 0.5, Coverage: 0.8, IDSearch: false, MaxResults: 100},
	{Name: "eztvish", MedianMS: 900, Sigma: 0.4, Coverage: 0.6, IDSearch: true, MaxResults: 100},
	{Name: "slowtracker", MedianMS: 5500, Sigma: 0.45, Coverage: 0.9, IDSearch: true, MaxResults: 100},
	{Name: "flarecf", MedianMS: 2800, Sigma: 0.6, FailRate: 0.15, Coverage: 0.5, MaxResults: 50},
	{Name: "deadpool", MedianMS: 1500, Sigma: 0.4, HangRate: 0.35, HangFor: 45, Coverage: 0.6, IDSearch: true, MaxResults: 100},
	{Name: "nyaaish", MedianMS: 700, Sigma: 0.4, Coverage: 0.9, Anime: true, MaxResults: 75},
}

// ---------------------------------------------------------------------------
// Catalogue

type show struct {
	Title    string
	Year     int
	TVDB     int
	TMDB     int
	IMDB     string
	Seasons  []int // episode counts, index 0 = season 1
	Anime    bool
	Romaji   string
	Runtime  int
	Finished bool
}

type movie struct {
	Title string
	Year  int
	TMDB  int
	IMDB  string
}

var shows = []show{
	{Title: "Severance", Year: 2022, TVDB: 371980, TMDB: 95396, IMDB: "tt11280740", Seasons: []int{9, 10}, Runtime: 55},
	{Title: "The Expanse", Year: 2015, TVDB: 280619, TMDB: 63639, IMDB: "tt3230854", Seasons: []int{10, 13, 13, 13, 10, 6}, Runtime: 45, Finished: true},
	{Title: "Breaking Bad", Year: 2008, TVDB: 81189, TMDB: 1396, IMDB: "tt0903747", Seasons: []int{7, 13, 13, 13, 16}, Runtime: 47, Finished: true},
	{Title: "Frieren: Beyond Journey's End", Year: 2023, TVDB: 424536, TMDB: 209867, IMDB: "tt22248376", Seasons: []int{28}, Anime: true, Romaji: "Sousou no Frieren", Runtime: 24},
	{Title: "Pluribus", Year: 2025, TVDB: 449358, TMDB: 225171, IMDB: "tt22202452", Seasons: []int{9}, Runtime: 50},
}

var movies = []movie{
	{Title: "Dune", Year: 2021, TMDB: 438631, IMDB: "tt1160419"},
	{Title: "Dune: Part Two", Year: 2024, TMDB: 693134, IMDB: "tt15239678"},
	{Title: "The Matrix", Year: 1999, TMDB: 603, IMDB: "tt0133093"},
	{Title: "The Matrix Reloaded", Year: 2003, TMDB: 604, IMDB: "tt0234215"},
	{Title: "The Matrix Revolutions", Year: 2003, TMDB: 605, IMDB: "tt0242653"},
	{Title: "Big Buck Bunny", Year: 2008, TMDB: 10378, IMDB: "tt1254207"},
}

type release struct {
	Title    string
	Size     int64
	Seeders  int
	Peers    int
	Cat      int
	Pub      time.Time
	Hash     string
	Seasons  []int // seasons covered; empty for movies
	Episode  int   // 0 for packs
	Complete bool  // carries no season token in the title
	TVDB     int
	TMDB     int
	IMDB     string
	Movie    bool
	Torrent  string // real .torrent served for the lab swarm, if any
	sortKey  int64
}

type variant struct {
	tag   string
	group string
	gbHr  float64 // size per hour of runtime
	pop   float64 // seeding popularity multiplier
	cat   int
}

var epVariants = []variant{
	{"1080p.WEB.H264", "GLHF", 2.6, 1.0, 5040},
	{"1080p.AMZN.WEB-DL.DDP5.1.H.264", "NTb", 3.4, 0.8, 5040},
	{"1080p.WEB.h264", "ETHEL", 2.4, 0.9, 5040},
	{"2160p.WEB-DL.DV.HDR.DDP5.1.Atmos.H.265", "FLUX", 9.5, 0.5, 5045},
	{"720p.WEB.H264", "SYNCOPY", 1.2, 0.6, 5040},
	{"1080p.WEB.H265", "MeGusta", 0.9, 1.4, 5040},
	{"1080p.x265.10bit", "ELiTE", 0.7, 0.9, 5040},
	{"720p.HDTV.x264", "KILLERS", 0.8, 0.3, 5040},
	{"1080p.BluRay.x264", "ROVERS", 5.5, 0.4, 5040},
	{"480p.WEB.x264", "mSD", 0.3, 0.3, 5030},
}

var packVariants = []variant{
	{"1080p.WEB-DL.DDP5.1.H.264", "NTb", 3.4, 0.8, 5040},
	{"2160p.WEB-DL.DV.HDR.DDP5.1.H.265", "FLUX", 9.5, 0.5, 5045},
	{"1080p.WEB.H265", "MeGusta", 0.9, 1.2, 5040},
	{"1080p.BluRay.x265.10bit.AAC.5.1", "Silence", 1.4, 1.5, 5040},
	{"720p.WEB.x264", "GalaxyTV", 0.7, 1.1, 5040},
	{"1080p.BluRay.REMUX.AVC.DTS-HD.MA.5.1", "EPSiLON", 16, 0.3, 5045},
}

var movieVariants = []variant{
	{"1080p.BluRay.x264.DTS", "SPARKS", 10, 0.8, 2040},
	{"2160p.UHD.BluRay.REMUX.DV.HDR.HEVC.TrueHD.Atmos.7.1", "FraMeSToR", 60, 0.4, 2045},
	{"1080p.WEB-DL.DDP5.1.H.264", "FLUX", 6, 1.0, 2040},
	{"1080p.BluRay.x265.10bit", "YTS", 2, 2.0, 2040},
	{"720p.BluRay.x264", "YIFY", 1, 1.5, 2040},
	{"2160p.WEB-DL.DDP5.1.Atmos.DV.HDR.H.265", "NTb", 18, 0.6, 2045},
	{"1080p.BluRay.REMUX.AVC.DTS-HD.MA.5.1", "EPSiLON", 30, 0.3, 2045},
}

func dotted(s string) string {
	s = strings.NewReplacer(":", "", "'", "", ",", "", "&", "and").Replace(s)
	return strings.Join(strings.Fields(s), ".")
}

func h(parts ...string) uint64 {
	f := fnv.New64a()
	for _, p := range parts {
		f.Write([]byte(p))
		f.Write([]byte{0})
	}
	return f.Sum64()
}

// jitter is deterministic per title so the same release has the same size
// on every indexer, like the same file would.
func jitter(title string, lo, span float64) float64 {
	return lo + float64(h(title)%1000)/1000*span
}

// infohash is per title: the same torrent is cross-posted to several trackers.
func infohash(title, indexer string) string {
	_ = indexer
	sum := sha1.Sum([]byte(title))
	return hex.EncodeToString(sum[:])
}

// buildCatalogue renders every release one indexer carries.
func buildCatalogue(p profile, labTorrents map[string]labTorrent) []release {
	rng := rand.New(rand.NewSource(int64(h(p.Name))))
	keep := func() bool { return rng.Float64() < p.Coverage }
	seed := func(pop float64, age float64) int {
		base := rng.ExpFloat64() * 60 * pop / math.Max(1, age/2)
		return int(base) + rng.Intn(4)
	}
	var out []release
	now := time.Now().UTC()

	for _, s := range shows {
		start := time.Date(s.Year, 3, 1, 0, 0, 0, 0, time.UTC)
		epIndex := 0
		for si, count := range s.Seasons {
			sn := si + 1
			seasonStart := start.AddDate(0, 0, si*380)
			for ep := 1; ep <= count; ep++ {
				epIndex++
				aired := seasonStart.AddDate(0, 0, (ep-1)*7)
				if aired.After(now) {
					continue
				}
				ageYears := now.Sub(aired).Hours() / 24 / 365
				if s.Anime && p.Anime {
					for _, g := range []struct{ grp, res string }{{"SubsPlease", "1080p"}, {"Erai-raws", "1080p"}, {"SubsPlease", "720p"}} {
						if !keep() {
							continue
						}
						t := fmt.Sprintf("[%s] %s - %02d (%s) [%08X].mkv", g.grp, s.Romaji, epIndex, g.res, uint32(h(g.grp, s.Romaji, strconv.Itoa(epIndex))))
						size := int64(1.4e9 * float64(s.Runtime) / 60)
						if g.res == "720p" {
							size /= 2
						}
						out = append(out, release{Title: t, Size: size, Seeders: seed(1.5, ageYears), Cat: 5070, Pub: aired.Add(2 * time.Hour), Seasons: []int{sn}, Episode: ep, TVDB: s.TVDB, TMDB: s.TMDB, IMDB: s.IMDB})
					}
					continue
				}
				if s.Anime {
					continue
				}
				for vi, v := range epVariants {
					if v.group == "KILLERS" && s.Year > 2012 {
						continue
					}
					if v.group == "ROVERS" && ageYears < 1 {
						continue
					}
					if !keep() || rng.Float64() < 0.25 {
						continue
					}
					t := fmt.Sprintf("%s.S%02dE%02d.%s-%s", dotted(s.Title), sn, ep, v.tag, v.group)
					if s.Year >= 2025 || strings.Contains(s.Title, ":") {
						t = fmt.Sprintf("%s.%d.S%02dE%02d.%s-%s", dotted(s.Title), s.Year, sn, ep, v.tag, v.group)
					}
					size := int64(v.gbHr * 1e9 * float64(s.Runtime) / 60 * (0.85 + rng.Float64()*0.3))
					out = append(out, release{Title: t, Size: size, Seeders: seed(v.pop, ageYears), Cat: v.cat, Pub: aired.Add(time.Duration(3+vi) * time.Hour), Seasons: []int{sn}, Episode: ep, TVDB: s.TVDB, TMDB: s.TMDB, IMDB: s.IMDB})
				}
			}
			// season packs once the season finished airing
			last := seasonStart.AddDate(0, 0, (count-1)*7)
			if last.After(now) {
				continue
			}
			ageYears := now.Sub(last).Hours() / 24 / 365
			for vi, v := range packVariants {
				if !keep() || (s.Anime && vi > 0) {
					continue
				}
				var t string
				if v.group == "Silence" {
					t = fmt.Sprintf("%s (%d) Season %d S%02d (1080p BluRay x265 HEVC 10bit AAC 5.1 Silence) [QxR]", s.Title, s.Year, sn, sn)
				} else {
					t = fmt.Sprintf("%s.S%02d.%s-%s", dotted(s.Title), sn, v.tag, v.group)
				}
				if s.Anime {
					t = fmt.Sprintf("[Judas] %s (Season %d) [1080p][HEVC x265 10bit][Multi-Subs] (Batch)", s.Romaji, sn)
				}
				size := int64(v.gbHr * 1e9 * float64(s.Runtime*count) / 60 * (0.9 + rng.Float64()*0.2))
				out = append(out, release{Title: t, Size: size, Seeders: seed(v.pop*1.5, ageYears), Cat: v.cat, Pub: last.AddDate(0, 0, 1+vi), Seasons: []int{sn}, TVDB: s.TVDB, TMDB: s.TMDB, IMDB: s.IMDB})
			}
		}
		if s.Finished && len(s.Seasons) > 1 {
			total := 0
			all := make([]int, len(s.Seasons))
			for i, c := range s.Seasons {
				total += c
				all[i] = i + 1
			}
			lastYear := s.Year + len(s.Seasons)
			for vi, v := range packVariants[:4] {
				if !keep() {
					continue
				}
				var t string
				complete := false
				switch vi {
				case 0:
					t = fmt.Sprintf("%s.S01-S%02d.COMPLETE.%s-%s", dotted(s.Title), len(s.Seasons), v.tag, v.group)
				case 3:
					t = fmt.Sprintf("%s (%d) Season 1-%d S01-S%02d (1080p BluRay x265 HEVC 10bit AAC 5.1 Silence) [QxR]", s.Title, s.Year, len(s.Seasons), len(s.Seasons))
				default:
					t = fmt.Sprintf("%s.Complete.Series.%s-%s", dotted(s.Title), v.tag, v.group)
					complete = true
				}
				size := int64(v.gbHr * 1e9 * float64(s.Runtime*total) / 60)
				out = append(out, release{Title: t, Size: size, Seeders: seed(v.pop*2, 3), Cat: v.cat, Pub: time.Date(lastYear, 6, 1+vi, 0, 0, 0, 0, time.UTC), Seasons: all, Complete: complete, TVDB: s.TVDB, TMDB: s.TMDB, IMDB: s.IMDB})
			}
		}
	}

	for _, m := range movies {
		released := time.Date(m.Year, 9, 1, 0, 0, 0, 0, time.UTC)
		ageYears := now.Sub(released).Hours() / 24 / 365
		for vi, v := range movieVariants {
			if !keep() {
				continue
			}
			t := fmt.Sprintf("%s.%d.%s-%s", dotted(m.Title), m.Year, v.tag, v.group)
			if v.group == "YTS" {
				t = fmt.Sprintf("%s (%d) [1080p] [BluRay] [x265] [10bit] [5.1] [YTS.MX]", m.Title, m.Year)
			}
			size := int64(v.gbHr * 1e9 * (0.85 + rng.Float64()*0.3))
			out = append(out, release{Title: t, Size: size, Seeders: seed(v.pop*2, ageYears), Cat: v.cat, Pub: released.AddDate(0, 3, vi), TMDB: m.TMDB, IMDB: m.IMDB, Movie: true})
		}
	}

	// Real torrents seeded inside the lab. They are carried by every indexer
	// so the grab -> download -> import path can be exercised end to end.
	for title, lt := range labTorrents {
		r := release{Title: title, Size: lt.Size, Seeders: 3, Peers: 3, Cat: 5040, Pub: now.Add(-time.Hour), Torrent: lt.File, Hash: lt.Hash}
		for _, s := range shows {
			if strings.HasPrefix(title, dotted(s.Title)+".") {
				r.TVDB, r.TMDB, r.IMDB = s.TVDB, s.TMDB, s.IMDB
				if m := regexp.MustCompile(`\.S(\d\d)(?:E(\d\d))?\.`).FindStringSubmatch(title); m != nil {
					sn, _ := strconv.Atoi(m[1])
					r.Seasons = []int{sn}
					if m[2] != "" {
						r.Episode, _ = strconv.Atoi(m[2])
					}
				}
			}
		}
		out = append(out, r)
	}

	for i := range out {
		if out[i].Hash == "" {
			out[i].Hash = infohash(out[i].Title, p.Name)
		}
		out[i].Peers = out[i].Seeders + rng.Intn(10)
		out[i].sortKey = out[i].Pub.UnixNano()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].sortKey > out[j].sortKey })
	return out
}

type labTorrent struct {
	File string
	Size int64
	Hash string
}

// ---------------------------------------------------------------------------
// Search

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func norm(s string) string {
	return strings.TrimSpace(nonAlnum.ReplaceAllString(strings.ToLower(s), " "))
}

var sxxRe = regexp.MustCompile(`(?i)\bs(\d{1,2})(?:e(\d{1,3}))?\b`)

type query struct {
	T       string
	Q       string
	Season  int
	Episode int
	TVDB    int
	TMDB    int
	IMDB    string
	Cats    []int
	Limit   int
	Offset  int
}

func (r release) matches(q query) bool {
	if len(q.Cats) > 0 {
		ok := false
		for _, c := range q.Cats {
			if c == r.Cat || c == r.Cat/1000*1000 {
				ok = true
			}
		}
		if !ok {
			return false
		}
	}
	switch q.T {
	case "movie":
		if !r.Movie {
			return false
		}
	case "tvsearch":
		if r.Movie {
			return false
		}
	}
	idMatched := false
	if q.TVDB != 0 {
		if r.TVDB != q.TVDB {
			return false
		}
		idMatched = true
	}
	if q.TMDB != 0 {
		if r.TMDB != q.TMDB {
			return false
		}
		idMatched = true
	}
	if q.IMDB != "" {
		if strings.TrimPrefix(r.IMDB, "tt") != strings.TrimPrefix(q.IMDB, "tt") {
			return false
		}
		idMatched = true
	}
	text := q.Q
	if !idMatched && text != "" {
		// Trackers do a dumb token AND match on the title. Strip an inline
		// SxxEyy token the way a real tracker would match it textually.
		season, episode := q.Season, q.Episode
		if m := sxxRe.FindStringSubmatch(text); m != nil {
			season, _ = strconv.Atoi(m[1])
			if m[2] != "" {
				episode, _ = strconv.Atoi(m[2])
			}
			text = sxxRe.ReplaceAllString(text, " ")
		}
		q.Season, q.Episode = season, episode
		title := " " + norm(r.Title) + " "
		for _, tok := range strings.Fields(norm(text)) {
			if !strings.Contains(title, " "+tok+" ") {
				return false
			}
		}
	} else if !idMatched && q.T != "" && q.Season == 0 {
		// empty query = RSS feed
		return true
	}
	if q.Season != 0 {
		if r.Complete {
			return false
		}
		covers := false
		for _, s := range r.Seasons {
			if s == q.Season {
				covers = true
			}
		}
		if !covers {
			return false
		}
		if q.Episode != 0 && r.Episode != q.Episode {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Torznab XML

type rss struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	Atom    string   `xml:"xmlns:atom,attr"`
	Torznab string   `xml:"xmlns:torznab,attr"`
	Channel channel  `xml:"channel"`
}

type channel struct {
	Title string `xml:"title"`
	Items []item `xml:"item"`
}

type item struct {
	Title     string    `xml:"title"`
	GUID      string    `xml:"guid"`
	Link      string    `xml:"link"`
	Comments  string    `xml:"comments"`
	PubDate   string    `xml:"pubDate"`
	Size      int64     `xml:"size"`
	Category  int       `xml:"category"`
	Enclosure enclosure `xml:"enclosure"`
	Attrs     []attr    `xml:"torznab:attr"`
}

type enclosure struct {
	URL    string `xml:"url,attr"`
	Length int64  `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

type attr struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

func capsXML(p profile) string {
	tv, mv := "q,season,ep", "q"
	if p.IDSearch {
		tv, mv = "q,season,ep,tvdbid,imdbid,tmdbid", "q,imdbid,tmdbid"
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<caps>
  <server version="1.0" title="%s"/>
  <limits default="%d" max="%d"/>
  <searching>
    <search available="yes" supportedParams="q"/>
    <tv-search available="yes" supportedParams="%s"/>
    <movie-search available="yes" supportedParams="%s"/>
  </searching>
  <categories>
    <category id="2000" name="Movies">
      <subcat id="2030" name="Movies/SD"/><subcat id="2040" name="Movies/HD"/><subcat id="2045" name="Movies/UHD"/>
    </category>
    <category id="5000" name="TV">
      <subcat id="5030" name="TV/SD"/><subcat id="5040" name="TV/HD"/><subcat id="5045" name="TV/UHD"/><subcat id="5070" name="TV/Anime"/>
    </category>
  </categories>
</caps>`, p.Name, p.MaxResults, p.MaxResults, tv, mv)
}

// ---------------------------------------------------------------------------
// Server

type stat struct {
	Indexer string  `json:"indexer"`
	T       string  `json:"t"`
	Params  string  `json:"params"`
	Results int     `json:"results"`
	MS      float64 `json:"ms"`
	Status  int     `json:"status"`
	At      string  `json:"at"`
}

type server struct {
	profiles  map[string]profile
	catalogue map[string][]release
	torrents  map[string]labTorrent
	public    string
	mu        sync.Mutex
	log       []stat
	rng       *rand.Rand
	speed     float64 // latency multiplier, 0 disables delays
}

func (s *server) delay(p profile) (time.Duration, int) {
	s.mu.Lock()
	r := s.rng.Float64()
	n := s.rng.NormFloat64()
	s.mu.Unlock()
	if r < p.HangRate {
		return time.Duration(p.HangFor * float64(time.Second) * s.speed), http.StatusGatewayTimeout
	}
	d := time.Duration(p.MedianMS*math.Exp(n*p.Sigma)*s.speed) * time.Millisecond
	if r < p.HangRate+p.FailRate {
		return d, http.StatusServiceUnavailable
	}
	return d, http.StatusOK
}

func atoi(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }

func (s *server) handleAPI(w http.ResponseWriter, r *http.Request, p profile) {
	v := r.URL.Query()
	t := v.Get("t")
	if t == "caps" {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, capsXML(p))
		return
	}
	start := time.Now()
	q := query{T: t, Q: v.Get("q"), Season: atoi(v.Get("season")), Episode: atoi(v.Get("ep")), TVDB: atoi(v.Get("tvdbid")), TMDB: atoi(v.Get("tmdbid")), IMDB: v.Get("imdbid"), Limit: atoi(v.Get("limit")), Offset: atoi(v.Get("offset"))}
	for _, c := range strings.Split(v.Get("cat"), ",") {
		if c != "" {
			q.Cats = append(q.Cats, atoi(c))
		}
	}
	if !p.IDSearch {
		q.TVDB, q.TMDB, q.IMDB = 0, 0, ""
	}
	if q.Limit <= 0 || q.Limit > p.MaxResults {
		q.Limit = p.MaxResults
	}
	d, status := s.delay(p)
	select {
	case <-time.After(d):
	case <-r.Context().Done():
		s.record(stat{Indexer: p.Name, T: t, Params: r.URL.RawQuery, MS: ms(start), Status: 499})
		return
	}
	if status != http.StatusOK {
		s.record(stat{Indexer: p.Name, T: t, Params: r.URL.RawQuery, MS: ms(start), Status: status})
		http.Error(w, "upstream unavailable", status)
		return
	}
	var hits []release
	for _, rel := range s.catalogue[p.Name] {
		if rel.matches(q) {
			hits = append(hits, rel)
		}
	}
	if q.Offset < len(hits) {
		hits = hits[q.Offset:]
	} else {
		hits = nil
	}
	if len(hits) > q.Limit {
		hits = hits[:q.Limit]
	}
	doc := rss{Version: "2.0", Atom: "http://www.w3.org/2005/Atom", Torznab: "http://torznab.com/schemas/2015/feed", Channel: channel{Title: p.Name}}
	base := s.public
	if base == "" {
		base = "http://" + r.Host
	}
	for _, rel := range hits {
		link := fmt.Sprintf("%s/%s/dl/%s.torrent", base, p.Name, rel.Hash)
		it := item{
			Title: rel.Title, GUID: fmt.Sprintf("%s/%s/details/%s", base, p.Name, rel.Hash), Link: link,
			Comments: fmt.Sprintf("%s/%s/details/%s", base, p.Name, rel.Hash), PubDate: rel.Pub.Format(time.RFC1123Z),
			Size: rel.Size, Category: rel.Cat, Enclosure: enclosure{URL: link, Length: rel.Size, Type: "application/x-bittorrent"},
			Attrs: []attr{{"category", strconv.Itoa(rel.Cat)}, {"seeders", strconv.Itoa(rel.Seeders)}, {"peers", strconv.Itoa(rel.Peers)}, {"infohash", rel.Hash}, {"downloadvolumefactor", "1"}, {"uploadvolumefactor", "1"}},
		}
		if rel.TVDB != 0 {
			it.Attrs = append(it.Attrs, attr{"tvdbid", strconv.Itoa(rel.TVDB)})
		}
		if rel.IMDB != "" {
			it.Attrs = append(it.Attrs, attr{"imdbid", strings.TrimPrefix(rel.IMDB, "tt")})
		}
		doc.Channel.Items = append(doc.Channel.Items, it)
	}
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	fmt.Fprint(w, xml.Header)
	enc := xml.NewEncoder(w)
	_ = enc.Encode(doc)
	s.record(stat{Indexer: p.Name, T: t, Params: r.URL.RawQuery, Results: len(hits), MS: ms(start), Status: 200})
}

func ms(start time.Time) float64 { return float64(time.Since(start).Microseconds()) / 1000 }

func (s *server) record(st stat) {
	st.At = time.Now().UTC().Format(time.RFC3339Nano)
	s.mu.Lock()
	s.log = append(s.log, st)
	s.mu.Unlock()
	log.Printf("%-12s t=%-8s status=%d results=%-3d %7.0fms %s", st.Indexer, st.T, st.Status, st.Results, st.MS, st.Params)
}

func (s *server) handleDownload(w http.ResponseWriter, r *http.Request, p profile, hash string) {
	for _, rel := range s.catalogue[p.Name] {
		if rel.Hash != hash {
			continue
		}
		if rel.Torrent != "" {
			w.Header().Set("Content-Type", "application/x-bittorrent")
			http.ServeFile(w, r, rel.Torrent)
			return
		}
		// Synthetic releases have no payload; hand back a magnet that will
		// never resolve so download clients accept the grab but stall.
		http.Redirect(w, r, fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s&tr=%s", rel.Hash, url.QueryEscape(rel.Title), url.QueryEscape("http://tracker.invalid:6969/announce")), http.StatusFound)
		return
	}
	http.NotFound(w, r)
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case len(parts) == 1 && parts[0] == "stats":
		s.mu.Lock()
		defer s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.log)
		return
	case len(parts) == 2 && parts[0] == "stats" && parts[1] == "reset":
		s.mu.Lock()
		s.log = nil
		s.mu.Unlock()
		fmt.Fprintln(w, "ok")
		return
	case len(parts) == 2 && parts[0] == "speed":
		f, err := strconv.ParseFloat(parts[1], 64)
		if err == nil {
			s.mu.Lock()
			s.speed = f
			s.mu.Unlock()
		}
		fmt.Fprintln(w, s.speed)
		return
	}
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	p, ok := s.profiles[parts[0]]
	if !ok {
		http.NotFound(w, r)
		return
	}
	switch {
	case parts[1] == "api":
		s.handleAPI(w, r, p)
	case parts[1] == "dl" && len(parts) == 3:
		s.handleDownload(w, r, p, strings.TrimSuffix(parts[2], ".torrent"))
	case parts[1] == "details":
		fmt.Fprintf(w, "<html><body>%s</body></html>", strings.Join(parts, "/"))
	default:
		http.NotFound(w, r)
	}
}

func loadLabTorrents(dir string) map[string]labTorrent {
	out := map[string]labTorrent{}
	if dir == "" {
		return out
	}
	// index.json is written by lab/swarm next to the .torrent files:
	// {"<release title>": {"file": "x.torrent", "size": 123, "hash": "abc"}}
	b, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		return out
	}
	var raw map[string]struct {
		File string `json:"file"`
		Size int64  `json:"size"`
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		log.Printf("bad lab index: %v", err)
		return out
	}
	for title, v := range raw {
		out[title] = labTorrent{File: filepath.Join(dir, v.File), Size: v.Size, Hash: v.Hash}
	}
	return out
}

func main() {
	addr := flag.String("addr", ":9117", "listen address")
	public := flag.String("public", os.Getenv("MOCK_PUBLIC_URL"), "public base URL used in download links")
	torrents := flag.String("torrents", os.Getenv("MOCK_TORRENTS"), "directory with lab torrents + index.json")
	speed := flag.Float64("speed", 1, "latency multiplier (0 = instant)")
	flag.Parse()

	s := &server{profiles: map[string]profile{}, catalogue: map[string][]release{}, public: strings.TrimRight(*public, "/"), rng: rand.New(rand.NewSource(time.Now().UnixNano())), speed: *speed}
	s.torrents = loadLabTorrents(*torrents)
	total := 0
	for _, p := range defaultProfiles {
		s.profiles[p.Name] = p
		s.catalogue[p.Name] = buildCatalogue(p, s.torrents)
		total += len(s.catalogue[p.Name])
	}
	log.Printf("mockindexer: %d indexers, %d releases, %d lab torrents, listening on %s", len(s.profiles), total, len(s.torrents), *addr)
	log.Fatal(http.ListenAndServe(*addr, s))
}
