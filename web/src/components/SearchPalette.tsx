import { useEffect, useMemo, useState } from "react";
import { Command as CommandPrimitive } from "cmdk";
import { Download, Search } from "lucide-react";
import { api, type DiscoverItem, type LibraryItem } from "@/lib/api";
import { autoGet } from "@/lib/actions";
import { fold } from "@/lib/format";
import { discoverHref, libraryHref, rankLibrary } from "@/lib/rank";
import { cached, forget, peek } from "@/lib/cache";
import { prefetch } from "@/lib/prefetch";
import { useRouter } from "@/lib/router";
import { useStore } from "@/lib/store";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { CellStrip, Spinner, State, libraryCells } from "./kit";

type Row = { key: string; href: string; lib?: LibraryItem; remote?: DiscoverItem };

export interface Command {
  id: string;
  label: string;
  kbd?: string;
  run: () => void;
}

/**
 * Search from anywhere. Library hits are instant (the library is already in
 * the browser); everything else comes from TMDB through Seerr. Movies can be
 * fetched straight from here with ⌘↵.
 */
export function SearchPalette({ open, onOpenChange, commands = [] }: { open: boolean; onOpenChange: (o: boolean) => void; commands?: Command[] }) {
  const { library, reloadActivity, reloadLibrary } = useStore();
  const { navigate } = useRouter();
  const [q, setQ] = useState("");
  const [remote, setRemote] = useState<DiscoverItem[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState("");
  const [getting, setGetting] = useState<Set<string>>(new Set());

  useEffect(() => {
    if (!open) {
      setQ("");
      setRemote(null);
      setError(null);
    }
  }, [open]);

  useEffect(() => {
    const term = q.trim();
    if (term.length < 2) {
      setRemote(null);
      setLoading(false);
      return;
    }
    setError(null);
    // A term seen before (backspace, retyping) answers from the cache at once.
    const key = `discover:${term.toLowerCase()}`;
    const hit = peek<{ results: DiscoverItem[] }>(key);
    if (hit) {
      setRemote(hit.results);
      setLoading(false);
      return;
    }
    setLoading(true);
    let alive = true;
    const t = setTimeout(() => {
      cached(key, () => api.discover(term), 5 * 60_000)
        .then((r) => {
          if (!alive) return;
          setRemote(r.results);
        })
        .catch((e) => alive && setError(String(e.message ?? e)))
        .finally(() => alive && setLoading(false));
    }, 150);
    return () => {
      alive = false;
      clearTimeout(t);
    };
  }, [q]);

  const local = useMemo(() => rankLibrary(library, q).slice(0, 6), [library, q]);
  const rows = useMemo(() => {
    const libKeys = new Set(local.map((l) => `${l.kind}:${l.id}`));
    const a: Row[] = local.map((l) => ({ key: `lib:${l.key}`, href: libraryHref(l), lib: l }));
    const b: Row[] = (remote ?? [])
      .filter((r) => !(r.inLibrary && libKeys.has(`${r.kind}:${r.libraryId}`)))
      .slice(0, 12)
      .map((r) => ({ key: `tmdb:${r.kind}:${r.tmdbId ?? r.tvdbId}`, href: discoverHref(r), remote: r }));
    return { a, b };
  }, [local, remote]);

  // The highlighted row is the likely pick: fetch its page while the user decides.
  useEffect(() => {
    const row = [...rows.a, ...rows.b].find((r) => r.key === selected);
    if (!open || !row) return;
    const t = setTimeout(() => prefetch(row.href), 120);
    return () => clearTimeout(t);
  }, [open, selected, rows]);

  const go = (href: string) => {
    onOpenChange(false);
    navigate(href);
  };

  const canGet = (r?: DiscoverItem) => r && r.kind === "movie" && !r.inLibrary && !!r.tmdbId;
  const get = async (row: Row) => {
    const r = row.remote!;
    setGetting((s) => new Set(s).add(row.key));
    const res = await autoGet({ kind: "movie", movies: [r.tmdbId!] }, r.title);
    setGetting((s) => {
      const n = new Set(s);
      n.delete(row.key);
      return n;
    });
    if (res) {
      forget("discover:"); // "in library" changed
      reloadActivity();
      setTimeout(() => reloadLibrary(), 1200);
    }
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key !== "Enter" || !(e.metaKey || e.ctrlKey)) return;
    const row = [...rows.a, ...rows.b].find((r) => r.key === selected);
    if (row && canGet(row.remote)) {
      e.preventDefault();
      get(row);
    }
  };

  const term = q.trim();
  const cmds = commands.filter((c) => !term || fold(c.label).includes(fold(term)));
  const runCmd = (c: Command) => {
    onOpenChange(false);
    c.run();
  };
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent showCloseButton={false} className="top-[12vh] translate-y-0 gap-0 overflow-hidden p-0 sm:max-w-[680px]">
        <DialogTitle className="sr-only">Search</DialogTitle>
        <DialogDescription className="sr-only">Search your library and everything on TMDB</DialogDescription>
        <CommandPrimitive shouldFilter={false} loop value={selected} onValueChange={setSelected} onKeyDown={onKeyDown} className="flex flex-col">
          <div className="flex h-12 items-center gap-3 border-b px-4">
            <Search className="size-5 shrink-0 text-muted-foreground" />
            <CommandPrimitive.Input
              value={q}
              onValueChange={setQ}
              placeholder="Find a show or movie"
              className="h-full flex-1 bg-transparent text-[16px] outline-none placeholder:text-muted-foreground/70"
            />
            {loading && <Spinner />}
          </div>
          <CommandPrimitive.List className="max-h-[min(60vh,520px)] overflow-y-auto p-2">
            {term.length > 0 && !loading && rows.a.length + rows.b.length + cmds.length === 0 && (
              <CommandPrimitive.Empty className="px-3 py-8 text-center text-sm text-muted-foreground">{error ?? `No match for “${term}”.`}</CommandPrimitive.Empty>
            )}
            {rows.a.length > 0 && (
              <CommandPrimitive.Group heading="In the library" className="pb-2 **:[[cmdk-group-heading]]:label-caps **:[[cmdk-group-heading]]:px-3 **:[[cmdk-group-heading]]:py-2">
                {rows.a.map((row) => {
                  const it = row.lib!;
                  return (
                    <Item key={row.key} value={row.key} onSelect={() => go(row.href)}>
                      <Meta title={it.title} sub={[it.year, it.kind === "tv" ? "show" : "film"]} />
                      <CellStrip cells={libraryCells(it, false, 24)} className="max-w-[288px] justify-end" />
                    </Item>
                  );
                })}
              </CommandPrimitive.Group>
            )}
            {rows.b.length > 0 && (
              <CommandPrimitive.Group heading="Not in the library" className="**:[[cmdk-group-heading]]:label-caps **:[[cmdk-group-heading]]:px-3 **:[[cmdk-group-heading]]:py-2">
                {rows.b.map((row) => {
                  const r = row.remote!;
                  return (
                    <Item key={row.key} value={row.key} onSelect={() => go(row.href)}>
                      <Meta title={r.title} sub={[r.year, r.kind === "tv" ? "show" : "film", r.requested ? "requested" : null]} />
                      {r.inLibrary ? (
                        <State tone="have">In library</State>
                      ) : canGet(r) ? (
                        <Button
                          size="sm"
                          variant={selected === row.key ? "default" : "outline"}
                          disabled={getting.has(row.key)}
                          onPointerDown={(e) => e.stopPropagation()}
                          onClick={(e) => {
                            e.stopPropagation();
                            get(row);
                          }}
                        >
                          {getting.has(row.key) ? <Spinner /> : <Download />} Get
                        </Button>
                      ) : (
                        <Button size="sm" variant="outline" tabIndex={-1}>
                          Open
                        </Button>
                      )}
                    </Item>
                  );
                })}
              </CommandPrimitive.Group>
            )}
            {cmds.length > 0 && (
              <CommandPrimitive.Group heading="Go to" className="**:[[cmdk-group-heading]]:label-caps **:[[cmdk-group-heading]]:px-3 **:[[cmdk-group-heading]]:py-2">
                {cmds.map((c) => (
                  <Item key={c.id} value={`cmd:${c.id}`} onSelect={() => runCmd(c)}>
                    <span className="flex-1 text-sm">{c.label}</span>
                  </Item>
                ))}
              </CommandPrimitive.Group>
            )}
          </CommandPrimitive.List>
        </CommandPrimitive>
      </DialogContent>
    </Dialog>
  );
}

function Item({ value, onSelect, children }: { value: string; onSelect: () => void; children: React.ReactNode }) {
  return (
    <CommandPrimitive.Item
      value={value}
      onSelect={onSelect}
      className="flex min-h-12 cursor-pointer items-center gap-4 rounded-[2px] px-3 py-2 outline-none data-[selected=true]:bg-accent"
    >
      {children}
    </CommandPrimitive.Item>
  );
}

function Meta({ title, sub }: { title: string; sub: (string | number | null | undefined)[] }) {
  return (
    <div className="min-w-0 flex-1">
      <div className="truncate text-[14px]">{title}</div>
      <div className="meta truncate font-mono">{sub.filter(Boolean).join(" · ")}</div>
    </div>
  );
}
