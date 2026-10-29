import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { api, type ActivityItem, type LibraryItem, type VerifyRecord } from "./api";
import { isBlocking, orphans } from "./verify";

// App-wide data that many pages share: the library (shipped once, then
// filtered locally), activity for the nav badge, and the request count.

interface Store {
  user?: string;
  library: LibraryItem[];
  libraryErrors: Record<string, string>;
  libraryLoaded: boolean;
  reloadLibrary: (hard?: boolean) => Promise<void>;
  /** Changes titles in the browser at once, before the server confirms. */
  patchLibrary: (keys: Iterable<string>, patch: Partial<LibraryItem>) => void;
  activity: ActivityItem[];
  reloadActivity: () => void;
  /** Download check records, newest first. Empty when checks are off. */
  verify: VerifyRecord[];
  reloadVerify: () => void;
  /** Pending Seerr requests; null when Seerr is not set up. */
  pendingRequests: number | null;
  reloadRequests: () => void;
}

const Ctx = createContext<Store | null>(null);

// The last library this browser saw. It paints the shelf on the first frame;
// the request below then checks it against the server with its ETag.
const SNAPSHOT = "roarr.library";
interface Snapshot {
  etag: string | null;
  items: LibraryItem[];
  errors: Record<string, string>;
}
function readSnapshot(): Snapshot | null {
  try {
    const raw = localStorage.getItem(SNAPSHOT);
    return raw ? (JSON.parse(raw) as Snapshot) : null;
  } catch {
    return null;
  }
}
function writeSnapshot(s: Snapshot) {
  try {
    localStorage.setItem(SNAPSHOT, JSON.stringify(s));
  } catch {
    /* full or blocked: the next visit loads from the network */
  }
}
/** Removes what this browser keeps of the account: on sign-out, or when the session ends. */
export function forgetLocalCopy() {
  try {
    localStorage.removeItem(SNAPSHOT);
    localStorage.removeItem("roarr.me");
  } catch {
    /* blocked */
  }
}

const snapshot = readSnapshot();
const fetchLibrary = (etag: string | null, hard = false) =>
  fetch(`/api/library${hard ? "?refresh=1" : ""}`, { headers: etag && !hard ? { "If-None-Match": etag } : {} });
// Sent while the module loads, in parallel with /api/me, not after it.
let early: Promise<Response> | null = fetchLibrary(snapshot?.etag ?? null);

export function StoreProvider({ children, user }: { children: ReactNode; user?: string }) {
  const [library, setLibrary] = useState<LibraryItem[]>(() => snapshot?.items ?? []);
  const [libraryErrors, setLibraryErrors] = useState<Record<string, string>>(() => snapshot?.errors ?? {});
  const [libraryLoaded, setLoaded] = useState(!!snapshot);
  const [activity, setActivity] = useState<ActivityItem[]>([]);
  const [verify, setVerify] = useState<VerifyRecord[]>([]);
  const [pendingRequests, setPending] = useState<number | null>(null);
  const etag = useRef<string | null>(snapshot?.etag ?? null);

  const reloadLibrary = useCallback(async (hard = false) => {
    const first = early;
    early = null;
    let res = await (first && !hard ? first : fetchLibrary(etag.current, hard)).catch(() => null);
    // The early request can go out before the sign-in that mounted us.
    if (first && !hard && res?.status === 401) res = await fetchLibrary(etag.current).catch(() => null);
    if (!res || res.status === 304 || !res.ok) return;
    etag.current = res.headers.get("ETag");
    const j = (await res.json()) as { items: LibraryItem[]; errors: Record<string, string> };
    setLibrary(j.items);
    setLibraryErrors(j.errors ?? {});
    setLoaded(true);
    const save = () => writeSnapshot({ etag: etag.current, items: j.items, errors: j.errors ?? {} });
    (window.requestIdleCallback ?? setTimeout)(save);
  }, []);

  const patchLibrary = useCallback((keys: Iterable<string>, patch: Partial<LibraryItem>) => {
    const ks = new Set(keys);
    setLibrary((l) => l.map((it) => (ks.has(it.key) ? { ...it, ...patch } : it)));
  }, []);

  const reloadActivity = useCallback(() => {
    api
      .activity()
      .then((r) => setActivity(r.items))
      .catch(() => {});
  }, []);

  const reloadVerify = useCallback(() => {
    api
      .verify()
      .then((r) => setVerify(r.items ?? []))
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
    reloadVerify();
    reloadRequests();
    const a = setInterval(() => reloadLibrary(), 60_000);
    const b = setInterval(() => {
      if (document.hidden) return;
      reloadActivity();
      reloadVerify();
    }, 5_000);
    const c = setInterval(() => {
      if (!document.hidden) reloadRequests();
    }, 60_000);
    return () => {
      clearInterval(a);
      clearInterval(b);
      clearInterval(c);
    };
  }, [reloadLibrary, reloadActivity, reloadVerify, reloadRequests]);

  return (
    <Ctx.Provider value={{ user, library, libraryErrors, libraryLoaded, reloadLibrary, patchLibrary, activity, reloadActivity, verify, reloadVerify, pendingRequests, reloadRequests }}>
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

/** How many things wait on a person: stuck downloads and failed checks. */
export function problemCount(activity: ActivityItem[], verify: VerifyRecord[]): number {
  const failing = new Set(verify.filter(isBlocking).map((r) => r.hash.toLowerCase()));
  const queued = activity.filter((a) => needsYou(a) || (!!a.downloadId && failing.has(a.downloadId.toLowerCase()))).length;
  return queued + orphans(verify, activity).length;
}
