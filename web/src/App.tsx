import { lazy, Suspense, useEffect, useMemo, useState, type ComponentType } from "react";
import { api, setUnauthorizedHandler } from "./lib/api";
import { Router, match, useRouter } from "./lib/router";
import { StoreProvider, forgetLocalCopy } from "./lib/store";
import { pages, warmPages, type Chunk } from "./lib/prefetch";
import { Toaster } from "./components/ui/sonner";
import { TooltipProvider } from "./components/ui/tooltip";
import { ConfirmProvider } from "./components/Confirm";
import { DownloadsDrawer } from "./components/Downloads";
import { Nav, isAdminPath, isDownloadsPath } from "./components/Nav";
import { SearchPalette, type Command } from "./components/SearchPalette";
import { ShortcutsDialog } from "./components/Shortcuts";
import { ShellCtx, keyIsBusy } from "./lib/shell";
import { undoLast } from "./lib/undo";
import { Home } from "./pages/Home";
import { Login } from "./pages/Login";

// Home ships with the first load; every other page is fetched on demand, and
// once the browser is idle (see warmPages).
const SeriesPage = page(pages.series, (m) => m.SeriesPage);
const MoviePage = page(pages.movie, (m) => m.MoviePage);
const ActivityPage = page(pages.activity, (m) => m.ActivityPage);
const RequestsPage = page(pages.requests, (m) => m.RequestsPage);
const SettingsPage = page(pages.settings, (m) => m.SettingsPage);

// React.lazy suspends on the first render even when the code is already
// here, and React then holds the fallback for up to 300 ms. A page whose code
// has arrived renders directly instead.
function page<M, P extends object>(c: Chunk<M>, pick: (m: M) => ComponentType<P>) {
  const Lazy = lazy(() => c().then((m) => ({ default: pick(m) })));
  return function Page(props: P) {
    // Decided once per mount: switching component types would remount the page.
    const [C] = useState<ComponentType<P>>(() => (c.mod ? pick(c.mod) : Lazy));
    return <C {...props} />;
  };
}

type Me = { authMode: string; authenticated: boolean; user?: string };

// The last answer to /api/me. A returning visitor gets the app on the first
// frame instead of a blank page for one round trip; /api/me then confirms it.
const ME = "roarr.me"; // forgetLocalCopy clears it too
function lastMe(): Me | null {
  try {
    const m = JSON.parse(localStorage.getItem(ME) ?? "null") as Me | null;
    return m?.authenticated ? m : null;
  } catch {
    return null;
  }
}
const firstMe = api.me();

export function App() {
  const [me, setMe] = useState<Me | null>(lastMe);

  const check = (req = api.me()) =>
    req
      .then((m) => {
        setMe(m);
        try {
          if (m.authenticated) localStorage.setItem(ME, JSON.stringify({ authMode: m.authMode, authenticated: true, user: m.user }));
          else forgetLocalCopy();
        } catch {
          /* blocked */
        }
      })
      .catch(() => setMe((m) => m ?? { authMode: "none", authenticated: true }));
  useEffect(() => {
    check(firstMe);
    setUnauthorizedHandler(() => {
      forgetLocalCopy();
      setMe((m) => (m ? { ...m, authenticated: false } : m));
    });
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

  useEffect(warmPages, []);

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
    { id: "shows", label: "Shows", run: () => navigate("/shows") },
    { id: "movies", label: "Movies", run: () => navigate("/movies") },
    { id: "activity", label: "Downloads", run: () => navigate("/downloads") },
    { id: "requests", label: "Requests", run: () => navigate("/requests") },
    { id: "settings", label: "Admin", run: () => navigate("/admin") },
    { id: "downloads", label: downloads ? "Hide downloads panel" : "Downloads panel", kbd: "D", run: () => setDownloads((d) => !d) },
    { id: "missing", label: "Library: missing only", run: () => navigate("/?filter=missing") },
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
  else if (path === "/shows") page = <Home kind="tv" />;
  else if (path === "/movies") page = <Home kind="movie" />;
  else if (path === "/requests") page = <RequestsPage />;
  else if (isAdminPath(path)) page = <SettingsPage />;

  return (
    <ShellCtx.Provider value={shell}>
      <Nav authMode={authMode} />
      {/* One padding on every side: 16 px on phones, 48 px next to the sidebar. Wide screens centre the content. */}
      <main className="px-4 pt-4 pb-28 md:ml-[240px] md:p-12">
        <div className="mx-auto max-w-[1104px]">
          <Suspense fallback={null}>{page}</Suspense>
        </div>
      </main>
      <SearchPalette open={searching} onOpenChange={setSearching} commands={commands} />
      <DownloadsDrawer open={downloads} onOpenChange={setDownloads} />
      <ShortcutsDialog open={shortcuts} onOpenChange={setShortcuts} />
    </ShellCtx.Provider>
  );
}
