package app

import "testing"

func TestAutoMoviesCompleteNeedsEveryMovie(t *testing.T) {
	hit := func(tmdb int, key string) Release { return Release{Key: key, MovieID: tmdb, Score: 50} }
	// The first indexer to answer has only one movie of a three-movie collection.
	rels := []Release{hit(603, "matrix")}
	if complete(pickMovies(3, rels)) {
		t.Fatal("one of three movies has a pick: the search must go on")
	}
	rels = append(rels, hit(604, "reloaded"), Release{Key: "fake", MovieID: 605, Score: 90, Rejected: true})
	if complete(pickMovies(3, rels)) {
		t.Fatal("a rejected release is not a pick")
	}
	rels = append(rels, hit(605, "revolutions"))
	p := pickMovies(3, rels)
	if !complete(p) || p.best[605] != "revolutions" {
		t.Fatalf("every movie has a pick: %+v", p)
	}
	if !complete(pickMovies(1, []Release{hit(603, "matrix")})) {
		t.Fatal("a single movie is complete on its first acceptable release")
	}
}
