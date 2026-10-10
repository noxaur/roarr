import { useEffect, useState } from "react";
import { Download } from "lucide-react";
import { toast } from "sonner";
import { api, type RequestItem } from "@/lib/api";
import { autoGet, errText } from "@/lib/actions";
import { ago } from "@/lib/format";
import { Link } from "@/lib/router";
import { useStore } from "@/lib/store";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Empty, Notice, PageHeader, Spinner } from "@/components/kit";
import { Thumb } from "@/components/Poster";

const FILTERS = [
  ["pending", "Waiting"],
  ["approved", "Approved"],
  ["all", "All"],
] as const;
type Filter = (typeof FILTERS)[number][0];

function href(r: RequestItem) {
  if (r.inLibrary && r.libraryId) return r.kind === "tv" ? `/tv/${r.libraryId}` : `/movie/${r.libraryId}`;
  return r.kind === "tv" ? `/tv/tvdb/${r.tvdbId ?? 0}?tmdb=${r.tmdbId}` : `/movie/tmdb/${r.tmdbId}`;
}

export function RequestsPage() {
  const { pendingRequests, reloadRequests, reloadActivity, reloadLibrary } = useStore();
  const [filter, setFilter] = useState<Filter>("pending");
  const [items, setItems] = useState<RequestItem[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<Record<number, string>>({});
  // When Seerr auto-fetch is on, approving already triggers a grab.
  const [autoFetch, setAutoFetch] = useState(false);

  const load = () =>
    api
      .requests(filter)
      .then((r) => {
        setItems(r.items);
        setError(null);
      })
      .catch((e) => setError(String(e.message ?? e)));
  useEffect(() => {
    setItems(null);
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filter]);
  useEffect(() => {
    api
      .settings()
      .then((s) => setAutoFetch(s.settings.behaviour.seerrAutoFetch))
      .catch(() => {});
  }, []);

  const act = async (r: RequestItem, action: "approve" | "decline" | "get") => {
    setBusy((b) => ({ ...b, [r.id]: action }));
    try {
      await api.requestAction(r.id, action === "decline" ? "decline" : "approve");
      if (action === "get") {
        const res = await autoGet(
          r.kind === "movie"
            ? { kind: "movie", movies: [r.tmdbId] }
            : { kind: "tv", seriesId: r.inLibrary ? r.libraryId : 0, tvdbId: r.tvdbId, tmdbId: r.tmdbId, seasons: r.seasons },
          r.title,
        );
        if (res) {
          reloadActivity();
          setTimeout(() => reloadLibrary(), 1200);
        }
      } else toast.success(`${action === "approve" ? "Approved" : "Declined"} ${r.title}`);
      load();
      reloadRequests();
    } catch (e) {
      toast.error(errText(e));
    } finally {
      setBusy((b) => {
        const n = { ...b };
        delete n[r.id];
        return n;
      });
    }
  };

  const notConfigured = error?.includes("not configured");

  return (
    <div className="space-y-10">
      <PageHeader label="From Seerr" title="Requests">
        <Tabs value={filter} onValueChange={(v) => setFilter(v as Filter)}>
          <TabsList>
            {FILTERS.map(([v, name]) => (
              <TabsTrigger key={v} value={v} className="flex-none">
                {name}
                {v === "pending" && pendingRequests ? <span className="font-mono text-[11px] text-muted-foreground">{pendingRequests}</span> : null}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </PageHeader>

      {error && <Notice>{notConfigured ? "Seerr is not set up. Add its URL and API key under Settings → Connections to see requests here." : error}</Notice>}

      {!items && !error && (
        <div className="space-y-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-20 rounded-xl" />
          ))}
        </div>
      )}
      {items && items.length === 0 && <Empty>{filter === "pending" ? "No one is waiting on you." : "No requests here."}</Empty>}

      {items && items.length > 0 && (
        <section>
          {items.map((r) => {
            const pending = r.status === "pending";
            const canGet = r.kind === "movie" || !!r.tvdbId || !!r.libraryId;
            const b = busy[r.id];
            return (
              <div key={r.id} className="grid grid-cols-[48px_minmax(0,1fr)] items-center gap-x-4 gap-y-3 border-t border-dashed border-dash py-4 sm:grid-cols-[54px_minmax(0,1fr)_auto] sm:gap-x-5">
                <Thumb src={r.poster} title={r.title} className="w-12 sm:w-[54px]" />
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1">
                    <Link to={href(r)} className="text-[17px] font-semibold tracking-tight hover:underline hover:decoration-dash hover:underline-offset-4">
                      {r.title}
                    </Link>
                    <span className="font-mono text-[11px] tracking-wide text-muted-foreground uppercase">
                      {[r.kind === "tv" ? "Show" : "Movie", r.year, r.seasons?.length ? `Season ${r.seasons.join(", ")}` : null].filter(Boolean).join(" · ")}
                    </span>
                    {!pending && <Badge variant={r.status === "declined" ? "destructive" : "ok"}>{r.status}</Badge>}
                    {r.available === "available" && <Badge variant="ok">Available</Badge>}
                    {r.available === "partial" && <Badge variant="ice">Partly available</Badge>}
                  </div>
                  <div className="mt-1 font-mono text-[11px] tracking-wide text-muted-foreground uppercase">
                    Asked by {r.requestedBy} · {ago(r.createdAt)}
                  </div>
                </div>
                <div className="col-span-2 flex flex-wrap justify-end gap-2 sm:col-span-1">
                  {pending ? (
                    <>
                      <Button variant="ghost" size="sm" disabled={!!b} onClick={() => act(r, "decline")}>
                        {b === "decline" && <Spinner />} Decline
                      </Button>
                      {autoFetch || !canGet ? (
                        <Button size="sm" disabled={!!b} onClick={() => act(r, "approve")}>
                          {b === "approve" && <Spinner />} Approve{autoFetch ? " & get" : ""}
                        </Button>
                      ) : (
                        <>
                          <Button variant="outline" size="sm" disabled={!!b} onClick={() => act(r, "approve")}>
                            {b === "approve" && <Spinner />} Approve only
                          </Button>
                          <Button size="sm" disabled={!!b} onClick={() => act(r, "get")}>
                            {b === "get" ? <Spinner /> : <Download />} Approve &amp; get
                          </Button>
                        </>
                      )}
                    </>
                  ) : (
                    <Button asChild variant="outline" size="sm">
                      <Link to={href(r)}>Open</Link>
                    </Button>
                  )}
                </div>
              </div>
            );
          })}
        </section>
      )}
    </div>
  );
}
