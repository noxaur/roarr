import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { api, type ActivityItem, type LibraryItem } from "./api";

// App-wide data that many pages share: the library (shipped once, then
// filtered locally), activity for the nav badge, and the request count.

interface Store {
  user?: string;
  library: LibraryItem[];
  libraryErrors: Record<string, string>;
  libraryLoaded: boolean;
  reloadLibrary: (hard?: boolean) => Promise<void>;
  activity: ActivityItem[];
  reloadActivity: () => void;
  /** Pending Seerr requests; null when Seerr is not set up. */
  pendingRequests: number | null;
  reloadRequests: () => void;
}

const Ctx = createContext<Store | null>(null);

export function StoreProvider({ children, user }: { children: ReactNode; user?: string }) {
  const [library, setLibrary] = useState<LibraryItem[]>([]);
  const [libraryErrors, setLibraryErrors] = useState<Record<string, string>>({});
  const [libraryLoaded, setLoaded] = useState(false);
  const [activity, setActivity] = useState<ActivityItem[]>([]);
  const [pendingRequests, setPending] = useState<number | null>(null);
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

  const reloadRequests = useCallback(() => {
    api
      .requests("pending")
      .then((r) => setPending(r.items.length))
      .catch(() => setPending(null));
  }, []);

  useEffect(() => {
    reloadLibrary();
    reloadActivity();
    reloadRequests();
    const a = setInterval(() => reloadLibrary(), 60_000);
    const b = setInterval(() => {
      if (!document.hidden) reloadActivity();
    }, 5_000);
    const c = setInterval(() => {
      if (!document.hidden) reloadRequests();
    }, 60_000);
    return () => {
      clearInterval(a);
      clearInterval(b);
      clearInterval(c);
    };
  }, [reloadLibrary, reloadActivity, reloadRequests]);

  return (
    <Ctx.Provider value={{ user, library, libraryErrors, libraryLoaded, reloadLibrary, activity, reloadActivity, pendingRequests, reloadRequests }}>
      {children}
    </Ctx.Provider>
  );
}

export function useStore(): Store {
  const s = useContext(Ctx);
  if (!s) throw new Error("useStore outside provider");
  return s;
}

/** Downloads that are moving (or about to). */
export const isActive = (a: ActivityItem) => a.status === "downloading" || a.status === "queued" || a.status === "importing";
/** Downloads that need a human. */
export const needsYou = (a: ActivityItem) => a.status === "warning" || a.status === "failed";
