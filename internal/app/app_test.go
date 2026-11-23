package app

import (
	"reflect"
	"testing"
	"time"

	"github.com/noxaur/roarr/internal/arr"
	"github.com/noxaur/roarr/internal/release"
	"github.com/noxaur/roarr/internal/search"
)

func testShow() *SeriesDetail {
	d := &SeriesDetail{LibraryItem: LibraryItem{Kind: "tv", ID: 2, Title: "The Expanse", Year: 2015, TvdbID: 280619}, InLibrary: true, Runtime: 45}
	id := 1
	for s := 1; s <= 3; s++ {
		sd := SeasonDetail{Number: s}
		for e := 1; e <= 10; e++ {
			sd.Episodes = append(sd.Episodes, EpisodeDetail{ID: id, Season: s, Number: e, Aired: true, HasFile: s == 1})
			id++
		}
		d.Seasons = append(d.Seasons, sd)
	}
	return d
}

func TestBuildRequestPrioritisesScope(t *testing.T) {
	d := testShow()
	// One episode of season 3 while the show page prefetches.
	req := buildRequest(Target{Kind: "tv", Series: d}, Scope{Show: true, Episodes: []int{25}}, nil)
	if !reflect.DeepEqual(req.Seasons, []int{3}) {
		t.Fatalf("focus seasons = %v, want [3]", req.Seasons)
	}
	if len(req.Episodes) != 1 || req.Episodes[0] != (search.EpQuery{Season: 3, Episode: 5}) {
		t.Fatalf("episodes = %+v", req.Episodes)
	}
	if req.BroadPriority != search.PShow {
		t.Fatalf("broad priority = %v, want PShow", req.BroadPriority)
	}
	// Season 2 (missing) is prefetched before season 1 (complete).
	if !reflect.DeepEqual(req.PrefetchSeasons, []int{2, 1}) {
		t.Fatalf("prefetch = %v, want [2 1]", req.PrefetchSeasons)
	}
}

func TestAnnotateCoverageAndWrongShow(t *testing.T) {
	d := testShow()
	an := newAnnotator(Target{Kind: "tv", Series: d}, release.DefaultPrefs())
	seeders := 50
	pack := an.annotate(arr.SearchResult{Title: "The.Expanse.S02.1080p.WEB-DL.DDP5.1.H.264-NTb", Size: 30e9, Seeders: &seeders, PublishDate: time.Now()}, nil)
	if pack.Kind != "season" || len(pack.Covers) != 10 || pack.Covers[0] != 11 || pack.Rejected {
		t.Fatalf("pack = %+v", pack)
	}
	other := an.annotate(arr.SearchResult{Title: "The.Expanse.Aftershow.S02E01.1080p.WEB.h264-X", Size: 1e9, Seeders: &seeders}, nil)
	if !other.Rejected || len(other.Covers) != 0 {
		t.Fatalf("aftershow should be rejected: %+v", other)
	}
}

// Prowlarr fills magnetUrl with its own http proxy link for some indexers
// (Anime Tosho). Sonarr refuses a pushed magnetUrl that is not a magnet, so
// only real magnets stay magnets.
func TestAnnotateKeepsOnlyRealMagnets(t *testing.T) {
	an := newAnnotator(Target{Kind: "tv", Series: testShow()}, release.DefaultPrefs())
	proxy := "http://prowlarr:9696/20/download?apikey=k&link=x"
	magnet := "magnet:?xt=urn:btih:a11a9843ea05296de1978e9804f07c9e4e648c97"
	for name, tc := range map[string]struct{ dl, magnet, wantDL, wantMagnet string }{
		"proxy link only":     {magnet: proxy, wantDL: proxy},
		"proxy beside a link": {dl: "http://dl", magnet: proxy, wantDL: "http://dl"},
		"real magnet":         {dl: "http://dl", magnet: magnet, wantDL: "http://dl", wantMagnet: magnet},
	} {
		got := an.annotate(arr.SearchResult{Title: "The.Expanse.S02E01.1080p.WEB.h264-X", DownloadURL: tc.dl, MagnetURL: tc.magnet}, nil)
		if got.DownloadURL != tc.wantDL || got.MagnetURL != tc.wantMagnet {
			t.Errorf("%s: downloadUrl=%q magnetUrl=%q, want %q %q", name, got.DownloadURL, got.MagnetURL, tc.wantDL, tc.wantMagnet)
		}
	}
}

func TestReleaseKeyMergesCrossPosts(t *testing.T) {
	a := Release{Title: "Show.S01E01.1080p.WEB.H264-GRP", Size: 1_500_000_000, InfoHash: "aa"}
	b := Release{Title: "Show S01E01 1080p WEB H264-GRP", Size: 1_500_000_100, InfoHash: "bb"}
	if releaseKey(a) != releaseKey(b) {
		t.Fatalf("%s != %s", releaseKey(a), releaseKey(b))
	}
}
