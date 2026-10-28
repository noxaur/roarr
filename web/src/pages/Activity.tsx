import { useEffect, useState } from "react";
import { Ban, ExternalLink, ListChecks, MoreHorizontal, RefreshCw, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, type ActivityItem, type VerifyRecord, type WatchJob } from "@/lib/api";
import { autoGet, errText } from "@/lib/actions";
import { bytes, epCode, pct, speed } from "@/lib/format";
import { Link, useRouter } from "@/lib/router";
import { needsYou, useStore } from "@/lib/store";
import { cn } from "@/lib/utils";
import { byHash, forActivity, headline, hrefOf, isBlocking, isFlagged, orphans } from "@/lib/verify";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { useConfirm } from "@/components/Confirm";
import { Bar, Dot, Empty, Figure, Notice, PageHeader, SectionHead, type Tone } from "@/components/kit";
import { Thumb } from "@/components/Poster";
import { VerdictActions, VerdictBadge, VerdictChecks } from "@/components/Verdict";

const withPick = (href: string) => `${href}${href.includes("?") ? "&" : "?"}pick=1`;

/**
 * Everything in flight, with the ones that need a decision first. For a
 * stuck or failed download the choices are: pick another release, let roarr
 * pick again, remove it, or remove it and blocklist that release.
 */
export function ActivityPage() {
  const { activity, reloadActivity, reloadLibrary, verify, library } = useStore();
  const [errors, setErrors] = useState<Record<string, string>>({});
  const confirm = useConfirm();

  // Faster refresh while this page is open.
  useEffect(() => {
    const tick = () => {
      reloadActivity();
      api
        .activity()
        .then((r) => setErrors(r.errors))
        .catch(() => {});
    };
    tick();
    const t = setInterval(tick, 2000);
    return () => clearInterval(t);
  }, [reloadActivity]);

  const remove = async (item: ActivityItem, blocklist: boolean) => {
    const a = await confirm({
      title: blocklist ? `Remove and blocklist ${item.title}?` : `Remove ${item.title}?`,
      body: (
        <>
          <p>{blocklist ? "The download is removed from the client and this release is blocklisted, so it is never picked again." : "The download is removed from the client."}</p>
          <p className="font-mono text-xs break-all text-foreground/70">{item.release}</p>
        </>
      ),
      confirm: blocklist ? "Remove and blocklist" : "Remove",
      danger: true,
    });
    if (!a) return;
    try {
      await api.removeActivity(item.id, blocklist);
      toast.success(blocklist ? `Removed and blocklisted ${item.title}` : `Removed ${item.title}`);
      reloadActivity();
    } catch (e) {
      toast.error(errText(e));
    }
  };

  // Blocklist the stuck release, then let roarr pick again for the same title.
  const retry = async (item: ActivityItem) => {
    const a = await confirm({
      title: `Try another release for ${item.title}?`,
      body: <p>Removes and blocklists this download, then roarr picks the next best release.</p>,
      confirm: "Remove and pick again",
      danger: true,
    });
    if (!a) return;
    try {
      await api.removeActivity(item.id, true);
    } catch (e) {
      toast.error(errText(e));
      return;
    }
    const m = item.href?.match(/^\/(tv|movie)\/(\d+)/);
    if (!m) {
      reloadActivity();
      return;
    }
    const lib = await api.library().catch(() => null);
    const it = lib?.items.find((x) => x.kind === m[1] && x.id === +m[2]);
    const res = m[1] === "tv" ? await autoGet({ kind: "tv", seriesId: +m[2] }, item.title) : it?.tmdbId ? await autoGet({ kind: "movie", movies: [it.tmdbId] }, item.title) : null;
    reloadActivity();
    if (res) setTimeout(() => reloadLibrary(), 1500);
  };

  const checks = byHash(verify);
  const recOf = (a: ActivityItem) => forActivity(checks, a);
  const stuck = (a: ActivityItem) => {
    const r = recOf(a);
    return needsYou(a) || (!!r && isBlocking(r));
  };
  const attention = activity.filter(stuck);
  const rest = activity.filter((a) => !stuck(a));
  // Failed checks whose download already left the queue.
  const done = orphans(verify, activity);
  const posterOf = (r: VerifyRecord) => library.find((x) => x.kind === r.kind && x.id === (r.kind === "tv" ? r.seriesId : r.movieId))?.poster;
  const total = activity.reduce((n, a) => n + (a.speed ?? 0), 0);
  const needs = attention.length + done.length;

  return (
    <div className="space-y-8">
      <PageHeader title="Downloads" sub="Refreshes every 2 s">
        <div className="flex flex-wrap gap-x-5 gap-y-1">
          <Figure value={rest.length} label="in flight" tone={rest.length ? "live" : undefined} />
          <Figure value={needs} label="need you" tone={needs ? "bad" : undefined} />
          <Figure value={total ? speed(total) : "0"} label="" />
        </div>
      </PageHeader>

      {Object.entries(errors).map(([k, v]) => (
        <Notice key={k} tone="bad">
          <b className="capitalize">{k}</b>: {v}
        </Notice>
      ))}

      {activity.length === 0 && done.length === 0 && <Empty>Nothing downloading.</Empty>}

      {needs > 0 && (
        <section>
          <SectionHead title="Needs you" className="mb-2" />
          {attention.map((a) => (
            <Row key={a.id} a={a} rec={recOf(a)} attention onRemove={(b) => remove(a, b)} onRetry={() => retry(a)} />
          ))}
          {done.map((r) => (
            <CheckRow key={r.hash} r={r} poster={posterOf(r)} />
          ))}
        </section>
      )}

      {rest.length > 0 && (
        <section>
          <SectionHead title="In flight" className="mb-2" />
          {rest.map((a) => (
            <Row key={a.id} a={a} rec={recOf(a)} onRemove={(b) => remove(a, b)} onRetry={() => retry(a)} />
          ))}
        </section>
      )}
    </div>
  );
}

