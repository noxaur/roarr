import { lazy, Suspense, useEffect, useMemo, useState } from "react";
import { api, setUnauthorizedHandler } from "./lib/api";
import { Router, match, useRouter } from "./lib/router";
import { StoreProvider } from "./lib/store";
import { Toaster } from "./components/ui/sonner";
import { TooltipProvider } from "./components/ui/tooltip";
import { ConfirmProvider } from "./components/Confirm";
import { DownloadsDrawer } from "./components/Downloads";
import { Nav, isAdminPath, isDownloadsPath } from "./components/Nav";
import { SearchPalette, type Command } from "./components/SearchPalette";
import { ShortcutsDialog } from "./components/Shortcuts";
import { ShellCtx, keyIsBusy } from "./lib/shell";
import { toggleTheme } from "./lib/theme";
import { undoLast } from "./lib/undo";
import { Home } from "./pages/Home";
import { Login } from "./pages/Login";

// Home ships with the first load; every other page is fetched on demand.
const SeriesPage = lazy(() => import("./pages/Series").then((m) => ({ default: m.SeriesPage })));
const MoviePage = lazy(() => import("./pages/Movie").then((m) => ({ default: m.MoviePage })));
const ActivityPage = lazy(() => import("./pages/Activity").then((m) => ({ default: m.ActivityPage })));
const RequestsPage = lazy(() => import("./pages/Requests").then((m) => ({ default: m.RequestsPage })));
const SettingsPage = lazy(() => import("./pages/Settings").then((m) => ({ default: m.SettingsPage })));

export function App() {
  const [me, setMe] = useState<{ authMode: string; authenticated: boolean; user?: string } | null>(null);

  const check = () =>
    api
      .me()
      .then(setMe)
      .catch(() => setMe({ authMode: "none", authenticated: true }));
  useEffect(() => {
    check();
    setUnauthorizedHandler(() => setMe((m) => (m ? { ...m, authenticated: false } : m)));
  }, []);

  if (!me) return null;
  return (
    <TooltipProvider delayDuration={300}>
      {me.authenticated ? (
        <Router>
          <StoreProvider user={me.user}>
            <ConfirmProvider>
              <Shell authMode={me.authMode} />
            </ConfirmProvider>
          </StoreProvider>
        </Router>
      ) : (
        <Login mode={me.authMode} onDone={check} />
      )}
      <Toaster position="bottom-right" />
    </TooltipProvider>
  );
}

function Shell({ authMode }: { authMode: string }) {
  const { path, search, navigate } = useRouter();
  const [searching, setSearching] = useState(false);
  const [downloads, setDownloads] = useState(false);
  const [shortcuts, setShortcuts] = useState(false);

  useEffect(() => {
    const on = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey;
      if (e.key === "k" && mod) {
        e.preventDefault();
        setSearching((s) => !s);
        return;
      }
      // D also closes the drawer it opened.
      if (e.key.toLowerCase() === "d" && !mod && document.querySelector("[data-downloads]") && !(e.target instanceof HTMLInputElement)) {
        e.preventDefault();
        setDownloads(false);
        return;
      }
      if (keyIsBusy(e) || e.altKey) return;
      if (mod && e.key.toLowerCase() === "z" && !e.shiftKey) {
        e.preventDefault();
        undoLast();
        return;
      }
      if (mod) return;
      const k = e.key.toLowerCase();
      if (e.key === "/") setSearching(true);
      else if (e.key === "?") setShortcuts(true);
      else if (k === "d") setDownloads((d) => !d);
      else if (k === "t") toggleTheme();
      else return;
      e.preventDefault();
    };
    addEventListener("keydown", on);
    return () => removeEventListener("keydown", on);
  }, []);

  const shell = useMemo(
    () => ({ openSearch: () => setSearching(true), toggleDownloads: () => setDownloads((d) => !d), openShortcuts: () => setShortcuts(true) }),
    [],
  );

  const commands: Command[] = [
    { id: "library", label: "Library", run: () => navigate("/") },
    { id: "activity", label: "Downloads", run: () => navigate("/downloads") },
    { id: "requests", label: "Requests", run: () => navigate("/requests") },
    { id: "settings", label: "Admin", run: () => navigate("/admin") },
    { id: "downloads", label: downloads ? "Hide downloads panel" : "Downloads panel", kbd: "D", run: () => setDownloads((d) => !d) },
    { id: "missing", label: "Library: missing only", run: () => navigate("/?filter=missing") },
    { id: "theme", label: "Switch light / dark", kbd: "T", run: toggleTheme },
    { id: "undo", label: "Undo last change", kbd: "⌘Z", run: undoLast },
    { id: "shortcuts", label: "Keyboard shortcuts", kbd: "?", run: () => setShortcuts(true) },
  ];

  let page = <Home />;
  let m: Record<string, string> | null;
  if ((m = match("/tv/tvdb/:tvdb", path))) page = <SeriesPage key={path} tvdb={+m.tvdb} tmdb={+(search.get("tmdb") ?? 0)} />;
  else if ((m = match("/tv/:id", path))) page = <SeriesPage key={path} id={+m.id} />;
  else if ((m = match("/movie/tmdb/:tmdb", path))) page = <MoviePage key={path} tmdb={+m.tmdb} />;
  else if ((m = match("/movie/:id", path))) page = <MoviePage key={path} id={+m.id} />;
  else if (isDownloadsPath(path)) page = <ActivityPage />;
  else if (path === "/requests") page = <RequestsPage />;
  else if (isAdminPath(path)) page = <SettingsPage />;

  return (
    <ShellCtx.Provider value={shell}>
      <Nav authMode={authMode} />
      <main className="mx-auto max-w-[1280px] px-4 pt-6 pb-28 sm:px-6 lg:pt-8">
        <Suspense fallback={null}>{page}</Suspense>
      </main>
      <SearchPalette open={searching} onOpenChange={setSearching} commands={commands} />
      <DownloadsDrawer open={downloads} onOpenChange={setDownloads} />
      <ShortcutsDialog open={shortcuts} onOpenChange={setShortcuts} />
    </ShellCtx.Provider>
  );
}
