import { useEffect, useRef, useState } from "react";
import { api, setUnauthorizedHandler } from "./lib/api";
import { Link, Router, match, useRouter } from "./lib/router";
import { StoreProvider, useStore, type Mode } from "./lib/store";
import { ToastProvider } from "./components/Toasts";
import { Logo, SearchIcon } from "./components/Icons";
import { Home } from "./pages/Home";
import { SeriesPage } from "./pages/Series";
import { MoviePage } from "./pages/Movie";
import { ActivityPage } from "./pages/Activity";
import { RequestsPage } from "./pages/Requests";
import { SettingsPage } from "./pages/Settings";
import { Login } from "./pages/Login";
import { cls } from "./lib/format";

export function App() {
  const [me, setMe] = useState<{ authMode: string; authenticated: boolean; user?: string } | null>(null);
  const [defaultMode, setDefaultMode] = useState<Mode>("manual");

  const check = () =>
    api
      .me()
      .then(setMe)
      .catch(() => setMe({ authMode: "none", authenticated: true }));
  useEffect(() => {
    check();
    setUnauthorizedHandler(() => setMe((m) => (m ? { ...m, authenticated: false } : m)));
  }, []);
  useEffect(() => {
    if (me?.authenticated)
      api
        .settings()
        .then((s) => setDefaultMode(s.settings.behaviour.defaultMode))
        .catch(() => {});
  }, [me?.authenticated]);

  if (!me) return null;
  if (!me.authenticated) return <Login mode={me.authMode} onDone={check} />;
  return (
    <ToastProvider>
      <Router>
        <StoreProvider defaultMode={defaultMode}>
          <Shell user={me.user} authMode={me.authMode} />
        </StoreProvider>
      </Router>
    </ToastProvider>
  );
}

function Shell({ user, authMode }: { user?: string; authMode: string }) {
  const { path, search, navigate } = useRouter();
  const { activity } = useStore();
  const q = path === "/" ? (search.get("q") ?? "") : "";
  const [text, setText] = useState(q);
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => setText(q), [q]);
  useEffect(() => {
    const on = (e: KeyboardEvent) => {
      if (e.key === "/" && document.activeElement?.tagName !== "INPUT" && document.activeElement?.tagName !== "TEXTAREA") {
        e.preventDefault();
        input.current?.focus();
      }
    };
    addEventListener("keydown", on);
    return () => removeEventListener("keydown", on);
  }, []);

  const onType = (v: string) => {
    setText(v);
    navigate(v ? `/?q=${encodeURIComponent(v)}` : "/", { replace: path === "/" });
  };

  const active = activity.filter((a) => a.status === "downloading" || a.status === "queued" || a.status === "importing").length;
  const problems = activity.filter((a) => a.status === "warning" || a.status === "failed").length;

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
      <header className="topbar">
        <Link to="/" className="brand">
          <Logo />
          <span>roarr</span>
        </Link>
        <div className="search">
          <SearchIcon className="icon" />
          <input
            ref={input}
            value={text}
            placeholder="Search shows and movies…"
            onChange={(e) => onType(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Escape") {
                onType("");
                input.current?.blur();
              }
            }}
          />
          {!text && <kbd>/</kbd>}
        </div>
        <nav className="nav">
          <Link to="/" className={cls(path === "/" && "active")}>
            Library
          </Link>
          <Link to="/activity" className={cls(path === "/activity" && "active")}>
            Activity
            {active > 0 && <span className="badge">{active}</span>}
            {problems > 0 && <span className="badge" style={{ background: "var(--warn)" }}>{problems}</span>}
          </Link>
          <Link to="/requests" className={cls(path === "/requests" && "active")}>
            Requests
          </Link>
          <Link to="/settings" className={cls(path === "/settings" && "active")}>
            Settings
          </Link>
        </nav>
        {authMode !== "none" && (
          <button
            className="btn ghost sm"
            title={user ? `Signed in as ${user}` : undefined}
            onClick={() => api.logout().then(() => location.reload())}
          >
            Sign out
          </button>
        )}
      </header>
      <main>{page}</main>
    </>
  );
}
