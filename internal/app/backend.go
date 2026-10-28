package app

import "context"

// Backend is who downloads and imports a grab. "arr" pushes releases to
// Sonarr/Radarr, which hand them to their download client and import the
// result; "native" sends torrents to qBittorrent itself and imports the
// finished files into the library folders (IMPORT_MODE).
type Backend interface {
	Name() string
	GrabEpisodes(ctx context.Context, g GrabRequest) (GrabResult, error)
	GrabMovie(ctx context.Context, g GrabRequest) (GrabResult, error)
}

// Backend returns the configured backend.
func (a *App) Backend() Backend {
	if a.Settings.Get().Import.Native() {
		return a.Native
	}
	return arrBackend{a}
}

type arrBackend struct{ a *App }

func (b arrBackend) Name() string { return "arr" }

func (b arrBackend) GrabEpisodes(ctx context.Context, g GrabRequest) (GrabResult, error) {
	return b.a.grabEpisodes(ctx, g)
}

func (b arrBackend) GrabMovie(ctx context.Context, g GrabRequest) (GrabResult, error) {
	return b.a.grabMovie(ctx, g)
}
