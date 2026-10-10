import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { api, type ActivityItem, type LibraryItem } from "./api";

// App-wide data that many pages share: the library (shipped once, then
// filtered locally), activity counts for the nav badge, and the UI mode.

export type Mode = "manual" | "auto";

interface Store {
  library: LibraryItem[];
  libraryErrors: Record<string, string>;
  libraryLoaded: boolean;
  reloadLibrary: (hard?: boolean) => Promise<void>;
  activity: ActivityItem[];
  reloadActivity: () => void;
  mode: Mode;
  setMode: (m: Mode) => void;
}

const Ctx = createContext<Store | null>(null);

export function StoreProvider({ children, defaultMode }: { children: ReactNode; defaultMode: Mode }) {
  const [library, setLibrary] = useState<LibraryItem[]>([]);
  const [libraryErrors, setLibraryErrors] = useState<Record<string, string>>({});
  const [libraryLoaded, setLoaded] = useState(false);
  const [activity, setActivity] = useState<ActivityItem[]>([]);
  const [mode, setModeState] = useState<Mode>(() => (localStorage.getItem("roarr.mode") as Mode) || defaultMode);
  const etag = useRef<string | null>(null);

  const reloadLibrary = useCallback(async (hard = false) => {
    const res = await fetch(`/api/library${hard ? "?refresh=1" : ""}`, {
      headers: etag.current && !hard ? { "If-None-Match": etag.current } : {},
    });
    if (res.status === 304) return;
    if (!res.ok) return;
    etag.current = res.headers.get("ETag");
    const j = (await res.json()) as { items: LibraryItem[]; errors: Record<string, string> };
    setLibrary(j.items);
    setLibraryErrors(j.errors ?? {});
    setLoaded(true);
  }, []);

  const reloadActivity = useCallback(() => {
    api
      .activity()
      .then((r) => setActivity(r.items))
      .catch(() => {});
  }, []);

  useEffect(() => {
    reloadLibrary();
    reloadActivity();
    const a = setInterval(() => reloadLibrary(), 60_000);
    const b = setInterval(() => {
      if (!document.hidden) reloadActivity();
    }, 5_000);
    return () => {
      clearInterval(a);
      clearInterval(b);
    };
  }, [reloadLibrary, reloadActivity]);

  const setMode = (m: Mode) => {
    localStorage.setItem("roarr.mode", m);
    setModeState(m);
  };

  return (
    <Ctx.Provider value={{ library, libraryErrors, libraryLoaded, reloadLibrary, activity, reloadActivity, mode, setMode }}>{children}</Ctx.Provider>
  );
}

export function useStore(): Store {
  const s = useContext(Ctx);
  if (!s) throw new Error("useStore outside provider");
  return s;
}
