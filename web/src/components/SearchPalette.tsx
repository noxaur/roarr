import { useEffect, useMemo, useState } from "react";
import { Command as CommandPrimitive } from "cmdk";
import { Download, Search } from "lucide-react";
import { api, type DiscoverItem, type LibraryItem } from "@/lib/api";
import { autoGet } from "@/lib/actions";
import { bytes, fold } from "@/lib/format";
import { discoverHref, libraryHref, rankLibrary } from "@/lib/rank";
import { useRouter } from "@/lib/router";
import { useStore } from "@/lib/store";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { Kbd, Spinner } from "./kit";
import { Thumb } from "./Poster";

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
  const [ms, setMs] = useState<number | null>(null);
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
    setLoading(true);
    setError(null);
    let alive = true;
    const t = setTimeout(() => {
      const started = performance.now();
      api
        .discover(term)
        .then((r) => {
          if (!alive) return;
          setRemote(r.results);
          setMs(Math.round(performance.now() - started));
        })
        .catch((e) => alive && setError(String(e.message ?? e)))
        .finally(() => alive && setLoading(false));
    }, 220);
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
          <div className="flex h-14 items-center gap-3 border-b border-border px-5">
            <Search className="size-5 shrink-0 text-muted-foreground" />
            <CommandPrimitive.Input
              value={q}
              onValueChange={setQ}
              placeholder="Search shows, movies, commands…"
              className="h-full flex-1 bg-transparent text-lg font-medium tracking-tight outline-none placeholder:text-muted-foreground/70"
            />
            {loading && <Spinner />}
            <Kbd>esc</Kbd>
          </div>
          <CommandPrimitive.List className="max-h-[min(60vh,520px)] overflow-y-auto px-2.5 py-2">
            {term.length === 0 && (
              <div className="px-3 pt-3 pb-1 text-sm text-muted-foreground">Type a title. Your library answers instantly; TMDB follows in a blink.</div>
            )}
            {term.length > 0 && !loading && rows.a.length + rows.b.length + cmds.length === 0 && (
              <CommandPrimitive.Empty className="px-3 py-8 text-center text-sm text-muted-foreground">{error ?? `Nothing found for “${term}”.`}</CommandPrimitive.Empty>
            )}
            {rows.a.length > 0 && (
              <CommandPrimitive.Group heading="In your library · instant" className="pb-2 **:[[cmdk-group-heading]]:label **:[[cmdk-group-heading]]:px-2.5 **:[[cmdk-group-heading]]:py-2">
                {rows.a.map((row) => {
                  const it = row.lib!;
                  const missing = it.total - it.have;
                  return (
                    <Item key={row.key} value={row.key} onSelect={() => go(row.href)}>
                      <Thumb src={it.poster} title={it.title} className="w-9" />
                      <Meta title={it.title} sub={[it.kind === "tv" ? "Show" : "Movie", it.year, it.kind === "tv" ? `${it.have}/${it.total} eps` : it.sizeOnDisk ? bytes(it.sizeOnDisk) : null]} />
                      {it.kind === "movie" ? (
                        it.have ? <Badge variant="ok">On disk</Badge> : <Badge variant="pink">Missing</Badge>
                      ) : missing > 0 ? (
                        <Badge variant="pink">{missing} missing</Badge>
                      ) : null}
                    </Item>
                  );
                })}
              </CommandPrimitive.Group>
            )}
            {rows.b.length > 0 && (
              <CommandPrimitive.Group heading="Not in your library · TMDB" className="**:[[cmdk-group-heading]]:label **:[[cmdk-group-heading]]:px-2.5 **:[[cmdk-group-heading]]:py-2">
                {rows.b.map((row) => {
                  const r = row.remote!;
                  return (
                    <Item key={row.key} value={row.key} onSelect={() => go(row.href)}>
                      <Thumb src={r.poster} title={r.title} className="w-9" />
                      <Meta title={r.title} sub={[r.kind === "tv" ? "Show" : "Movie", r.year, r.requested ? "requested" : null]} />
                      {r.inLibrary ? (
                        <Badge variant="ok">In library</Badge>
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
              <CommandPrimitive.Group heading="Commands" className="**:[[cmdk-group-heading]]:label **:[[cmdk-group-heading]]:px-2.5 **:[[cmdk-group-heading]]:py-2">
                {cmds.map((c) => (
                  <Item key={c.id} value={`cmd:${c.id}`} onSelect={() => runCmd(c)}>
                    <span className="size-2 shrink-0 rounded-[2px] bg-muted-foreground/40" />
                    <span className="flex-1 text-sm">{c.label}</span>
                    {c.kbd && <Kbd>{c.kbd}</Kbd>}
                  </Item>
                ))}
              </CommandPrimitive.Group>
            )}
          </CommandPrimitive.List>
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 border-t border-border px-5 py-3 font-mono text-[11px] text-muted-foreground">
            <span>
              <Kbd>↑↓</Kbd> move
            </span>
            <span>
              <Kbd>↵</Kbd> open
            </span>
            <span>
              <Kbd>⌘↵</Kbd> get best release
            </span>
            <span className="ml-auto hidden sm:inline">{ms !== null && remote ? `TMDB · ${ms} ms` : ""}</span>
          </div>
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
      className="flex cursor-pointer items-center gap-3.5 rounded-lg px-2.5 py-2 outline-none data-[selected=true]:bg-accent"
    >
      {children}
    </CommandPrimitive.Item>
  );
}

function Meta({ title, sub }: { title: string; sub: (string | number | null | undefined)[] }) {
  return (
    <div className="min-w-0 flex-1">
      <div className="truncate font-semibold">{title}</div>
      <div className="truncate font-mono text-[10.5px] tracking-wider text-muted-foreground uppercase">{sub.filter(Boolean).join(" · ")}</div>
    </div>
  );
}
