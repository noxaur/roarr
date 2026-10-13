import { useMemo, useRef, useState, type ReactNode } from "react";
import { ArrowRight } from "lucide-react";
import type { LibraryItem } from "@/lib/api";
import { bytes } from "@/lib/format";
import { libraryHref } from "@/lib/rank";
import { Link } from "@/lib/router";
import { isActive, useStore } from "@/lib/store";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Empty, Label, Notice, PageHeader, SectionHead, Stat, Stats } from "@/components/kit";
import { PosterCard } from "@/components/Poster";

type Filter = "all" | "tv" | "movie" | "missing" | "unmonitored";
type Sort = "title" | "year" | "missing" | "size";

const missingOf = (it: LibraryItem) => Math.max(0, it.total - it.have);

function greeting(user?: string) {
  const h = new Date().getHours();
  const part = h < 5 ? "Up late" : h < 12 ? "Good morning" : h < 18 ? "Good afternoon" : "Good evening";
  return user ? `${part}, ${user}` : part;
}

export function Home() {
  const { user, library, libraryLoaded, libraryErrors, activity, pendingRequests } = useStore();
  const [filter, setFilterState] = useState<Filter>(() => (localStorage.getItem("roarr.filter") as Filter) || "all");
  const [sort, setSortState] = useState<Sort>(() => (localStorage.getItem("roarr.sort") as Sort) || "title");
  const shelf = useRef<HTMLDivElement>(null);
  const setFilter = (f: Filter) => {
    localStorage.setItem("roarr.filter", f);
    setFilterState(f);
  };
  const setSort = (s: Sort) => {
    localStorage.setItem("roarr.sort", s);
    setSortState(s);
  };

  const counts = useMemo(() => {
    const shows = library.filter((i) => i.kind === "tv");
    const movies = library.filter((i) => i.kind === "movie");
    const gaps = library.filter((i) => missingOf(i) > 0);
    return {
      shows: shows.length,
      movies: movies.length,
      gaps: gaps.length,
      episodes: shows.reduce((n, i) => n + missingOf(i), 0),
      missingMovies: movies.filter((i) => missingOf(i) > 0).length,
      disk: library.reduce((n, i) => n + (i.sizeOnDisk ?? 0), 0),
      unmonitored: library.filter((i) => !i.monitored).length,
    };
  }, [library]);

  const items = useMemo(() => {
    const xs = library.filter((it) =>
      filter === "all" ? true : filter === "missing" ? missingOf(it) > 0 : filter === "unmonitored" ? !it.monitored : it.kind === filter,
    );
    if (sort === "year") return [...xs].sort((a, b) => (b.year ?? 0) - (a.year ?? 0));
    if (sort === "missing") return [...xs].sort((a, b) => missingOf(b) - missingOf(a));
    if (sort === "size") return [...xs].sort((a, b) => (b.sizeOnDisk ?? 0) - (a.sizeOnDisk ?? 0));
    return xs;
  }, [library, filter, sort]);

  const moving = activity.filter(isActive);
  const [diskValue, diskUnit] = bytes(counts.disk).split(" ");

  return (
    <div className="space-y-10">
      <PageHeader label={greeting(user)} title="Library">
        {libraryLoaded && (
          <Stats>
            <Stat value={counts.shows} label="Shows" />
            <Stat value={counts.movies} label="Movies" />
            {counts.disk > 0 && <Stat value={diskValue} unit={diskUnit} label="On disk" />}
            <Stat value={counts.gaps} label="Have gaps" />
          </Stats>
        )}
      </PageHeader>

      {Object.entries(libraryErrors).map(([svc, err]) => (
        <Notice key={svc}>
          <b className="capitalize">{svc}</b>: {err}
        </Notice>
      ))}

      <div className={cn("grid gap-3.5", pendingRequests !== null ? "md:grid-cols-[1.3fr_1fr_1fr]" : "md:grid-cols-2")}>
        <Tile tone="ice" index={1} title="Downloading" to="/activity">
          <div className="mt-auto flex items-end gap-5">
            <Big>{moving.length}</Big>
            <div className="min-w-0 flex-1 space-y-1.5 pb-1.5">
              {moving.length === 0 && <p className="text-sm">Nothing in flight right now.</p>}
              {moving.slice(0, 3).map((a) => (
                <div key={a.id}>
                  <div className="flex justify-between gap-3 text-[13px]">
                    <span className="truncate">
                      {a.title}
                      {a.subtitle && ` · ${a.subtitle}`}
                    </span>
                    <b className="font-mono text-xs font-medium">{Math.floor(a.progress * 100)}%</b>
                  </div>
                  <div className="mt-1 h-1 overflow-hidden rounded-full bg-ink/12">
                    <i className="block h-full rounded-full bg-ink" style={{ width: `${a.progress * 100}%` }} />
                  </div>
                </div>
              ))}
            </div>
          </div>
        </Tile>
        {pendingRequests !== null && (
          <Tile tone="pink" index={2} title="Requests" to="/requests">
            <Big className="my-auto">{pendingRequests}</Big>
            <p className="mb-3 text-sm">{pendingRequests === 0 ? "No one is waiting on you." : `${pendingRequests === 1 ? "request is" : "requests are"} waiting for a yes or no.`}</p>
            <ChipLink>{pendingRequests ? "Review requests" : "See all requests"}</ChipLink>
          </Tile>
        )}
        <Tile
          tone="lime"
          index={pendingRequests !== null ? 3 : 2}
          title="Gaps"
          onClick={() => {
            setFilter("missing");
            shelf.current?.scrollIntoView({ behavior: "smooth", block: "start" });
          }}
        >
          <Big className="my-auto">{counts.gaps}</Big>
          <p className="mb-3 text-sm">
            {counts.gaps === 0
              ? "Everything you track is on disk."
              : `titles are missing something: ${[counts.episodes && `${counts.episodes} episode${counts.episodes > 1 ? "s" : ""}`, counts.missingMovies && `${counts.missingMovies} movie${counts.missingMovies > 1 ? "s" : ""}`].filter(Boolean).join(" and ")}.`}
          </p>
          <ChipLink>Show what's missing</ChipLink>
        </Tile>
      </div>

      <section ref={shelf} className="scroll-mt-24 space-y-6 border-t border-dashed border-dash pt-6">
        <SectionHead index={pendingRequests !== null ? 4 : 3} title="Everything">
          <Tabs value={filter} onValueChange={(v) => setFilter(v as Filter)}>
            <TabsList className="max-w-full overflow-x-auto">
              {(
                [
                  ["all", "All", library.length],
                  ["tv", "Shows", counts.shows],
                  ["movie", "Movies", counts.movies],
                  ["missing", "Missing", counts.gaps],
                  ...(counts.unmonitored > 0 ? ([["unmonitored", "Not monitored", counts.unmonitored]] as const) : []),
                ] as const
              ).map(([v, name, n]) => (
                <TabsTrigger key={v} value={v} className="flex-none">
                  {name} <span className="font-mono text-[11px] text-muted-foreground">{n}</span>
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
          <div className="ml-auto">
            <Select value={sort} onValueChange={(v) => setSort(v as Sort)}>
              <SelectTrigger className="h-9 rounded-full px-4" aria-label="Sort">
                <SelectValue />
              </SelectTrigger>
              <SelectContent align="end">
                <SelectItem value="title">Title A–Z</SelectItem>
                <SelectItem value="year">Newest first</SelectItem>
                <SelectItem value="missing">Most missing</SelectItem>
                <SelectItem value="size">Largest on disk</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </SectionHead>

        {!libraryLoaded ? (
          <Grid>
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
                Your library is empty. Press <b>⌘K</b> to find something.
              </>
            ) : (
              "Nothing matches this filter."
            )}
          </Empty>
        ) : (
          <Grid>
            {items.map((it) => {
              const miss = missingOf(it);
              return (
                <PosterCard
                  key={it.key}
                  to={libraryHref(it)}
                  poster={it.poster}
                  title={it.title}
                  year={it.year}
                  sub={[it.year, it.kind === "tv" ? `${it.have}/${it.total} eps` : it.have ? "Movie" : null].filter(Boolean).join(" · ")}
                  corner={miss > 0 ? <Badge variant="pink">{it.kind === "movie" ? "missing" : `${miss} missing`}</Badge> : null}
                />
              );
            })}
          </Grid>
        )}
      </section>
    </div>
  );
}

function Grid({ children }: { children: ReactNode }) {
  return <div className="grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] gap-x-4 gap-y-6 sm:grid-cols-[repeat(auto-fill,minmax(168px,1fr))] sm:gap-x-5">{children}</div>;
}

function Big({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("font-display text-[4rem] leading-none tracking-[-0.05em]", className)}>{children}</div>;
}

function ChipLink({ children }: { children: ReactNode }) {
  return (
    <span className="inline-flex h-8 w-fit items-center gap-1.5 rounded-full bg-ink px-3.5 text-[13px] font-medium text-paper">
      {children}
    </span>
  );
}

function Tile({
  tone,
  index,
  title,
  to,
  onClick,
  children,
}: {
  tone: "ice" | "pink" | "lime";
  index: number;
  title: string;
  to?: string;
  onClick?: () => void;
  children: ReactNode;
}) {
  const cls = cn(
    "group flex min-h-[200px] flex-col rounded-2xl p-5 text-left text-ink outline-none transition-transform hover:-translate-y-0.5 focus-visible:ring-[3px] focus-visible:ring-ring/50",
    tone === "ice" && "bg-ice",
    tone === "pink" && "bg-pink",
    tone === "lime" && "bg-lime",
  );
  const head = (
    <div className="flex items-center justify-between">
      <Label className="text-ink/60">
        {String(index).padStart(2, "0")} — {title}
      </Label>
      <ArrowRight className="size-4 transition-transform group-hover:translate-x-0.5" />
    </div>
  );
  if (to)
    return (
      <Link to={to} className={cls}>
        {head}
        {children}
      </Link>
    );
  return (
    <button onClick={onClick} className={cn(cls, "cursor-pointer")}>
      {head}
      {children}
    </button>
  );
}

