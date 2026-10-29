import { Ban, Copy, ExternalLink, ListChecks, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, type ActivityItem, type VerifyRecord } from "@/lib/api";
import { errText } from "@/lib/actions";
import { bytes, pct, speed } from "@/lib/format";
import { Link, useRouter } from "@/lib/router";
import { needsYou, useStore } from "@/lib/store";
import { cn } from "@/lib/utils";
import { byHash, forActivity, headline, hrefOf, isBlocking, orphans } from "@/lib/verify";
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuLabel, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Bar } from "./kit";
import { useConfirm } from "./Confirm";
import { VerdictActions } from "./Verdict";

/** Live download speed of everything in flight, for the nav and drawer. */
export function totalSpeed(activity: ActivityItem[]): number {
  return activity.reduce((n, a) => n + (a.speed ?? 0), 0);
}

const withPick = (href: string) => `${href}${href.includes("?") ? "&" : "?"}pick=1`;

/** The quick downloads panel (D). Right-click a download for more. */
export function DownloadsDrawer({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const { activity, reloadActivity, verify } = useStore();
  const { navigate } = useRouter();
  const confirm = useConfirm();
  const total = totalSpeed(activity);
  const checks = byHash(verify);
  const failedCheck = (a: ActivityItem) => {
    const r = forActivity(checks, a);
    return r && isBlocking(r) ? r : undefined;
  };
  const stuck = (a: ActivityItem) => needsYou(a) || !!failedCheck(a);
  const attention = activity.filter(stuck);
  const rest = activity.filter((a) => !stuck(a));
  const done = orphans(verify, activity);

  const remove = async (a: ActivityItem, blocklist: boolean) => {
    const ok = await confirm({
      title: blocklist ? `Remove and blocklist ${a.title}?` : `Remove ${a.title}?`,
      body: <p className="font-mono text-xs break-all">{a.release}</p>,
      confirm: blocklist ? "Remove and blocklist" : "Remove",
      danger: true,
    });
    if (!ok) return;
    try {
      await api.removeActivity(a.id, blocklist);
      toast.success(blocklist ? `Removed and blocklisted ${a.title}` : `Removed ${a.title}`);
      reloadActivity();
    } catch (e) {
      toast.error(errText(e));
    }
  };

  const go = (href: string) => {
    onOpenChange(false);
    navigate(href);
  };

  const checkCard = (r: VerifyRecord) => {
    const href = hrefOf(r);
    return (
      <div key={r.hash} className="rounded-[2px] bg-card p-3">
        <div className="flex items-baseline gap-2">
          <span className="min-w-0 flex-1 truncate text-[13px] font-medium">
            {href ? (
              <button onClick={() => go(href)} className="cursor-pointer hover:underline">
                {r.title}
              </button>
            ) : (
              r.title
            )}
          </span>
          <span className="font-mono text-[11px] text-bad">{r.rejected ? "re-grab failed" : "failed check"}</span>
        </div>
        <div className="mt-2 text-[12px] leading-snug text-bad">{headline(r)}</div>
        <VerdictActions r={r} size="xs" className="mt-3" />
      </div>
    );
  };

  const row = (a: ActivityItem) => {
    const rec = failedCheck(a);
    const bad = needsYou(a) || !!rec;
    return (
      <ContextMenu key={a.id}>
        <ContextMenuTrigger asChild>
          <div className="rounded-[2px] bg-card p-3">
            <div className="flex items-baseline gap-2">
              <span className="min-w-0 flex-1 truncate text-[13px] font-medium">
                {a.title}
                {a.subtitle && <span className="font-mono text-[12px] font-normal text-muted-foreground"> {a.subtitle}</span>}
              </span>
              <span className={cn("font-mono text-[11px]", bad ? "text-bad" : a.status === "downloading" ? "text-live" : "text-muted-foreground")}>
                {rec ? "failed check" : a.status === "warning" ? "stuck" : a.status}
              </span>
            </div>
            <Bar className="mt-3" value={a.progress} segments={24} stretch tone={bad ? "bad" : "live"} />
            <div className="meta mt-2 flex justify-between font-mono tabular">
              <span>
                {pct(a.progress)} of {bytes(a.size)}
              </span>
              <span className="truncate pl-3">{rec ? "" : bad ? (a.messages?.[0] ?? "") : [speed(a.speed), a.eta].filter(Boolean).join(", ")}</span>
            </div>
            {rec && (
              <>
                <div className="mt-2 text-[12px] leading-snug text-bad">{headline(rec)}</div>
                <VerdictActions r={rec} size="xs" className="mt-3" />
              </>
            )}
            {bad && !rec && a.href && (
              <button onClick={() => go(withPick(a.href!))} className="mt-2 inline-flex cursor-pointer items-center gap-2 text-[12px] font-medium underline-offset-4 hover:underline">
                Pick release manually
              </button>
            )}
          </div>
        </ContextMenuTrigger>
        <ContextMenuContent className="w-60">
          <ContextMenuLabel className="truncate text-xs font-normal text-muted-foreground">{a.title}</ContextMenuLabel>
          {a.href && (
            <ContextMenuItem onSelect={() => go(a.href!)}>
              <ExternalLink /> Open title
            </ContextMenuItem>
          )}
          {a.href && (
            <ContextMenuItem onSelect={() => go(withPick(a.href!))}>
              <ListChecks /> Pick release manually
            </ContextMenuItem>
          )}
          <ContextMenuItem onSelect={() => navigator.clipboard?.writeText(a.release).then(() => toast("Copied release name"), () => {})}>
            <Copy /> Copy release name
          </ContextMenuItem>
          <ContextMenuSeparator />
          <ContextMenuItem onSelect={() => remove(a, false)}>
            <Trash2 /> Remove
          </ContextMenuItem>
          <ContextMenuItem variant="destructive" onSelect={() => remove(a, true)}>
            <Ban /> Remove and blocklist
          </ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>
    );
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" data-downloads className="w-full gap-0 p-0 sm:max-w-[400px]">
        <SheetHeader className="border-b px-4 py-4">
          <SheetTitle className="text-[14px] font-medium">Downloads</SheetTitle>
          <SheetDescription className="font-mono text-[12px]">{total ? speed(total) : "idle"}</SheetDescription>
        </SheetHeader>
        <div className="flex-1 space-y-2 overflow-y-auto p-3">
          {activity.length === 0 && done.length === 0 && <div className="px-3 py-16 text-center text-[13px] text-muted-foreground">Nothing in flight.</div>}
          {attention.length + done.length > 0 && <div className="label-caps px-1 pt-1">Needs you</div>}
          {attention.map(row)}
          {done.map(checkCard)}
          {attention.length + done.length > 0 && rest.length > 0 && <div className="label-caps px-1 pt-3">In flight</div>}
          {rest.map(row)}
        </div>
        <div className="flex items-center justify-end border-t px-4 py-3 text-[12px] text-muted-foreground">
          <Link to="/downloads" onClick={() => onOpenChange(false)} className="font-medium text-foreground hover:underline">
            All downloads
          </Link>
        </div>
      </SheetContent>
    </Sheet>
  );
}
