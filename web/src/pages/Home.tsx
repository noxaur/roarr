import { useCallback, useEffect, useMemo, useRef, useState, type MouseEvent, type ReactNode } from "react";
import { Check, ChevronDown, Copy, Download, ExternalLink, Eye, FolderSync, LayoutGrid, List, ListChecks, Pin, Radar, Search, Trash2, X } from "lucide-react";
import { toast } from "sonner";
import { api, type LibraryItem } from "@/lib/api";
import { autoGet, run } from "@/lib/actions";
import { bytes, fold } from "@/lib/format";
import { libraryHref } from "@/lib/rank";
import { Link, useRouter } from "@/lib/router";
import { usePointerSelect } from "@/lib/select";
import { keyIsBusy, useShell } from "@/lib/shell";
import { isActive, useStore } from "@/lib/store";
import { toggleTheme } from "@/lib/theme";
import { done } from "@/lib/undo";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuRadioGroup,
  ContextMenuRadioItem,
  ContextMenuSeparator,
  ContextMenuShortcut,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useConfirm } from "@/components/Confirm";
import { Empty, Kbd, Notice } from "@/components/kit";
import { PosterImg } from "@/components/Poster";

type Filter = "all" | "tv" | "movie" | "missing" | "downloading" | "unmonitored" | "pinned";
type Sort = "title" | "year" | "missing" | "size";
type View = "grid" | "list";
type Size = "s" | "m" | "l";

const FILTERS: [Filter, string][] = [
  ["all", "Everything"],
  ["tv", "Shows"],
  ["movie", "Movies"],
  ["missing", "Missing"],
  ["downloading", "Downloading"],
  ["unmonitored", "Not monitored"],
  ["pinned", "Pinned"],
];
const SORTS: [Sort, string][] = [
  ["title", "Title A–Z"],
  ["year", "Newest first"],
  ["missing", "Most missing"],
  ["size", "Largest on disk"],
];
const MIN: Record<Size, number> = { s: 124, m: 164, l: 224 };

const missingOf = (it: LibraryItem) => Math.max(0, it.total - it.have);
const appOf = (it: LibraryItem) => (it.kind === "tv" ? "Sonarr" : "Radarr");
const plural = (n: number, w: string) => `${n} ${w}${n === 1 ? "" : "s"}`;

/** A setting remembered in this browser. */
function useStored<T extends string>(key: string, initial: T, allowed: readonly string[]): [T, (v: T) => void] {
  const [v, setV] = useState<T>(() => {
    try {
      const s = localStorage.getItem(key);
      return s && allowed.includes(s) ? (s as T) : initial;
    } catch {
      return initial;
    }
  });
  const set = useCallback(
    (x: T) => {
      try {
        localStorage.setItem(key, x);
      } catch {
        /* private mode */
      }
      setV(x);
    },
    [key],
  );
  return [v, set];
}

function usePinned(): [Set<string>, (next: Set<string>) => void] {
  const [p, setP] = useState<Set<string>>(() => {
    try {
      return new Set(JSON.parse(localStorage.getItem("roarr.pinned") ?? "[]"));
    } catch {
      return new Set();
    }
  });
  const set = useCallback((next: Set<string>) => {
    try {
      localStorage.setItem("roarr.pinned", JSON.stringify([...next]));
    } catch {
      /* private mode */
    }
    setP(next);
  }, []);
  return [p, set];
}

function greeting(user?: string) {
  const h = new Date().getHours();
  const part = h < 5 ? "Up late" : h < 12 ? "Good morning" : h < 18 ? "Good afternoon" : "Good evening";
  return user ? `${part}, ${user}` : part;
}

