import { useEffect, useState } from "react";
import { Ban, ExternalLink, MoreHorizontal, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, type ActivityItem, type WatchJob } from "@/lib/api";
import { errText } from "@/lib/actions";
import { bytes, epCode, pct, speed } from "@/lib/format";
import { Link, useRouter } from "@/lib/router";
import { needsYou, useStore } from "@/lib/store";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Bar, Empty, Notice, PageHeader, SectionHead, Stat, Stats } from "@/components/kit";
import { Thumb } from "@/components/Poster";

const withPick = (href: string) => `${href}${href.includes("?") ? "&" : "?"}pick=1`;

export function ActivityPage() {
  const { activity, reloadActivity } = useStore();
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [removing, setRemoving] = useState<{ item: ActivityItem; blocklist: boolean } | null>(null);

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

  const remove = async () => {
    if (!removing) return;
    const { item, blocklist } = removing;
    setRemoving(null);
    try {
      await api.removeActivity(item.id, blocklist);
      toast.success(blocklist ? `Removed and blocklisted ${item.title}` : `Removed ${item.title}`);
      reloadActivity();
    } catch (e) {
      toast.error(errText(e));
    }
  };

  const attention = activity.filter(needsYou);
  const rest = activity.filter((a) => !needsYou(a));
  const total = activity.reduce((n, a) => n + (a.speed ?? 0), 0);
  const [rate, unit] = total ? speed(total).split(" ") : ["0", "MB/s"];

  return (
    <div className="space-y-10">
      <PageHeader label="Live · refreshes every 2 s" title="Activity">
        <Stats>
          <Stat value={rest.length} label="Downloading" />
          <Stat value={attention.length} label="Needs you" />
          <Stat value={rate} unit={unit} label="Combined" />
        </Stats>
      </PageHeader>

      {Object.entries(errors).map(([k, v]) => (
        <Notice key={k}>
          <b className="capitalize">{k}</b>: {v}
        </Notice>
      ))}

      {activity.length === 0 && <Empty>Nothing downloading. Find something with ⌘K.</Empty>}

      {attention.length > 0 && (
        <section className="space-y-3">
          <SectionHead index={1} title="Needs you" />
          {attention.map((a) => (
            <Row key={a.id} a={a} attention onRemove={(blocklist) => setRemoving({ item: a, blocklist })} />
          ))}
        </section>
      )}

      {rest.length > 0 && (
        <section>
          <SectionHead index={attention.length ? 2 : 1} title="Downloading" className="mb-3" />
          {rest.map((a) => (
            <Row key={a.id} a={a} onRemove={(blocklist) => setRemoving({ item: a, blocklist })} />
          ))}
        </section>
      )}

      <AlertDialog open={!!removing} onOpenChange={(o) => !o && setRemoving(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle className="text-xl font-semibold tracking-tight">
              {removing?.blocklist ? "Remove and never grab again?" : "Remove this download?"}
            </AlertDialogTitle>
            <AlertDialogDescription asChild>
              <div className="space-y-2">
                <p>
                  {removing?.blocklist
                    ? "The download is deleted from the client and this exact release is blocklisted, so roarr and Sonarr/Radarr will pick a different one next time."
                    : "The download is deleted from the client. You can grab it, or something else, again at any time."}
                </p>
                <p className="font-mono text-xs break-all text-foreground/70">{removing?.item.release}</p>
              </div>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Keep it</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={remove}>
              {removing?.blocklist ? "Remove & blocklist" : "Remove"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function StatusBadge({ s }: { s: string }) {
  if (s === "queued") return <Badge>Queued</Badge>;
  if (s === "importing") return <Badge variant="ice">Importing</Badge>;
  if (s === "warning") return <Badge variant="destructive">Stuck</Badge>;
  if (s === "failed") return <Badge variant="destructive">Failed</Badge>;
  return null;
}

function Row({ a, attention, onRemove }: { a: ActivityItem; attention?: boolean; onRemove: (blocklist: boolean) => void }) {
  const { navigate } = useRouter();
  const title = a.href ? (
    <Link to={a.href} className="hover:underline hover:decoration-dash hover:underline-offset-4">
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
      <DropdownMenuContent align="end" className="w-64">
        {a.href && (
          <DropdownMenuItem onSelect={() => navigate(a.href!)}>
            <ExternalLink /> Open {a.title}
          </DropdownMenuItem>
        )}
        <DropdownMenuItem onSelect={() => onRemove(false)}>
          <Trash2 /> Remove download
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onSelect={() => onRemove(true)} className="items-start">
          <Ban className="mt-0.5" />
          <span>
            Remove and never grab again
            <span className="block font-mono text-[10.5px] text-muted-foreground">Blocklists this release</span>
          </span>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );

  return (
    <div
      className={cn(
        "grid grid-cols-[48px_minmax(0,1fr)_auto] items-center gap-x-4 gap-y-3 md:grid-cols-[54px_minmax(0,1fr)_minmax(220px,300px)_auto] md:gap-x-5",
        attention ? "rounded-2xl bg-pink/40 p-4 dark:bg-pink/10" : "border-t border-border py-4",
      )}
    >
      <Thumb src={a.poster} title={a.title} className="w-12 md:w-[54px]" />
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1">
          <span className="text-[17px] font-semibold tracking-tight">{title}</span>
          {a.subtitle && <span className="font-mono text-[11px] tracking-wide text-muted-foreground uppercase">{a.subtitle}</span>}
          <StatusBadge s={a.status} />
          {a.watch && <Badge variant="lime">Plays in order</Badge>}
        </div>
        <div className="mt-0.5 truncate font-mono text-[11.5px] text-muted-foreground" title={a.release}>
          {a.release}
        </div>
        {a.messages?.map((m, i) => (
          <div key={i} className="mt-1.5 text-[13px] text-foreground/80">
            {m}
          </div>
        ))}
        {a.watch && <WatchStrip w={a.watch} />}
      </div>
      {attention ? (
        <div className="col-span-3 flex items-center justify-end gap-2 md:col-span-2">
          {a.href && (
            <Button size="sm" onClick={() => navigate(withPick(a.href!))}>
              Pick another release
            </Button>
          )}
          {menu}
        </div>
      ) : (
        <>
          <div className="col-span-3 md:col-span-1">
            <div className="flex justify-between font-mono text-xs tabular">
              <span>{a.size > 0 ? `${bytes(a.size - a.sizeLeft)} / ${bytes(a.size)}` : "size unknown yet"}</span>
              <span>{pct(a.progress)}</span>
            </div>
            <Bar value={a.progress} className="my-1.5" tone={a.status === "importing" ? "ink" : "lime"} />
            <div className="font-mono text-[10.5px] tracking-wide text-muted-foreground uppercase">
              {[a.speed ? speed(a.speed) : a.status === "queued" ? "waiting" : null, a.eta && a.status === "downloading" ? `${a.eta} left` : null].filter(Boolean).join(" · ") || " "}
            </div>
          </div>
          <div className="col-start-3 row-start-1 md:col-start-4">{menu}</div>
        </>
      )}
    </div>
  );
}

function WatchStrip({ w }: { w: WatchJob }) {
  const files = w.files ?? [];
  const focus = files.find((f) => !f.skipped && f.progress < 1);
  const imported = files.filter((f) => f.imported).length;
  return (
    <div className="mt-2.5">
      <div className="flex flex-wrap gap-1">
        {files.map((f) => {
          const isFocus = focus?.index === f.index;
          return (
            <span
              key={f.index}
              title={f.error ?? f.name}
              className={cn(
                "relative grid h-[22px] min-w-[34px] place-items-center overflow-hidden rounded-[5px] border border-border px-1 font-mono text-[10px] text-muted-foreground",
                f.imported && "border-transparent bg-foreground text-background",
                isFocus && "border-foreground text-foreground",
                f.skipped && "line-through opacity-50",
                f.error && "border-destructive text-destructive",
              )}
            >
              {!f.imported && f.progress > 0 && f.progress < 1 && <i className="absolute inset-y-0 left-0 -z-0 bg-lime/70" style={{ width: pct(f.progress) }} />}
              <span className="relative">{epCode(f.season, f.episode).replace(/^S\d+/, "")}</span>
            </span>
          );
        })}
      </div>
      <div className="mt-1.5 font-mono text-[10.5px] text-muted-foreground">
        {files.length === 0 ? "waiting for the torrent's file list" : imported > 0 ? `${imported} ready to watch` : "first episode downloads first"}
        {w.message ? ` · ${w.message}` : ""}
      </div>
    </div>
  );
}
