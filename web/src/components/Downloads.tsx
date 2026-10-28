import { Ban, Copy, ExternalLink, ListChecks, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, type ActivityItem } from "@/lib/api";
import { errText } from "@/lib/actions";
import { bytes, pct, speed } from "@/lib/format";
import { Link, useRouter } from "@/lib/router";
import { needsYou, useStore } from "@/lib/store";
import { cn } from "@/lib/utils";
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuLabel, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Bar, Kbd } from "./kit";
import { useConfirm } from "./Confirm";

/** Live download speed of everything in flight, for the nav and drawer. */
export function totalSpeed(activity: ActivityItem[]): number {
  return activity.reduce((n, a) => n + (a.speed ?? 0), 0);
}

const withPick = (href: string) => `${href}${href.includes("?") ? "&" : "?"}pick=1`;

/** The quick downloads panel (D). Right-click a download for more. */
export function DownloadsDrawer({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const { activity, reloadActivity } = useStore();
  const { navigate } = useRouter();
  const confirm = useConfirm();
  const total = totalSpeed(activity);
  const attention = activity.filter(needsYou);
  const rest = activity.filter((a) => !needsYou(a));

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

  const row = (a: ActivityItem) => {
    const bad = needsYou(a);
    return (
      <ContextMenu key={a.id}>
        <ContextMenuTrigger asChild>
          <div className={cn("rounded-md border bg-card p-3", bad && "border-l-2 border-l-bad")}>
            <div className="flex items-baseline gap-2">
              <span className="min-w-0 flex-1 truncate text-[13px] font-medium">
                {a.title}
                {a.subtitle && <span className="font-mono text-[11.5px] font-normal text-muted-foreground"> {a.subtitle}</span>}
              </span>
              <span className={cn("font-mono text-[11px]", bad ? "text-bad" : a.status === "downloading" ? "text-live" : "text-muted-foreground")}>{a.status === "warning" ? "stuck" : a.status}</span>
            </div>
            <div className="mt-0.5 mb-2 truncate font-mono text-[10.5px] text-muted-foreground" title={a.release}>
              {a.release}
            </div>
            <Bar value={a.progress} tone={bad ? "bad" : "live"} />
            <div className="meta mt-1.5 flex justify-between font-mono tabular">
              <span>
                {pct(a.progress)} of {bytes(a.size)}
              </span>
              <span className="truncate pl-3">{bad ? (a.messages?.[0] ?? "") : [speed(a.speed), a.eta].filter(Boolean).join(", ")}</span>
            </div>
            {bad && a.href && (
              <button onClick={() => go(withPick(a.href!))} className="mt-2 inline-flex cursor-pointer items-center gap-1.5 text-[12.5px] font-medium underline-offset-4 hover:underline">
                <ListChecks className="size-3.5" /> Choose another release
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
              <ListChecks /> Choose another release
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
        <SheetHeader className="border-b px-4 py-3.5">
          <SheetTitle className="text-[15px] font-medium">Downloads</SheetTitle>
          <SheetDescription className="font-mono text-[11px]">
            {total ? speed(total) : "idle"}, {activity.length} listed, <Kbd>D</Kbd> toggles
          </SheetDescription>
        </SheetHeader>
        <div className="flex-1 space-y-2 overflow-y-auto p-3">
          {activity.length === 0 && <div className="px-3 py-16 text-center text-[13px] text-muted-foreground">Nothing in flight.</div>}
          {attention.length > 0 && <div className="meta px-1 pt-1">Needs you</div>}
          {attention.map(row)}
          {attention.length > 0 && rest.length > 0 && <div className="meta px-1 pt-3">In flight</div>}
          {rest.map(row)}
        </div>
        <div className="flex items-center justify-between border-t px-4 py-3 text-[12.5px] text-muted-foreground">
          <span>Right-click for more</span>
          <Link to="/downloads" onClick={() => onOpenChange(false)} className="font-medium text-foreground hover:underline">
            All downloads
          </Link>
        </div>
      </SheetContent>
    </Sheet>
  );
}
