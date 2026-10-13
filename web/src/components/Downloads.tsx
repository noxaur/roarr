import { Ban, Copy, ExternalLink, Trash2 } from "lucide-react";
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

/** The downloads drawer (D). Right-click a download for more. */
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
      confirm: blocklist ? "Remove & blocklist" : "Remove",
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

  const row = (a: ActivityItem) => (
    <ContextMenu key={a.id}>
      <ContextMenuTrigger asChild>
        <div className={cn("rounded-xl border bg-background p-3 transition-colors hover:border-input", needsYou(a) && "border-destructive/40")}>
          <div className="flex items-baseline gap-2">
            <span className="min-w-0 flex-1 truncate text-[13.5px] font-medium">
              {a.title}
              {a.subtitle && <span className="text-muted-foreground"> · {a.subtitle}</span>}
            </span>
            <span className={cn("font-mono text-[11px]", needsYou(a) ? "text-destructive" : a.status === "downloading" ? "text-lime-strong" : "text-muted-foreground")}>
              {a.status === "warning" ? "stuck" : a.status}
            </span>
          </div>
          <div className="mt-1 mb-2.5 truncate font-mono text-[10.5px] text-muted-foreground" title={a.release}>
            {a.release}
          </div>
          <Bar value={a.progress} tone={needsYou(a) ? "bad" : "lime"} />
          <div className="mt-1.5 flex justify-between font-mono text-[10.5px] text-muted-foreground">
            <span>
              {pct(a.progress)} of {bytes(a.size)}
            </span>
            <span>{needsYou(a) ? (a.messages?.[0] ?? "right-click for options") : [speed(a.speed), a.eta].filter(Boolean).join(" · ")}</span>
          </div>
        </div>
      </ContextMenuTrigger>
      <ContextMenuContent className="w-60">
        <ContextMenuLabel className="truncate text-xs font-normal text-muted-foreground">{a.title}</ContextMenuLabel>
        {a.href && (
          <ContextMenuItem
            onSelect={() => {
              onOpenChange(false);
              navigate(a.href!);
            }}
          >
            <ExternalLink /> Open title
          </ContextMenuItem>
        )}
        <ContextMenuItem
          onSelect={() => {
            navigator.clipboard?.writeText(a.release).then(() => toast("Copied release name"), () => {});
          }}
        >
          <Copy /> Copy release name
        </ContextMenuItem>
        <ContextMenuSeparator />
        <ContextMenuItem onSelect={() => remove(a, false)}>
          <Trash2 /> Remove
        </ContextMenuItem>
        <ContextMenuItem variant="destructive" onSelect={() => remove(a, true)}>
          <Ban /> Remove & blocklist
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" data-downloads className="w-full gap-0 p-0 sm:max-w-[400px]">
        <SheetHeader className="border-b px-5 py-4">
          <SheetTitle className="text-base font-semibold tracking-tight">Downloads</SheetTitle>
          <SheetDescription className="font-mono text-[11px]">
            {total ? speed(total) : "idle"} · {activity.length} in the list · <Kbd>D</Kbd> toggles
          </SheetDescription>
        </SheetHeader>
        <div className="flex-1 space-y-2 overflow-y-auto p-3">
          {activity.length === 0 && <div className="px-3 py-16 text-center text-sm text-muted-foreground">Nothing in flight.</div>}
          {attention.length > 0 && <div className="label px-1 pt-1">Needs you</div>}
          {attention.map(row)}
          {attention.length > 0 && rest.length > 0 && <div className="label px-1 pt-3">Downloading</div>}
          {rest.map(row)}
        </div>
        <div className="flex items-center justify-between border-t px-5 py-3 text-xs text-muted-foreground">
          <span>Right-click a download for more</span>
          <Link to="/activity" onClick={() => onOpenChange(false)} className="font-medium text-foreground hover:underline">
            Activity page
          </Link>
        </div>
      </SheetContent>
    </Sheet>
  );
}
