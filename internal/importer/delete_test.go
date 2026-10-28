package importer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func TestInsideRoot(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "tv")
	outside := filepath.Join(dir, "outside")
	writeFile(t, filepath.Join(root, "Show", "Season 01", "a.mkv"), "a")
	writeFile(t, filepath.Join(outside, "secret.mkv"), "s")
	// A folder in the library that links to a folder outside it.
	if err := os.Symlink(outside, filepath.Join(root, "Linked")); err != nil {
		t.Fatal(err)
	}
	ok := []string{
		filepath.Join(root, "Show", "Season 01", "a.mkv"),
		filepath.Join(root, "Show", "Season 01", "gone.mkv"), // need not exist
	}
	for _, p := range ok {
		if err := InsideRoot(root, p); err != nil {
			t.Errorf("InsideRoot(%q) = %v, want nil", p, err)
		}
	}
	bad := []string{
		root, // the library folder itself
		filepath.Join(root, "..", "outside", "secret.mkv"),
		filepath.Join(root, "Show", "..", "..", "outside", "secret.mkv"),
		filepath.Join(outside, "secret.mkv"),
		filepath.Join(root, "Linked", "secret.mkv"), // escapes through a link
		"relative/Show/a.mkv",
		dir + "/tv-other/a.mkv", // shares the prefix, not the folder
	}
	for _, p := range bad {
		if err := InsideRoot(root, p); err == nil {
			t.Errorf("InsideRoot(%q) = nil, want an error", p)
		}
	}
	if err := InsideRoot("", filepath.Join(root, "Show", "Season 01", "a.mkv")); err == nil {
		t.Error("an empty library folder must refuse every path")
	}
}

func TestDeleteOnlyRecordedFilesInsideTheLibrary(t *testing.T) {
	dir := t.TempDir()
	tvRoot, movieRoot := filepath.Join(dir, "tv"), filepath.Join(dir, "movies")
	outside := filepath.Join(dir, "downloads")
	st, _ := OpenStore(filepath.Join(dir, "library.json"))
	im := New(Config{TVRoot: tvRoot, MovieRoot: movieRoot}, st, nil)
	show := Title{Kind: "tv", Title: "Show", Year: 2020, TmdbID: 1}

	season := filepath.Join(tvRoot, "Show (2020)", "Season 01")
	e1 := filepath.Join(season, "Show - S01E01.mkv")
	e1sub := filepath.Join(season, "Show - S01E01.en.srt")
	e1hand := filepath.Join(season, "Show - S01E01.de.srt") // added by hand
	e2 := filepath.Join(season, "Show - S01E02.mkv")        // placed by hand
	secret := filepath.Join(outside, "Show.S01E03.mkv")
	escape := filepath.Join(tvRoot, "..", "downloads", "Show.S01E03.mkv")
	linked := filepath.Join(tvRoot, "Linked", "Show.S01E03.mkv")
	wrongRoot := filepath.Join(movieRoot, "Other (2000)", "Other (2000).mkv")
	for _, p := range []string{e1, e1sub, e1hand, e2, secret, wrongRoot} {
		writeFile(t, p, "x")
	}
	if err := os.Symlink(outside, filepath.Join(tvRoot, "Linked")); err != nil {
		t.Fatal(err)
	}
	put := func(p string, subs ...string) {
		if err := st.Put(FileRecord{TitleKey: show.Key(), Season: 1, Episodes: []int{1}, Path: p, Subtitles: subs, Size: 1, Hash: "h1"}); err != nil {
			t.Fatal(err)
		}
	}
	put(e1, e1sub, secret) // a recorded subtitle outside the library stays too
	put(escape)
	put(linked)
	put(wrongRoot) // a TV record that points into the movie folder

	res := im.Delete([]string{e1, e2, escape, linked, wrongRoot, secret})
	if len(res.Deleted) != 1 || res.Deleted[0].Path != e1 {
		t.Fatalf("deleted %+v, refused %v", res.Deleted, res.Refused)
	}
	if exists(e1) || exists(e1sub) {
		t.Fatal("recorded video or its recorded subtitle is still there")
	}
	for _, p := range []string{e1hand, e2, secret, wrongRoot} {
		if !exists(p) {
			t.Errorf("%s was deleted", p)
		}
	}
	for _, p := range []string{e2, escape, linked, wrongRoot, secret} {
		if res.Refused[p] == "" {
			t.Errorf("%s not reported as refused: %v", p, res.Refused)
		}
	}
	if _, ok := st.Record(e1); ok {
		t.Error("the record of the deleted file stays")
	}
	if _, ok := st.Record(escape); !ok {
		t.Error("a refused record was forgotten")
	}
	// The season folder still holds files: it stays.
	if !exists(season) {
		t.Fatal("a folder that is not empty was removed")
	}
}

func TestDeletePrunesEmptyFoldersUpToTheRoot(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "movies")
	st, _ := OpenStore("")
	im := New(Config{MovieRoot: root}, st, nil)
	m := Title{Kind: "movie", Title: "Film", Year: 2001, TmdbID: 9}
	p := filepath.Join(root, "Film (2001)", "Film (2001).mkv")
	writeFile(t, p, "film")
	_ = st.Put(FileRecord{TitleKey: m.Key(), Path: p, Size: 4})
	res := im.Delete([]string{p})
	if len(res.Deleted) != 1 || res.Freed != 4 {
		t.Fatalf("%+v", res)
	}
	if exists(filepath.Dir(p)) {
		t.Error("the empty movie folder stays")
	}
	if !exists(root) {
		t.Fatal("the library folder itself was removed")
	}
	// A record whose file is already gone is forgotten without an error.
	_ = st.Put(FileRecord{TitleKey: m.Key(), Path: p})
	if res := im.Delete([]string{p}); len(res.Deleted) != 1 || len(res.Refused) != 0 {
		t.Fatalf("missing file: %+v", res)
	}
}

func TestDeleteRefusesFolders(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "tv")
	st, _ := OpenStore("")
	im := New(Config{TVRoot: root}, st, nil)
	show := filepath.Join(root, "Show (2020)")
	writeFile(t, filepath.Join(show, "Season 01", "Show - S01E01.mkv"), "x")
	_ = st.Put(FileRecord{TitleKey: "tv:tmdb:1", Path: show})
	res := im.Delete([]string{show})
	if len(res.Deleted) != 0 || res.Refused[show] == "" || !exists(filepath.Join(show, "Season 01", "Show - S01E01.mkv")) {
		t.Fatalf("a folder record must not be deleted: %+v", res)
	}
}

func TestInsideRootReportsMissingFolders(t *testing.T) {
	root := t.TempDir()
	err := InsideRoot(root, filepath.Join(root, "No Show", "Season 01", "x.mkv"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("got %v, want a not-exist error", err)
	}
}

func TestStorePutTitleKeepsManagementFields(t *testing.T) {
	st, _ := OpenStore("")
	tl := Title{Kind: "tv", Title: "Show", TmdbID: 1, Poster: "p1"}
	_ = st.PutTitle(tl)
	_, _ = st.UpdateTitle(tl.Key(), func(x *Title) { x.Unmonitored, x.Monitor, x.Added = true, []int{10001}, "2020-01-01" })
	_ = st.PutTitle(Title{Kind: "tv", Title: "Show 2", TmdbID: 1})
	got, _ := st.Title(tl.Key())
	if got.Title != "Show 2" || !got.Unmonitored || len(got.Monitor) != 1 || got.Added != "2020-01-01" || got.Poster != "p1" {
		t.Fatalf("%+v", got)
	}
}
