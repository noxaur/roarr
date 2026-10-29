import type { ReactNode } from "react";
import type { LibraryItem } from "@/lib/api";
import { cn } from "@/lib/utils";

// Small pieces every page shares. Colour here always means state:
// have / miss / live / bad (see styles.css).

export type Tone = "have" | "miss" | "live" | "bad" | "none";

export function PageHeader({ title, sub, children }: { title: ReactNode; sub?: ReactNode; children?: ReactNode }) {
  return (
    <header className="flex flex-wrap items-end justify-between gap-x-8 gap-y-4">
      <div className="min-w-0">
        <h1 className="text-[24px] leading-8 font-semibold">{title}</h1>
        {sub && <p className="meta mt-1">{sub}</p>}
      </div>
      {children}
    </header>
  );
}

export function SectionHead({ title, children, className }: { title: ReactNode; children?: ReactNode; className?: string }) {
  return (
    <div className={cn("flex flex-wrap items-center gap-x-4 gap-y-2", className)}>
      <h2 className="label-caps">{title}</h2>
      {children}
    </div>
  );
}

export function Empty({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("rounded-lg bg-card px-6 py-12 text-center text-[14px] text-muted-foreground", className)}>{children}</div>;
}

export function Notice({ tone = "miss", children, className }: { tone?: Tone; children: ReactNode; className?: string }) {
  return (
    <div
      role={tone === "bad" ? "alert" : "status"}
      className={cn(
        "flex gap-3 rounded-lg bg-card px-4 py-3 text-[13px] leading-5",
        className,
      )}
    >
      {tone !== "none" && <Dot tone={tone} className="mt-2" />}
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}

export function Spinner({ className }: { className?: string }) {
  return <span aria-hidden className={cn("inline-block size-3 animate-spin rounded-full border-2 border-current border-t-transparent opacity-70", className)} />;
}

const fill: Record<Tone, string> = {
  have: "bg-have",
  miss: "bg-miss",
  live: "bg-live",
  bad: "bg-bad",
  none: "bg-unlit",
};

/** One state, drawn as a small square cell. */
export function Dot({ tone, pulse, className }: { tone: Tone; pulse?: boolean; className?: string }) {
  return <span aria-hidden className={cn("inline-block size-2 shrink-0 rounded-[1px]", fill[tone], pulse && "motion-safe:animate-pulse", className)} />;
}

/** A state cell with short text: "missing", "Downloading". */
export function State({ tone, children, className }: { tone: Tone; children: ReactNode; className?: string }) {
  return (
    <span className={cn("inline-flex items-center gap-2 text-[12px] whitespace-nowrap", tone === "bad" ? "text-bad" : "text-foreground/85", className)}>
      <Dot tone={tone} />
      {children}
    </span>
  );
}

/**
 * Progress as a row of segments that light one by one, like a VU meter.
 * Never a smooth bar. Segments are 4 px with 4 px gaps; `stretch` lets them
 * share the width instead.
 */
export function Bar({ value, className, tone = "live", segments = 20, stretch }: { value: number; className?: string; tone?: Tone; segments?: number; stretch?: boolean }) {
  const lit = Math.round(Math.max(0, Math.min(1, value)) * segments);
  return (
    <div className={cn("flex h-2 gap-1", className)} role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(value * 100)}>
      {Array.from({ length: segments }, (_, i) => (
        <i key={i} className={cn("block h-full rounded-[1px]", stretch ? "flex-1" : "w-1", i < lit ? fill[tone === "none" ? "live" : tone] : "bg-unlit")} />
      ))}
    </div>
  );
}

/** A strip of state cells: a title's episodes, a release's cover, a mini grid. */
export function CellStrip({ cells, className }: { cells: Tone[]; className?: string }) {
  return (
    <span aria-hidden className={cn("flex flex-wrap gap-1", className)}>
      {cells.map((t, i) => (
        <i key={i} className={cn("block size-2 shrink-0 rounded-[1px]", fill[t])} />
      ))}
    </span>
  );
}

/**
 * A title's cells: one per episode (one for a movie), on disk first, then
 * the rest. Long shows are grouped so the strip stays at most `max` cells.
 */
export function libraryCells(it: LibraryItem, downloading: boolean, max = 48): Tone[] {
  const total = Math.max(1, it.total);
  const per = Math.ceil(total / max);
  const n = Math.ceil(total / per);
  const have = Math.floor(it.have / per);
  const gap: Tone = downloading ? "live" : it.monitored ? "miss" : "none";
  return Array.from({ length: n }, (_, i) => (i < have ? "have" : i === have && downloading ? "live" : gap === "live" ? "miss" : gap));
}

export function Kbd({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <kbd className={cn("inline-flex h-5 min-w-5 items-center justify-center rounded-[2px] bg-muted px-1 font-mono text-[11px] text-muted-foreground", className)}>
      {children}
    </kbd>
  );
}

/** Compact mono spec tag. `tone` tints it; `dim` is for "unknown". */
export function Spec({ children, tone, dim, title, className }: { children: ReactNode; tone?: Tone; dim?: boolean; title?: string; className?: string }) {
  return (
    <span
      title={title}
      className={cn(
        "spec",
        tone === "have" && "bg-have-soft text-have",
        tone === "miss" && "bg-miss-soft text-miss",
        tone === "live" && "bg-live-soft text-live",
        tone === "bad" && "bg-bad-soft text-bad",
        dim && "bg-transparent text-muted-foreground",
        className,
      )}
    >
      {children}
    </span>
  );
}

/** A labelled number for a header: "22.3 GB on disk". */
export function Figure({ value, label, tone }: { value: ReactNode; label: string; tone?: Tone }) {
  return (
    <span className="inline-flex items-baseline gap-2 whitespace-nowrap">
      <b className={cn("font-mono text-[14px] font-normal tabular", tone === "miss" && "text-miss", tone === "live" && "text-live", tone === "bad" && "text-bad", tone === "have" && "text-have")}>
        {value}
      </b>
      {label && <span className="meta">{label}</span>}
    </span>
  );
}
