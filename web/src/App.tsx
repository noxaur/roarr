import { lazy, Suspense, useEffect, useState } from "react";
import { api, setUnauthorizedHandler } from "./lib/api";
import { Router, match, useRouter } from "./lib/router";
import { StoreProvider } from "./lib/store";
import { Toaster } from "./components/ui/sonner";
import { TooltipProvider } from "./components/ui/tooltip";
import { Nav } from "./components/Nav";
import { SearchPalette } from "./components/SearchPalette";
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
            <Shell authMode={me.authMode} />
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
  const { path, search } = useRouter();
  const [searching, setSearching] = useState(false);

  useEffect(() => {
    const on = (e: KeyboardEvent) => {
      const typing = e.target instanceof HTMLElement && (e.target.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(e.target.tagName));
      if ((e.key === "k" && (e.metaKey || e.ctrlKey)) || (e.key === "/" && !typing)) {
        e.preventDefault();
        setSearching((s) => !s);
      }
    };
    addEventListener("keydown", on);
    return () => removeEventListener("keydown", on);
  }, []);

  let page = <Home />;
  let m: Record<string, string> | null;
  if ((m = match("/tv/tvdb/:tvdb", path))) page = <SeriesPage key={path} tvdb={+m.tvdb} tmdb={+(search.get("tmdb") ?? 0)} />;
  else if ((m = match("/tv/:id", path))) page = <SeriesPage key={path} id={+m.id} />;
  else if ((m = match("/movie/tmdb/:tmdb", path))) page = <MoviePage key={path} tmdb={+m.tmdb} />;
  else if ((m = match("/movie/:id", path))) page = <MoviePage key={path} id={+m.id} />;
  else if (path === "/activity") page = <ActivityPage />;
  else if (path === "/requests") page = <RequestsPage />;
  else if (path === "/settings") page = <SettingsPage />;

  return (
    <>
      <Nav onSearch={() => setSearching(true)} authMode={authMode} />
      <main className="mx-auto max-w-[1360px] px-4 pt-8 pb-24 sm:px-8 lg:px-10 lg:pt-10">
        <Suspense fallback={null}>{page}</Suspense>
      </main>
      <SearchPalette open={searching} onOpenChange={setSearching} />
    </>
  );
}
