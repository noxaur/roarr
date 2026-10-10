import { createContext, useCallback, useContext, useEffect, useState, type ReactNode, type MouseEvent } from "react";

// A tiny history router: roarr has a handful of routes and does not need a
// dependency for them.

interface RouterState {
  path: string;
  search: URLSearchParams;
  navigate: (to: string, opts?: { replace?: boolean }) => void;
}

const Ctx = createContext<RouterState | null>(null);

export function Router({ children }: { children: ReactNode }) {
  const [loc, setLoc] = useState(() => ({ path: location.pathname, search: location.search }));
  useEffect(() => {
    const on = () => setLoc({ path: location.pathname, search: location.search });
    addEventListener("popstate", on);
    return () => removeEventListener("popstate", on);
  }, []);
  const navigate = useCallback((to: string, opts?: { replace?: boolean }) => {
    if (opts?.replace) history.replaceState(null, "", to);
    else history.pushState(null, "", to);
    const u = new URL(to, location.origin);
    setLoc({ path: u.pathname, search: u.search });
    if (!opts?.replace) scrollTo({ top: 0 });
  }, []);
  return <Ctx.Provider value={{ path: loc.path, search: new URLSearchParams(loc.search), navigate }}>{children}</Ctx.Provider>;
}

export function useRouter(): RouterState {
  const r = useContext(Ctx);
  if (!r) throw new Error("useRouter outside Router");
  return r;
}

/** Matches "/tv/:id" style patterns. */
export function match(pattern: string, path: string): Record<string, string> | null {
  const p = pattern.split("/").filter(Boolean);
  const s = path.split("/").filter(Boolean);
  if (p.length !== s.length) return null;
  const out: Record<string, string> = {};
  for (let i = 0; i < p.length; i++) {
    if (p[i].startsWith(":")) out[p[i].slice(1)] = decodeURIComponent(s[i]);
    else if (p[i] !== s[i]) return null;
  }
  return out;
}

export function Link({ to, children, className, title }: { to: string; children: ReactNode; className?: string; title?: string }) {
  const { navigate } = useRouter();
  const onClick = (e: MouseEvent) => {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
    e.preventDefault();
    navigate(to);
  };
  return (
    <a href={to} onClick={onClick} className={className} title={title}>
      {children}
    </a>
  );
}