export function Home() {
  const { user, library, libraryLoaded, libraryErrors, activity, reloadLibrary, reloadActivity } = useStore();
  const { navigate, search } = useRouter();
  const { openSearch, toggleDownloads, openShortcuts } = useShell();
  const confirm = useConfirm();

  const [filter, setFilter] = useStored<Filter>("roarr.filter", "all", FILTERS.map((f) => f[0]));
  const [sort, setSort] = useStored<Sort>("roarr.sort", "title", SORTS.map((s) => s[0]));
  const [view, setView] = useStored<View>("roarr.view", "grid", ["grid", "list"]);
  const [size, setSize] = useStored<Size>("roarr.size", "m", ["s", "m", "l"]);
  const [pinned, setPinned] = usePinned();
  const [find, setFind] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [cursor, setCursor] = useState<string | null>(null);
  const [peek, setPeek] = useState<LibraryItem | null>(null);
  const [menuFor, setMenuFor] = useState<string | null>(null);
  const anchor = useRef<string | null>(null);
  const findRef = useRef<HTMLInputElement>(null);
  const shelf = useRef<HTMLDivElement>(null);

  // "/?filter=missing" from the command palette.
  useEffect(() => {
    const f = search.get("filter");
    if (f && FILTERS.some(([k]) => k === f)) {
      setFilter(f as Filter);
      navigate("/", { replace: true });
    }
  }, [search, setFilter, navigate]);

  // Progress of every title that is downloading right now.
  const progress = useMemo(() => {
    const m = new Map<string, number>();
    for (const a of activity) if (a.href && isActive(a)) m.set(a.href, Math.max(m.get(a.href) ?? 0, a.progress));
    return m;
  }, [activity]);
  const isDownloading = useCallback((it: LibraryItem) => progress.has(libraryHref(it)), [progress]);

  const counts = useMemo(() => {
    const c: Record<Filter, number> = { all: library.length, tv: 0, movie: 0, missing: 0, downloading: 0, unmonitored: 0, pinned: 0 };
    let disk = 0;
    for (const it of library) {
      c[it.kind]++;
      if (missingOf(it) > 0) c.missing++;
      if (isDownloading(it)) c.downloading++;
      if (!it.monitored) c.unmonitored++;
      if (pinned.has(it.key)) c.pinned++;
      disk += it.sizeOnDisk ?? 0;
    }
    return { ...c, disk };
  }, [library, isDownloading, pinned]);

  const items = useMemo(() => {
    const q = fold(find);
    const keep = (it: LibraryItem) => {
      if (q && ![it.title, ...(it.aliases ?? []), String(it.year ?? "")].some((t) => fold(t).includes(q))) return false;
      switch (filter) {
        case "tv":
        case "movie":
          return it.kind === filter;
        case "missing":
          return missingOf(it) > 0;
        case "downloading":
          return isDownloading(it);
        case "unmonitored":
          return !it.monitored;
        case "pinned":
          return pinned.has(it.key);
        default:
          return true;
      }
    };
    const by: Record<Sort, (a: LibraryItem, b: LibraryItem) => number> = {
      title: () => 0,
      year: (a, b) => (b.year ?? 0) - (a.year ?? 0),
      missing: (a, b) => missingOf(b) - missingOf(a),
      size: (a, b) => (b.sizeOnDisk ?? 0) - (a.sizeOnDisk ?? 0),
    };
    return library.filter(keep).sort((a, b) => Number(pinned.has(b.key)) - Number(pinned.has(a.key)) || by[sort](a, b));
  }, [library, find, filter, sort, pinned, isDownloading]);

  const byKey = useMemo(() => new Map(library.map((i) => [i.key, i])), [library]);
  // Selection survives filtering but never points at titles that are gone.
  useEffect(() => {
    setSelected((s) => {
      const n = new Set([...s].filter((k) => byKey.has(k)));
      return n.size === s.size ? s : n;
    });
  }, [byKey]);

  const pointer = usePointerSelect({ selected, onChange: setSelected });

  // What an action applies to: the selection when the target is part of it,
  // otherwise just the target.
  const targetsOf = useCallback(
    (key?: string | null): LibraryItem[] => {
      const keys = key ? (selected.has(key) && selected.size > 1 ? [...selected] : [key]) : [...selected];
      return keys.map((k) => byKey.get(k)).filter((x): x is LibraryItem => !!x);
    },
    [selected, byKey],
  );

  // ---------- actions ----------
  const getMissing = async (xs: LibraryItem[]) => {
    const todo = xs.filter((it) => missingOf(it) > 0 && !isDownloading(it));
    if (!todo.length) return toast("Nothing missing there");
    for (const it of todo) {
      if (it.kind === "tv") await autoGet({ kind: "tv", seriesId: it.id }, `the missing episodes of ${it.title}`);
      else if (it.tmdbId) await autoGet({ kind: "movie", movies: [it.tmdbId] }, it.title);
    }
    reloadActivity();
    setTimeout(() => reloadLibrary(), 1500);
  };

  const setMonitored = async (xs: LibraryItem[], on: boolean, quiet = false) => {
    const ok = await Promise.all(xs.map((it) => api.patchTitle(it.kind, it.id, { monitored: on }).then(() => true, () => false)));
    await reloadLibrary(true);
    const n = ok.filter(Boolean).length;
    if (n < xs.length) toast.error(`${xs.length - n} of ${xs.length} could not be changed`);
    if (!quiet && n) {
      const what = xs.length === 1 ? xs[0].title : plural(n, "title");
      done(on ? `Monitoring ${what}` : `Stopped monitoring ${what}`, () => setMonitored(xs, !on, true));
    }
  };
  const toggleMonitor = (xs: LibraryItem[]) => {
    if (!xs.length) return;
    setMonitored(xs, !xs.every((i) => i.monitored));
  };

  const togglePin = (xs: LibraryItem[]) => {
    if (!xs.length) return;
    const before = new Set(pinned);
    const unpin = xs.every((i) => pinned.has(i.key));
    const n = new Set(pinned);
    xs.forEach((i) => (unpin ? n.delete(i.key) : n.add(i.key)));
    setPinned(n);
    done(`${unpin ? "Unpinned" : "Pinned"} ${xs.length === 1 ? xs[0].title : plural(xs.length, "title")}`, () => setPinned(before));
  };

  const command = (xs: LibraryItem[], name: "search" | "refresh") => {
    if (!xs.length) return;
    run(
      name === "search" ? `Asking ${xs.length === 1 ? appOf(xs[0]) : "Sonarr & Radarr"} to search…` : "Asking for a rescan…",
      () => Promise.all(xs.map((it) => api.titleCommand(it.kind, it.id, name))),
      name === "search" ? `Search started for ${xs.length === 1 ? xs[0].title : plural(xs.length, "title")}` : "Rescan started",
    ).then((ok) => ok && name === "refresh" && setTimeout(() => reloadLibrary(true), 2500));
  };

  const remove = async (xs: LibraryItem[]) => {
    if (!xs.length) return;
    const one = xs.length === 1 ? xs[0] : null;
    const a = await confirm({
      title: one ? `Remove ${one.title}?` : `Remove ${plural(xs.length, "title")}?`,
      body: <p>{one ? appOf(one) : "Sonarr and Radarr"} forget about {one ? "it" : "them"}. roarr can add {one ? "it" : "them"} back any time you get something.</p>,
      options: [{ id: "files", label: "Delete the files from disk too" }],
      confirm: one ? `Remove from ${appOf(one)}` : `Remove ${xs.length}`,
      danger: true,
    });
    if (!a) return;
    const ok = await run(
      `Removing ${one ? one.title : plural(xs.length, "title")}…`,
      () => Promise.all(xs.map((it) => api.removeTitle(it.kind, it.id, a.checked.files))),
      a.checked.files ? "Removed, files deleted" : "Removed from the library",
    );
    if (ok) {
      setSelected(new Set());
      reloadLibrary(true);
    }
  };

  const copyTitle = (it: LibraryItem) => navigator.clipboard?.writeText(it.title).then(() => toast(`Copied “${it.title}”`), () => {});

  const toggle = (key: string, range = false) => {
    const n = new Set(selected);
    let keys = [key];
    if (range && anchor.current) {
      const order = items.map((i) => i.key);
      const a = order.indexOf(anchor.current);
      const b = order.indexOf(key);
      if (a >= 0 && b >= 0) keys = order.slice(Math.min(a, b), Math.max(a, b) + 1);
    }
    const on = range ? true : !n.has(key);
    keys.forEach((k) => (on ? n.add(k) : n.delete(k)));
    anchor.current = key;
    setCursor(key);
    setSelected(n);
  };
  const selectAll = () => setSelected(new Set(items.map((i) => i.key)));
  const invert = () => setSelected(new Set(items.filter((i) => !selected.has(i.key)).map((i) => i.key)));

  const onItemClick = (e: MouseEvent, it: LibraryItem) => {
    if (pointer.swallowClick()) return e.preventDefault();
    const tick = (e.target as HTMLElement).closest("[data-tick]");
    if (e.shiftKey) {
      e.preventDefault();
      return toggle(it.key, true);
    }
    if (e.metaKey || e.ctrlKey || tick || selected.size > 0) {
      e.preventDefault();
      return toggle(it.key);
    }
    setCursor(it.key);
  };

  // ---------- keyboard ----------
  const focusKey = (key: string) => {
    setCursor(key);
    shelf.current?.querySelector<HTMLElement>(`[data-sel="${CSS.escape(key)}"]`)?.focus({ preventScroll: false });
  };
  const columns = () => {
    if (view === "list") return 1;
    const els = shelf.current?.querySelectorAll<HTMLElement>("[data-sel]");
    if (!els || els.length < 2) return 1;
    const top = els[0].getBoundingClientRect().top;
    let n = 0;
    for (const el of els) {
      if (Math.abs(el.getBoundingClientRect().top - top) > 4) break;
      n++;
    }
    return Math.max(1, n);
  };

  const keyState = useRef({ items, cursor, selected, peek });
  keyState.current = { items, cursor, selected, peek };
  const actions = useRef({ getMissing, toggleMonitor, togglePin, remove, copyTitle, toggle, selectAll, invert, focusKey, columns, targetsOf });
  actions.current = { getMissing, toggleMonitor, togglePin, remove, copyTitle, toggle, selectAll, invert, focusKey, columns, targetsOf };

  useEffect(() => {
    const on = (e: KeyboardEvent) => {
      const { items, cursor, selected, peek } = keyState.current;
      const A = actions.current;
      if (e.key === "Escape" && !document.querySelector("[role=dialog],[role=alertdialog],[role=menu]")) {
        if (document.activeElement === findRef.current) {
          setFind("");
          findRef.current?.blur();
        } else if (selected.size) setSelected(new Set());
        return;
      }
      if (keyIsBusy(e) || e.altKey) return;
      const mod = e.metaKey || e.ctrlKey;
      const k = e.key.length === 1 ? e.key.toLowerCase() : e.key;
      let i = items.findIndex((x) => x.key === cursor);
      const cur = i >= 0 ? items[i] : undefined;
      if (mod) {
        if (k === "a") {
          e.preventDefault();
          A.selectAll();
        } else if (k === "c" && cur && !getSelection()?.toString()) {
          e.preventDefault();
          A.copyTitle(cur);
        }
        return;
      }
      if (i < 0) i = -1;
      const move = (d: number) => {
        const n = items[Math.max(0, Math.min(items.length - 1, i < 0 ? 0 : i + d))];
        if (n) A.focusKey(n.key);
      };
      const targets = () => A.targetsOf(cur?.key ?? null);
      const map: Record<string, () => void> = {
        ArrowRight: () => move(1),
        l: () => move(1),
        ArrowLeft: () => move(-1),
        h: () => move(-1),
        ArrowDown: () => move(A.columns()),
        j: () => move(A.columns()),
        ArrowUp: () => move(-A.columns()),
        k: () => move(-A.columns()),
        x: () => cur && A.toggle(cur.key),
        " ": () => cur && setPeek(peek ? null : cur),
        g: () => A.getMissing(targets()),
        m: () => A.toggleMonitor(targets()),
        p: () => A.togglePin(targets()),
        i: () => A.invert(),
        f: () => findRef.current?.focus(),
        v: () => setView(view === "grid" ? "list" : "grid"),
        "-": () => setSize(size === "l" ? "m" : "s"),
        "=": () => setSize(size === "s" ? "m" : "l"),
        "+": () => setSize(size === "s" ? "m" : "l"),
        Backspace: () => A.remove(targets()),
        Delete: () => A.remove(targets()),
      };
      FILTERS.forEach(([f], n) => (map[String(n + 1)] = () => setFilter(f)));
      const fn = map[k];
      if (fn) {
        e.preventDefault();
        fn();
      }
    };
    addEventListener("keydown", on);
    return () => removeEventListener("keydown", on);
  }, [view, size, setView, setSize, setFilter]);

  // ---------- context menu ----------
  const menuTargets = menuFor ? targetsOf(menuFor) : [];
  const menuOne = menuTargets.length === 1 ? menuTargets[0] : null;
  const onMenuCapture = (e: MouseEvent) => {
    const el = (e.target as HTMLElement).closest<HTMLElement>("[data-sel]");
    setMenuFor(el?.dataset.sel ?? null);
    if (el?.dataset.sel) setCursor(el.dataset.sel);
  };

  const titleMenu = (
    <>
      <ContextMenuLabel className="truncate text-xs font-normal text-muted-foreground">
        {menuOne ? [menuOne.title, menuOne.year].filter(Boolean).join(" · ") : `${menuTargets.length} selected`}
      </ContextMenuLabel>
      {menuOne && (
        <>
          <ContextMenuItem onSelect={() => navigate(libraryHref(menuOne))}>
            <ExternalLink /> Open <ContextMenuShortcut>↵</ContextMenuShortcut>
          </ContextMenuItem>
          <ContextMenuItem onSelect={() => setPeek(menuOne)}>
            <Eye /> Quick look <ContextMenuShortcut>Space</ContextMenuShortcut>
          </ContextMenuItem>
          <ContextMenuItem onSelect={() => open(libraryHref(menuOne), "_blank")}>
            <ExternalLink /> Open in new tab
          </ContextMenuItem>
        </>
      )}
      <ContextMenuSeparator />
      {menuTargets.some((i) => missingOf(i) > 0) && (
        <ContextMenuItem onSelect={() => getMissing(menuTargets)}>
          <Download /> {menuOne ? (menuOne.kind === "movie" ? "Get it" : `Get ${plural(missingOf(menuOne), "missing episode")}`) : "Get everything missing"}
          <ContextMenuShortcut>G</ContextMenuShortcut>
        </ContextMenuItem>
      )}
      {menuOne && (
        <ContextMenuItem onSelect={() => navigate(`${libraryHref(menuOne)}?pick=1`)}>
          <ListChecks /> {menuOne.have > 0 && missingOf(menuOne) === 0 ? "Choose a replacement…" : "Choose release…"}
        </ContextMenuItem>
      )}
      <ContextMenuItem onSelect={() => command(menuTargets, "search")}>
        <Radar /> Let {menuOne ? appOf(menuOne) : "the apps"} search
      </ContextMenuItem>
      <ContextMenuItem onSelect={() => command(menuTargets, "refresh")}>
        <FolderSync /> Rescan disk
      </ContextMenuItem>
      <ContextMenuSeparator />
      <ContextMenuItem onSelect={() => toggleMonitor(menuTargets)}>
        <Eye /> {menuTargets.every((i) => i.monitored) ? "Stop monitoring" : "Monitor"} <ContextMenuShortcut>M</ContextMenuShortcut>
      </ContextMenuItem>
      <ContextMenuItem onSelect={() => togglePin(menuTargets)}>
        <Pin /> {menuTargets.every((i) => pinned.has(i.key)) ? "Unpin" : "Pin to top"} <ContextMenuShortcut>P</ContextMenuShortcut>
      </ContextMenuItem>
      <ContextMenuItem onSelect={() => (menuTargets.length > 1 ? setSelected(new Set()) : menuOne && toggle(menuOne.key))}>
        <Check /> {menuTargets.length > 1 ? "Deselect all" : menuOne && selected.has(menuOne.key) ? "Deselect" : "Select"} <ContextMenuShortcut>X</ContextMenuShortcut>
      </ContextMenuItem>
      {menuOne && (
        <ContextMenuItem onSelect={() => copyTitle(menuOne)}>
          <Copy /> Copy title <ContextMenuShortcut>⌘C</ContextMenuShortcut>
        </ContextMenuItem>
      )}
      <ContextMenuSeparator />
      <ContextMenuItem variant="destructive" onSelect={() => remove(menuTargets)}>
        <Trash2 /> {menuOne ? `Remove from ${appOf(menuOne)}…` : `Remove ${menuTargets.length}…`} <ContextMenuShortcut>⌫</ContextMenuShortcut>
      </ContextMenuItem>
    </>
  );

  const libraryMenu = (
    <>
      <ContextMenuLabel className="text-xs font-normal text-muted-foreground">Library</ContextMenuLabel>
      <ContextMenuItem onSelect={openSearch}>
        <Search /> Search or run… <ContextMenuShortcut>⌘K</ContextMenuShortcut>
      </ContextMenuItem>
      <ContextMenuItem onSelect={() => run("Refreshing from Sonarr & Radarr…", () => reloadLibrary(true), "Library is up to date")}>
        <FolderSync /> Refresh library
      </ContextMenuItem>
      <ContextMenuSeparator />
      <ContextMenuSub>
        <ContextMenuSubTrigger>View as</ContextMenuSubTrigger>
        <ContextMenuSubContent>
          <ContextMenuRadioGroup value={view} onValueChange={(v) => setView(v as View)}>
            <ContextMenuRadioItem value="grid">Grid</ContextMenuRadioItem>
            <ContextMenuRadioItem value="list">List</ContextMenuRadioItem>
          </ContextMenuRadioGroup>
        </ContextMenuSubContent>
      </ContextMenuSub>
      <ContextMenuSub>
        <ContextMenuSubTrigger>Sort by</ContextMenuSubTrigger>
        <ContextMenuSubContent>
          <ContextMenuRadioGroup value={sort} onValueChange={(v) => setSort(v as Sort)}>
            {SORTS.map(([k, l]) => (
              <ContextMenuRadioItem key={k} value={k}>
                {l}
              </ContextMenuRadioItem>
            ))}
          </ContextMenuRadioGroup>
        </ContextMenuSubContent>
      </ContextMenuSub>
      <ContextMenuSub>
        <ContextMenuSubTrigger>Show</ContextMenuSubTrigger>
        <ContextMenuSubContent>
          <ContextMenuRadioGroup value={filter} onValueChange={(v) => setFilter(v as Filter)}>
            {FILTERS.map(([k, l], n) => (
              <ContextMenuRadioItem key={k} value={k}>
                {l} <ContextMenuShortcut>{n + 1}</ContextMenuShortcut>
              </ContextMenuRadioItem>
            ))}
          </ContextMenuRadioGroup>
        </ContextMenuSubContent>
      </ContextMenuSub>
      <ContextMenuSub>
        <ContextMenuSubTrigger>Poster size</ContextMenuSubTrigger>
        <ContextMenuSubContent>
          <ContextMenuRadioGroup value={size} onValueChange={(v) => setSize(v as Size)}>
            <ContextMenuRadioItem value="s">Small</ContextMenuRadioItem>
            <ContextMenuRadioItem value="m">Medium</ContextMenuRadioItem>
            <ContextMenuRadioItem value="l">Large</ContextMenuRadioItem>
          </ContextMenuRadioGroup>
        </ContextMenuSubContent>
      </ContextMenuSub>
      <ContextMenuSeparator />
      <ContextMenuItem onSelect={selectAll}>
        <Check /> Select all <ContextMenuShortcut>⌘A</ContextMenuShortcut>
      </ContextMenuItem>
      <ContextMenuItem onSelect={toggleDownloads}>
        <Download /> Downloads <ContextMenuShortcut>D</ContextMenuShortcut>
      </ContextMenuItem>
      <ContextMenuItem onSelect={toggleTheme}>
        Light / dark <ContextMenuShortcut>T</ContextMenuShortcut>
      </ContextMenuItem>
      <ContextMenuItem onSelect={openShortcuts}>
        Keyboard shortcuts <ContextMenuShortcut>?</ContextMenuShortcut>
      </ContextMenuItem>
    </>
  );

  // ---------- render ----------
  const status = (it: LibraryItem): [string, string] => {
    if (isDownloading(it)) return ["Downloading", "bg-lime-strong"];
    const miss = missingOf(it);
    if (!it.monitored) return ["Not monitored", "bg-muted-foreground/40"];
    if (miss > 0) return [it.kind === "movie" ? "Missing" : `${miss} missing`, "bg-destructive"];
    return ["On disk", "bg-muted-foreground/70"];
  };
  const countOf = (it: LibraryItem) => (it.kind === "tv" ? `${it.have}/${it.total}` : it.sizeOnDisk ? bytes(it.sizeOnDisk) : "—");
  const selItems = targetsOf(null);

  return (
    <div className="space-y-7">
      <header className="flex flex-wrap items-end justify-between gap-x-8 gap-y-5">
        <div>
          <div className="label">{greeting(user)}</div>
          <h1 className="mt-2 text-[40px] leading-none font-semibold tracking-[-0.04em]">Library</h1>
          {libraryLoaded && (
            <p className="mt-2.5 text-sm text-muted-foreground">
              <span className="font-mono tabular">{counts.all}</span> titles · <span className="font-mono tabular">{counts.missing}</span> with gaps ·{" "}
              <span className="font-mono tabular">{counts.downloading}</span> downloading
              {counts.disk > 0 && (
                <>
                  {" "}
                  · <span className="font-mono tabular">{bytes(counts.disk)}</span> on disk
                </>
              )}
            </p>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <label className="relative flex items-center">
            <Search className="pointer-events-none absolute left-2.5 size-3.5 text-muted-foreground" />
            <span className="sr-only">Filter the library</span>
            <input
              ref={findRef}
              value={find}
              onChange={(e) => setFind(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && e.currentTarget.blur()}
              placeholder="Filter…"
              className="h-8 w-44 rounded-lg border bg-transparent pr-8 pl-8 text-[13px] outline-none placeholder:text-muted-foreground focus:border-ring focus:bg-card"
            />
            {find ? (
              <button onClick={() => setFind("")} className="absolute right-2 cursor-pointer text-muted-foreground hover:text-foreground" aria-label="Clear filter">
                <X className="size-3.5" />
              </button>
            ) : (
              <Kbd className="pointer-events-none absolute right-1.5">F</Kbd>
            )}
          </label>
          <Button variant="outline" size="sm" className="h-8" onClick={() => (selected.size ? setSelected(new Set()) : selectAll())}>
            {selected.size ? "Clear" : "Select all"}
          </Button>
          <Segmented
            label="Poster size"
            value={size}
            onChange={(v) => setSize(v as Size)}
            options={[
              ["s", "S", "Small posters (−)"],
              ["m", "M", "Medium posters"],
              ["l", "L", "Large posters (=)"],
            ]}
            disabled={view === "list"}
          />
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="sm" className="h-8 font-normal">
                {SORTS.find((s) => s[0] === sort)?.[1]} <ChevronDown className="opacity-60" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuRadioGroup value={sort} onValueChange={(v) => setSort(v as Sort)}>
                {SORTS.map(([k, l]) => (
                  <DropdownMenuRadioItem key={k} value={k}>
                    {l}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuContent>
          </DropdownMenu>
          <Segmented
            label="View"
            value={view}
            onChange={(v) => setView(v as View)}
            options={[
              ["grid", <LayoutGrid key="g" className="size-3.5" />, "Grid (V)"],
              ["list", <List key="l" className="size-3.5" />, "List (V)"],
            ]}
          />
        </div>
      </header>

      {Object.entries(libraryErrors).map(([svc, err]) => (
        <Notice key={svc}>
          <b className="capitalize">{svc}</b>: {err}
        </Notice>
      ))}

      <div className="flex flex-wrap gap-1 border-b pb-4">
        {FILTERS.filter(([k]) => k === "all" || counts[k] > 0 || k === filter).map(([k, l], n) => (
          <button
            key={k}
            onClick={() => setFilter(k)}
            title={`${l} (${n + 1})`}
            className={cn(
              "flex h-[30px] cursor-pointer items-center gap-2 rounded-full border border-transparent px-3 text-[13px] text-muted-foreground transition-colors outline-none hover:bg-muted hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50",
              filter === k && "border-input bg-muted text-foreground",
            )}
          >
            {l}
            <span className="font-mono text-[11px] text-muted-foreground tabular">{counts[k]}</span>
          </button>
        ))}
      </div>

      <ContextMenu onOpenChange={(o) => !o && setMenuFor(null)}>
        <ContextMenuTrigger asChild>
          <div ref={shelf} onMouseDown={pointer.onMouseDown} onContextMenuCapture={onMenuCapture} className="min-h-[50vh] select-none">
            {!libraryLoaded ? (
              <Grid min={MIN[size]}>
                {Array.from({ length: 12 }, (_, i) => (
                  <div key={i}>
                    <Skeleton className="aspect-[2/3] rounded-lg" />
                    <Skeleton className="mt-2.5 h-4 w-3/4" />
                  </div>
                ))}
              </Grid>
            ) : items.length === 0 ? (
              <Empty>
                {library.length === 0 ? (
                  <>
                    Your library is empty. Press <Kbd>⌘K</Kbd> to find something.
                  </>
                ) : (
                  <>
                    Nothing matches.{" "}
                    <button
                      className="cursor-pointer font-medium text-foreground underline-offset-4 hover:underline"
                      onClick={() => {
                        setFilter("all");
                        setFind("");
                      }}
                    >
                      Show everything
                    </button>
                  </>
                )}
              </Empty>
            ) : view === "grid" ? (
              <Grid min={MIN[size]}>
                {items.map((it) => {
                  const [label, dot] = status(it);
                  const sel = selected.has(it.key);
                  const p = progress.get(libraryHref(it));
                  return (
                    <Link
                      key={it.key}
                      to={libraryHref(it)}
                      data-sel={it.key}
                      draggable={false}
                      aria-pressed={sel}
                      onClick={(e) => onItemClick(e, it)}
                      onFocus={() => setCursor(it.key)}
                      className={cn("group -m-1.5 block min-w-0 rounded-xl p-1.5 outline-none focus-visible:ring-2 focus-visible:ring-ring", cursor === it.key && "ring-1 ring-ring/50")}
                    >
                      <div
                        className={cn(
                          "relative aspect-[2/3] overflow-hidden rounded-lg bg-muted transition-transform duration-200 group-hover:-translate-y-0.5",
                          sel && "ring-2 ring-lime-strong ring-offset-2 ring-offset-background",
                        )}
                      >
                        <PosterImg src={it.poster} title={it.title} year={it.year} />
                        <span
                          data-tick
                          aria-hidden
                          className={cn(
                            "absolute top-2 right-2 grid size-5 place-items-center rounded-full border-[1.5px] border-white/80 bg-black/35 text-transparent opacity-0 transition-opacity group-hover:opacity-100",
                            selected.size > 0 && "opacity-100",
                            sel && "border-lime bg-lime text-ink opacity-100",
                          )}
                        >
                          <Check className="size-3" strokeWidth={3} />
                        </span>
                        {pinned.has(it.key) && (
                          <span className="absolute top-2 left-2 grid size-5 place-items-center rounded-full bg-black/45 text-lime" title="Pinned">
                            <Pin className="size-3" fill="currentColor" />
                          </span>
                        )}
                        {p !== undefined && (
                          <div className="absolute inset-x-0 bottom-0 h-[3px] bg-white/15">
                            <i className="block h-full bg-lime transition-[width] duration-700" style={{ width: `${Math.max(0.03, p) * 100}%` }} />
                          </div>
                        )}
                      </div>
                      <div className="mt-2.5 truncate px-0.5 text-[14px] font-medium tracking-tight" title={it.title}>
                        {it.title}
                      </div>
                      <div className="mt-0.5 flex items-center gap-2 px-0.5 text-[12.5px] text-muted-foreground">
                        <span className={cn("size-1.5 shrink-0 rounded-full", dot)} />
                        <span className="truncate">{label}</span>
                        <span className="ml-auto font-mono text-[11px] tabular">{countOf(it)}</span>
                      </div>
                    </Link>
                  );
                })}
              </Grid>
            ) : (
              <div role="table" className="border-t">
                <div role="row" className="hidden h-9 grid-cols-[minmax(0,1fr)_80px_60px_90px_100px_150px] items-center gap-4 border-b px-3 label sm:grid">
                  <span>Title</span>
                  <span>Type</span>
                  <span>Year</span>
                  <span>Have</span>
                  <span>Size</span>
                  <span>Status</span>
                </div>
                {items.map((it) => {
                  const [label, dot] = status(it);
                  const sel = selected.has(it.key);
                  return (
                    <Link
                      key={it.key}
                      to={libraryHref(it)}
                      data-sel={it.key}
                      draggable={false}
                      aria-pressed={sel}
                      role="row"
                      onClick={(e) => onItemClick(e, it)}
                      onFocus={() => setCursor(it.key)}
                      className={cn(
                        "grid h-12 grid-cols-[minmax(0,1fr)_110px] items-center gap-4 border-b px-3 outline-none hover:bg-muted/60 focus-visible:bg-accent sm:grid-cols-[minmax(0,1fr)_80px_60px_90px_100px_150px]",
                        cursor === it.key && "bg-accent",
                        sel && "bg-lime/20 shadow-[inset_2px_0_0_var(--lime-strong)] hover:bg-lime/25 dark:bg-lime/10",
                      )}
                    >
                      <span className="flex min-w-0 items-center gap-3">
                        <span
                          data-tick
                          aria-hidden
                          className={cn("grid size-4 shrink-0 place-items-center rounded-[4px] border-[1.5px] border-input text-transparent", sel && "border-lime-strong bg-lime-strong text-background")}
                        >
                          <Check className="size-2.5" strokeWidth={3.5} />
                        </span>
                        <span className="h-8 w-[22px] shrink-0 overflow-hidden rounded-[3px] bg-muted">
                          <PosterImg src={it.poster} title={it.title} />
                        </span>
                        <span className="truncate text-sm font-medium">{it.title}</span>
                        {pinned.has(it.key) && <Pin className="size-3 shrink-0 text-lime-strong" fill="currentColor" />}
                      </span>
                      <span className="hidden text-[13px] text-muted-foreground sm:block">{it.kind === "tv" ? "Show" : "Movie"}</span>
                      <span className="hidden font-mono text-xs text-muted-foreground sm:block">{it.year ?? ""}</span>
                      <span className="hidden font-mono text-xs text-muted-foreground tabular sm:block">{it.kind === "tv" ? `${it.have}/${it.total}` : it.have ? "1/1" : "0/1"}</span>
                      <span className="hidden font-mono text-xs text-muted-foreground tabular sm:block">{bytes(it.sizeOnDisk ?? 0)}</span>
                      <span className="flex items-center gap-2 text-[13px] text-muted-foreground">
                        <span className={cn("size-1.5 rounded-full", dot)} />
                        <span className="truncate">{label}</span>
                      </span>
                    </Link>
                  );
                })}
              </div>
            )}
          </div>
        </ContextMenuTrigger>
        <ContextMenuContent className="w-64">{menuFor ? titleMenu : libraryMenu}</ContextMenuContent>
      </ContextMenu>

      {pointer.box && <div className="marquee" style={{ left: pointer.box.x, top: pointer.box.y, width: pointer.box.w, height: pointer.box.h }} />}

      {selected.size > 0 && (
        <div className="pointer-events-none fixed inset-x-0 bottom-[calc(env(safe-area-inset-bottom)+4.75rem)] z-40 flex justify-center px-4 sm:bottom-6">
          <div className="pointer-events-auto flex max-w-full animate-in items-center gap-0.5 overflow-x-auto rounded-xl border bg-popover p-1.5 pl-4 text-sm text-popover-foreground shadow-xl fade-in-0 slide-in-from-bottom-3">
            <span className="mr-2 flex shrink-0 flex-col leading-tight whitespace-nowrap">
              <span>
                <b className="font-mono font-medium tabular">{selected.size}</b> selected
              </span>
              <span className="text-[11px] text-muted-foreground">click or drag to add</span>
            </span>
            <BarBtn onClick={selectAll}>All</BarBtn>
            <BarBtn onClick={invert} hint="I">
              Invert
            </BarBtn>
            <span className="mx-1 h-5 w-px shrink-0 bg-border" />
            {selItems.some((i) => missingOf(i) > 0) && (
              <Button size="sm" className="shrink-0" onClick={() => getMissing(selItems)}>
                <Download /> Get missing
              </Button>
            )}
            <BarBtn onClick={() => toggleMonitor(selItems)} hint="M">
              {selItems.every((i) => i.monitored) ? "Unmonitor" : "Monitor"}
            </BarBtn>
            <BarBtn onClick={() => command(selItems, "search")}>Search</BarBtn>
            <BarBtn onClick={() => togglePin(selItems)} hint="P">
              {selItems.every((i) => pinned.has(i.key)) ? "Unpin" : "Pin"}
            </BarBtn>
            <BarBtn onClick={() => remove(selItems)} hint="⌫" danger>
              Remove
            </BarBtn>
            <span className="mx-1 h-5 w-px shrink-0 bg-border" />
            <Button variant="ghost" size="sm" className="shrink-0 text-muted-foreground" onClick={() => setSelected(new Set())} aria-label="Clear selection">
              <X /> <Kbd className="hidden sm:inline-flex">esc</Kbd>
            </Button>
          </div>
        </div>
      )}

      <QuickLook
        item={peek}
        onClose={() => setPeek(null)}
        status={peek ? status(peek) : undefined}
        downloading={peek ? isDownloading(peek) : false}
        onGet={(it) => {
          setPeek(null);
          getMissing([it]);
        }}
      />
    </div>
  );
}

function Grid({ min, children }: { min: number; children: ReactNode }) {
  return (
    <div className="grid gap-x-5 gap-y-7" style={{ gridTemplateColumns: `repeat(auto-fill, minmax(min(${min}px, 42vw), 1fr))` }}>
      {children}
    </div>
  );
}

function Segmented({
  label,
  value,
  onChange,
  options,
  disabled,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  options: [string, ReactNode, string][];
  disabled?: boolean;
}) {
  return (
    <div role="group" aria-label={label} className={cn("flex h-8 gap-0.5 rounded-lg border p-0.5", disabled && "opacity-50")}>
      {options.map(([v, content, title]) => (
        <Tooltip key={v}>
          <TooltipTrigger asChild>
            <button
              disabled={disabled}
              aria-pressed={value === v}
              aria-label={title}
              onClick={() => onChange(v)}
              className={cn(
                "grid min-w-7 cursor-pointer place-items-center rounded-md px-1.5 font-mono text-[11px] text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-default",
                value === v && "bg-muted text-foreground",
              )}
            >
              {content}
            </button>
          </TooltipTrigger>
          <TooltipContent>{title}</TooltipContent>
        </Tooltip>
      ))}
    </div>
  );
}

function BarBtn({ onClick, hint, danger, children }: { onClick: () => void; hint?: string; danger?: boolean; children: ReactNode }) {
  return (
    <Button variant="ghost" size="sm" className={cn("shrink-0", danger && "text-destructive hover:text-destructive")} onClick={onClick} title={hint}>
      {children}
    </Button>
  );
}

function QuickLook({
  item,
  onClose,
  status,
  downloading,
  onGet,
}: {
  item: LibraryItem | null;
  onClose: () => void;
  status?: [string, string];
  downloading: boolean;
  onGet: (it: LibraryItem) => void;
}) {
  const { navigate } = useRouter();
  return (
    <Dialog open={!!item} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="gap-0 overflow-hidden p-0 sm:max-w-[640px]">
        {item && (
          <div className="flex flex-col sm:flex-row">
            <div className="aspect-[2/3] w-full shrink-0 bg-muted sm:w-[220px]">
              <PosterImg src={item.poster} title={item.title} year={item.year} />
            </div>
            <div className="flex min-w-0 flex-1 flex-col gap-5 p-6">
              <div>
                <div className="label">
                  {item.kind === "tv" ? "Show" : "Movie"}
                  {item.year ? ` · ${item.year}` : ""}
                  {item.network ? ` · ${item.network}` : ""}
                </div>
                <DialogTitle className="mt-2 text-2xl font-semibold tracking-tight">{item.title}</DialogTitle>
                <DialogDescription className="sr-only">Quick look at {item.title}</DialogDescription>
              </div>
              <dl className="grid grid-cols-2 gap-x-6 gap-y-3 text-sm">
                <div>
                  <dt className="text-xs text-muted-foreground">Status</dt>
                  <dd className="mt-0.5 flex items-center gap-2">
                    <span className={cn("size-1.5 rounded-full", status?.[1])} />
                    {status?.[0]}
                  </dd>
                </div>
                <div>
                  <dt className="text-xs text-muted-foreground">On disk</dt>
                  <dd className="mt-0.5 font-mono tabular">{item.kind === "tv" ? `${item.have} of ${item.total} episodes` : item.have ? "Yes" : "No"}</dd>
                </div>
                <div>
                  <dt className="text-xs text-muted-foreground">Size</dt>
                  <dd className="mt-0.5 font-mono tabular">{bytes(item.sizeOnDisk ?? 0)}</dd>
                </div>
                <div>
                  <dt className="text-xs text-muted-foreground">Monitored</dt>
                  <dd className="mt-0.5">{item.monitored ? "Yes" : "No"}</dd>
                </div>
              </dl>
              <div className="mt-auto flex flex-wrap gap-2">
                <Button
                  onClick={() => {
                    onClose();
                    navigate(libraryHref(item));
                  }}
                >
                  Open
                </Button>
                {missingOf(item) > 0 && !downloading && (
                  <Button variant="outline" onClick={() => onGet(item)}>
                    <Download /> {item.kind === "movie" ? "Get it" : `Get ${missingOf(item)} missing`}
                  </Button>
                )}
                <Button
                  variant="outline"
                  onClick={() => {
                    onClose();
                    navigate(`${libraryHref(item)}?pick=1`);
                  }}
                >
                  <ListChecks /> Choose release
                </Button>
              </div>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