function toneOf(s: string): Tone {
  if (s === "warning" || s === "failed") return "bad";
  if (s === "downloading" || s === "importing") return "live";
  return "none";
}
const statusText = (s: string) => (s === "warning" ? "Stuck" : s === "failed" ? "Failed" : s === "importing" ? "Importing" : s === "queued" ? "Queued" : "");

function Row({ a, rec, attention, onRemove, onRetry }: { a: ActivityItem; rec?: VerifyRecord; attention?: boolean; onRemove: (blocklist: boolean) => void; onRetry: () => void }) {
  const { navigate } = useRouter();
  // A failed check decides the row: its reasons and actions replace the
  // generic stuck-download ones.
  const failed = !!rec && isBlocking(rec);
  const title = a.href ? (
    <Link to={a.href} className="rounded-sm outline-none hover:underline focus-visible:ring-[3px] focus-visible:ring-ring/50">
      {a.title}
    </Link>
  ) : (
    a.title
  );
  const menu = (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="icon-sm" aria-label={`More for ${a.title}`}>
          <MoreHorizontal />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-60">
        {a.href && (
          <DropdownMenuItem onSelect={() => navigate(a.href!)}>
            <ExternalLink /> Open title
          </DropdownMenuItem>
        )}
        {a.href && (
          <DropdownMenuItem onSelect={() => navigate(withPick(a.href!))}>
            <ListChecks /> Choose another release
          </DropdownMenuItem>
        )}
        <DropdownMenuItem onSelect={onRetry}>
          <RefreshCw /> Blocklist and pick again
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={() => onRemove(false)}>
          <Trash2 /> Remove
        </DropdownMenuItem>
        <DropdownMenuItem variant="destructive" onSelect={() => onRemove(true)}>
          <Ban /> Remove and blocklist
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );

  return (
    <div
      className={cn(
        "grid grid-cols-[40px_minmax(0,1fr)_auto] items-center gap-x-3.5 gap-y-2.5 border-t py-3 md:grid-cols-[44px_minmax(0,1fr)_minmax(200px,280px)_auto] md:gap-x-5",
        attention && "border-l-2 border-l-bad pl-3",
      )}
    >
      <Thumb src={a.poster} title={a.title} className="w-10 md:w-11" />
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1">
          <span className="text-[15px] font-medium">{title}</span>
          {a.subtitle && <span className="font-mono text-[12px] text-muted-foreground">{a.subtitle}</span>}
          {statusText(a.status) && (
            <span className={cn("inline-flex items-center gap-1.5 text-[12.5px]", toneOf(a.status) === "bad" ? "text-bad" : "text-muted-foreground")}>
              <Dot tone={toneOf(a.status)} />
              {statusText(a.status)}
            </span>
          )}
          {a.watch && <span className="meta">imports as episodes finish</span>}
          {rec && isFlagged(rec) && !failed && <VerdictBadge r={rec} />}
        </div>
        <div className="mt-0.5 truncate font-mono text-[11.5px] text-muted-foreground" title={a.release}>
          {a.release}
        </div>
        {failed ? (
          <VerdictChecks r={rec!} className="mt-2" />
        ) : (
          a.messages?.map((m, i) => (
            <div key={i} className={cn("mt-1 text-[13px]", attention ? "text-bad" : "text-foreground/80")}>
              {m}
            </div>
          ))
        )}
        {a.watch && <WatchStrip w={a.watch} />}
      </div>
      {failed ? (
        <div className="col-span-3 flex flex-wrap items-center justify-end gap-2 md:col-span-2">
          <VerdictActions r={rec!} className="justify-end" />
          {menu}
        </div>
      ) : attention ? (
        <div className="col-span-3 flex flex-wrap items-center justify-end gap-2 md:col-span-2">
          {a.href && (
            <Button size="sm" onClick={() => navigate(withPick(a.href!))}>
              <ListChecks /> Choose another release
            </Button>
          )}
          <Button size="sm" variant="outline" onClick={onRetry}>
            <RefreshCw /> Pick again
          </Button>
          <Button size="sm" variant="outline" onClick={() => onRemove(false)}>
            <Trash2 /> Remove
          </Button>
          {menu}
        </div>
      ) : (
        <>
          <div className="col-span-3 md:col-span-1">
            <div className="flex justify-between font-mono text-[11.5px] tabular">
              <span>{a.size > 0 ? `${bytes(a.size - a.sizeLeft)} / ${bytes(a.size)}` : "size unknown yet"}</span>
              <span>{pct(a.progress)}</span>
            </div>
            <Bar value={a.progress} className="my-1.5" tone={a.status === "importing" ? "have" : "live"} />
            <div className="meta font-mono tabular">
              {[a.speed ? speed(a.speed) : a.status === "queued" ? "waiting" : null, a.eta && a.status === "downloading" ? `${a.eta} left` : null].filter(Boolean).join(", ") || " "}
            </div>
          </div>
          <div className="col-start-3 row-start-1 md:col-start-4">{menu}</div>
        </>
      )}
    </div>
  );
}

