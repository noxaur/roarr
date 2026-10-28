import { useState } from "react";
import { Check, RefreshCw, RotateCcw } from "lucide-react";
import { toast } from "sonner";
import { api, type VerifyRecord } from "@/lib/api";
import { errText } from "@/lib/actions";
import { useStore } from "@/lib/store";
import { cn } from "@/lib/utils";
import { headline, outcome, problems, tag } from "@/lib/verify";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { useConfirm } from "./Confirm";
import { Spec, Spinner, type Tone } from "./kit";

// A download's check verdict where the person decides on it: the reasons,
// then Re-check, Re-grab or Keep anyway.

export const verdictTone = (r: VerifyRecord): Tone => (outcome(r) === "warn" ? "miss" : outcome(r) === "checking" ? "live" : "bad");

type Busy = "recheck" | "regrab" | "keep" | null;

/** The three decisions, with their confirm and toasts. */
export function useVerdictActions() {
  const { reloadVerify, reloadActivity, reloadLibrary } = useStore();
  const confirm = useConfirm();
  const [busy, setBusy] = useState<Busy>(null);

  const go = async (b: Busy, f: () => Promise<void>) => {
    setBusy(b);
    try {
      await f();
    } finally {
      setBusy(null);
      reloadVerify();
    }
  };

  const recheck = (r: VerifyRecord) =>
    go("recheck", async () => {
      const id = toast.loading(`Checking ${r.title}…`);
      try {
        const next = await api.verifyRecheck(r.hash);
        const o = outcome(next);
        if (o === "ok") toast.success(`${r.title} passes`, { id });
        else toast.warning(`${r.title}: ${headline(next)}`, { id });
      } catch (e) {
        toast.error(errText(e), { id });
      }
    });

  const regrab = async (r: VerifyRecord) => {
    const ok = await confirm({
      title: `Re-grab ${r.title}?`,
      body: (
        <>
          <p>Removes and blocklists this download, deletes the files it imported, then gets the next best release.</p>
          <p className="font-mono text-xs break-all text-foreground/70">{r.release}</p>
        </>
      ),
      confirm: "Re-grab",
      danger: true,
    });
    if (!ok) return;
    await go("regrab", async () => {
      const id = toast.loading(`Re-grabbing ${r.title}…`);
      try {
        const next = await api.verifyRegrab(r.hash);
        toast.success([next.regrab?.message || `Re-grabbed ${r.title}`, next.regrab?.note].filter(Boolean).join(" "), { id });
        reloadActivity();
        setTimeout(() => reloadLibrary(), 1500);
      } catch (e) {
        toast.error(errText(e), { id });
      }
    });
  };

  const keep = (r: VerifyRecord) =>
    go("keep", async () => {
      try {
        await api.verifyDismiss(r.hash);
        toast.success(`Kept ${r.title}`);
      } catch (e) {
        toast.error(errText(e));
      }
    });

  return { busy, recheck, regrab, keep };
}

/** Per-check reasons, worst first. */
export function VerdictChecks({ r, className }: { r: VerifyRecord; className?: string }) {
  const list = problems(r);
  if (outcome(r) === "regrabFailed") {
    return <p className={cn("text-[13px] text-bad", className)}>{headline(r)}</p>;
  }
  if (list.length === 0) return <p className={cn("text-[13px]", className)}>{headline(r)}</p>;
  return (
    <dl className={cn("grid grid-cols-[auto_minmax(0,1fr)] items-baseline gap-x-3 gap-y-1", className)}>
      {list.map((c, i) => (
        <div key={i} className="contents">
          <dt className={cn("font-mono text-[11px]", c.level === "fail" ? "text-bad" : "text-miss")}>{c.name}</dt>
          <dd className="text-[13px] leading-snug">{c.reason}</dd>
        </div>
      ))}
    </dl>
  );
}

/** Re-check / Re-grab / Keep anyway. */
export function VerdictActions({ r, size = "sm", className }: { r: VerifyRecord; size?: "sm" | "xs"; className?: string }) {
  const { busy, recheck, regrab, keep } = useVerdictActions();
  const o = outcome(r);
  return (
    <div className={cn("flex flex-wrap items-center gap-2", className)}>
      <Button size={size} variant={o === "warn" ? "outline" : "default"} disabled={!!busy} onClick={() => regrab(r)}>
        {busy === "regrab" ? <Spinner /> : <RotateCcw />} Re-grab
      </Button>
      {o !== "regrabFailed" && (
        <Button size={size} variant="outline" disabled={!!busy} onClick={() => recheck(r)}>
          {busy === "recheck" ? <Spinner /> : <RefreshCw />} Re-check
        </Button>
      )}
      <Button size={size} variant="ghost" disabled={!!busy} onClick={() => keep(r)}>
        {busy === "keep" ? <Spinner /> : <Check />} Keep anyway
      </Button>
    </div>
  );
}

// Rows under a badge select on click and drag; keep the badge's events to itself.
const stop = (e: { stopPropagation: () => void }) => e.stopPropagation();

/** Compact badge for dense rows; opens the reasons and actions. */
export function VerdictBadge({ r, className }: { r: VerifyRecord; className?: string }) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          onClick={stop}
          onPointerDown={stop}
          onMouseDown={stop}
          className={cn("shrink-0 cursor-pointer rounded-[4px] outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50", className)}
          aria-label={`${r.title}: ${tag(r)}`}
        >
          <Spec tone={verdictTone(r)}>{tag(r)}</Spec>
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[min(92vw,380px)] p-3.5" onClick={stop} onPointerDown={stop} onMouseDown={stop}>
        <VerdictCard r={r} />
      </PopoverContent>
    </Popover>
  );
}

/** Release, reasons and actions: the body of a badge, or a row's detail. */
export function VerdictCard({ r }: { r: VerifyRecord }) {
  return (
    <div className="space-y-3">
      <div className="truncate font-mono text-[11px] text-muted-foreground" title={r.release}>
        {r.release}
      </div>
      <VerdictChecks r={r} />
      <VerdictActions r={r} size="xs" />
    </div>
  );
}
