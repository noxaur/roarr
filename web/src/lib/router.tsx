import { createContext, useCallback, useContext, useEffect, useRef, useState, type ComponentProps, type FocusEvent, type ReactNode, type MouseEvent, type PointerEvent } from "react";
import { prefetch } from "./prefetch";

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

// A pointer that rests this long on a link is about to click it. Shorter
// would fetch every poster the pointer crosses on its way somewhere else.
const INTENT_MS = 60;

export function Link({
  to,
  onClick,
  onPointerEnter,
  onPointerLeave,
  onPointerDown,
  onFocus,
  onBlur,
  ...rest
}: { to: string } & Omit<ComponentProps<"a">, "href">) {
  const { navigate } = useRouter();
  const timer = useRef(0);
  useEffect(() => () => clearTimeout(timer.current), []);
  const go = (e: MouseEvent<HTMLAnchorElement>) => {
    onClick?.(e);
    if (e.defaultPrevented || e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
    e.preventDefault();
    navigate(to);
  };
  return (
    <a
      href={to}
      onClick={go}
      onPointerEnter={(e: PointerEvent<HTMLAnchorElement>) => {
        onPointerEnter?.(e);
        clearTimeout(timer.current);
        timer.current = window.setTimeout(() => prefetch(to), INTENT_MS);
      }}
      onPointerLeave={(e: PointerEvent<HTMLAnchorElement>) => {
        onPointerLeave?.(e);
        clearTimeout(timer.current);
      }}
      // A press comes about 100 ms before the click: start the fetch now.
      onPointerDown={(e: PointerEvent<HTMLAnchorElement>) => {
        onPointerDown?.(e);
        clearTimeout(timer.current);
        prefetch(to);
      }}
      // Arrow keys move focus fast through the library grid: wait for it to rest.
      onFocus={(e: FocusEvent<HTMLAnchorElement>) => {
        onFocus?.(e);
        clearTimeout(timer.current);
        if (e.currentTarget.matches(":focus-visible")) timer.current = window.setTimeout(() => prefetch(to), INTENT_MS * 2);
      }}
      onBlur={(e: FocusEvent<HTMLAnchorElement>) => {
        onBlur?.(e);
        clearTimeout(timer.current);
      }}
      {...rest}
    />
  );
}