/** A finished download that failed its check: same row shape, verdict actions. */
function CheckRow({ r, poster }: { r: VerifyRecord; poster?: string }) {
  const href = hrefOf(r);
  return (
    <div className="grid grid-cols-[40px_minmax(0,1fr)] items-center gap-x-3.5 gap-y-2.5 border-t border-l-2 border-l-bad py-3 pl-3 md:grid-cols-[44px_minmax(0,1fr)_auto] md:gap-x-5">
      <Thumb src={poster} title={r.title} className="w-10 md:w-11" />
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1">
          <span className="text-[15px] font-medium">
            {href ? (
              <Link to={href} className="rounded-sm outline-none hover:underline focus-visible:ring-[3px] focus-visible:ring-ring/50">
                {r.title}
              </Link>
            ) : (
              r.title
            )}
          </span>
          <span className="inline-flex items-center gap-1.5 text-[12.5px] text-bad" title={headline(r)}>
            <Dot tone="bad" />
            {r.rejected ? "Re-grab failed" : "Failed check"}
          </span>
        </div>
        <div className="mt-0.5 truncate font-mono text-[11.5px] text-muted-foreground" title={r.release}>
          {r.release}
        </div>
        <VerdictChecks r={r} className="mt-2" />
      </div>
      <VerdictActions r={r} className="col-span-2 justify-end md:col-span-1" />
    </div>
  );
}

function WatchStrip({ w }: { w: WatchJob }) {
  const files = w.files ?? [];
  const focus = files.find((f) => !f.skipped && f.progress < 1);
  const imported = files.filter((f) => f.imported).length;
  return (
    <div className="mt-2">
      <div className="flex flex-wrap gap-1">
        {files.map((f) => {
          const isFocus = focus?.index === f.index;
          return (
            <span
              key={f.index}
              title={f.error ?? f.name}
              className={cn(
                "relative grid h-5 min-w-[30px] place-items-center overflow-hidden rounded-[4px] border px-1 font-mono text-[10px] text-muted-foreground",
                f.imported && "border-transparent bg-have text-background",
                isFocus && "border-live text-foreground",
                f.skipped && "line-through opacity-50",
                f.error && "border-bad text-bad",
              )}
            >
              {!f.imported && f.progress > 0 && f.progress < 1 && <i className="absolute inset-y-0 left-0 bg-live-soft" style={{ width: pct(f.progress) }} />}
              <span className="relative">{epCode(f.season, f.episode).replace(/^S\d+/, "")}</span>
            </span>
          );
        })}
      </div>
      <div className="meta mt-1 font-mono">
        {files.length === 0 ? "waiting for the file list" : imported > 0 ? `${imported} imported` : "first episode first"}
        {w.message ? `, ${w.message}` : ""}
      </div>
    </div>
  );
}
