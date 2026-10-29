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
import { Bar, Dot, Empty, Notice, PageHeader, SectionHead } from "@/components/kit";
import { VerdictActions, VerdictBadge, VerdictChecks } from "@/components/Verdict";

const withPick = (href: string) => `${href}${href.includes("?") ? "&" : "?"}pick=1`;

/**
 * Everything in flight, with the ones that need a decision first. For a
 * stuck or failed download the choices are: pick another release, let roarr
 * pick again, remove it, or remove it and blocklist that release.
 */
export function ActivityPage() {
  const { activity, reloadActivity, reloadLibrary, verify } = useStore();
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
  const needs = attention.length + done.length;

  return (
    <div className="space-y-8">
      <PageHeader title="Downloads" />

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
            <CheckRow key={r.hash} r={r} />
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

const statusText = (s: string) => (s === "warning" ? "Stuck" : s === "failed" ? "Failed" : s === "importing" ? "Importing" : s === "queued" ? "Queued" : "");

function Row({ a, rec, attention, onRemove, onRetry }: { a: ActivityItem; rec?: VerifyRecord; attention?: boolean; onRemove: (blocklist: boolean) => void; onRetry: () => void }) {
  const { navigate } = useRouter();
  // A failed check decides the row: its reasons and actions replace the
  // generic stuck-download ones.
  const failed = !!rec && isBlocking(rec);
  const title = a.href ? (
    <Link to={a.href} title={a.release} className="rounded-[2px] outline-none hover:underline focus-visible:ring-2 focus-visible:ring-ring/60">
      {a.title}
    </Link>
  ) : (
    a.title
  );
  const menu = (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon-sm" className="text-muted-foreground" aria-label={`More for ${a.title}`}>
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
            <ListChecks /> Pick release manually
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
        "grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-3 border-t py-4 md:grid-cols-[minmax(0,1fr)_minmax(200px,280px)_auto] md:gap-x-6",
      )}
    >
      <div className="flex min-w-0 gap-3">
        {attention && <Dot tone="bad" className="mt-2 size-3" />}
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <span className="text-[14px] font-medium">{title}</span>
            {a.subtitle && <span className="font-mono text-[12px] text-muted-foreground">{a.subtitle}</span>}
            {!attention && statusText(a.status) && <span className="text-[12px] text-muted-foreground">{statusText(a.status)}</span>}
            {rec && isFlagged(rec) && !failed && <VerdictBadge r={rec} />}
          </div>
          {failed ? (
            <VerdictChecks r={rec!} className="mt-2" />
          ) : (
            a.messages?.map((m, i) => (
              <div key={i} className="mt-1 max-w-[60ch] text-[13px] leading-5 text-muted-foreground">
                {m}
              </div>
            ))
          )}
          {a.watch && <WatchStrip w={a.watch} />}
          {(failed || attention) && (
            <div className="mt-3 flex flex-wrap items-center gap-2">
              {failed ? (
                <VerdictActions r={rec!} />
              ) : (
                <>
                  <Button size="sm" variant="outline" onClick={onRetry}>
                    Re-grab
                  </Button>
                  {a.href && (
                    <Button size="sm" variant="ghost" className="font-normal text-muted-foreground" onClick={() => navigate(withPick(a.href!))}>
                      Pick release manually
                    </Button>
                  )}
                </>
              )}
            </div>
          )}
        </div>
      </div>
      {failed || attention ? (
        <div className="col-start-2 row-start-1 self-start md:col-start-3">{menu}</div>
      ) : (
        <>
          <div className={cn("col-span-2 font-mono text-[12px] tabular md:col-span-1", a.status === "importing" ? "text-have" : "text-live")}>
            <div className="mb-2 flex justify-end gap-2">
              {[pct(a.progress), a.speed ? speed(a.speed) : a.status === "queued" ? "waiting" : null, a.eta && a.status === "downloading" ? a.eta : null, a.size > 0 ? bytes(a.size) : null]
                .filter(Boolean)
                .map((x, i) => (
                  <span key={i} className="whitespace-nowrap">
                    {i > 0 && <span className="mr-2 opacity-60">·</span>}
                    {x}
                  </span>
                ))}
            </div>
            <Bar value={a.progress} segments={32} stretch tone={a.status === "importing" ? "have" : "live"} />
          </div>
          <div className="col-start-2 row-start-1 md:col-start-3">{menu}</div>
        </>
      )}
    </div>
  );
}

/** A finished download that failed its check: same row shape, verdict actions. */
function CheckRow({ r }: { r: VerifyRecord }) {
  const href = hrefOf(r);
  return (
    <div className="flex gap-3 border-t py-4">
      <Dot tone="bad" className="mt-2 size-3" />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
          <span className="text-[14px] font-medium" title={r.release}>
            {href ? (
              <Link to={href} className="rounded-[2px] outline-none hover:underline focus-visible:ring-2 focus-visible:ring-ring/60">
                {r.title}
              </Link>
            ) : (
              r.title
            )}
          </span>
          <span className="text-[12px] text-bad" title={headline(r)}>
            {r.rejected ? "Re-grab failed" : "Failed check"}
          </span>
        </div>
        <VerdictChecks r={r} className="mt-2" />
        <VerdictActions r={r} className="mt-3" />
      </div>
    </div>
  );
}

function WatchStrip({ w }: { w: WatchJob }) {
  const files = w.files ?? [];
  const focus = files.find((f) => !f.skipped && f.progress < 1);
  const imported = files.filter((f) => f.imported).length;
  return (
    <div className="mt-2">
      <div className="flex flex-wrap gap-2">
        {files.map((f) => {
          const isFocus = focus?.index === f.index;
          return (
            <span
              key={f.index}
              title={f.error ?? f.name}
              className={cn(
                "grid h-5 min-w-8 place-items-center rounded-[2px] bg-unlit px-1 font-mono text-[10px] text-muted-foreground",
                f.imported && "bg-have text-black/80",
                isFocus && "bg-live text-black/80",
                f.skipped && "line-through opacity-50",
                f.error && "bg-bad text-black/80",
              )}
            >
              {epCode(f.season, f.episode).replace(/^S\d+/, "")}
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
