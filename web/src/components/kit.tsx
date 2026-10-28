import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

// Small pieces every page shares. Colour here always means state:
// have / miss / live / bad (see styles.css).

export type Tone = "have" | "miss" | "live" | "bad" | "none";

export function PageHeader({ title, sub, children }: { title: ReactNode; sub?: ReactNode; children?: ReactNode }) {
  return (
    <header className="flex flex-wrap items-end justify-between gap-x-8 gap-y-4">
      <div className="min-w-0">
        <h1 className="text-[26px] leading-none font-medium">{title}</h1>
        {sub && <p className="meta mt-2">{sub}</p>}
      </div>
      {children}
    </header>
  );
}

export function SectionHead({ title, children, className }: { title: ReactNode; children?: ReactNode; className?: string }) {
  return (
    <div className={cn("flex flex-wrap items-center gap-x-4 gap-y-2", className)}>
      <h2 className="text-[15px] font-medium">{title}</h2>
      {children}
    </div>
  );
}

export function Empty({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("rounded-lg border border-dashed px-6 py-12 text-center text-[13.5px] text-muted-foreground", className)}>{children}</div>;
}

export function Notice({ tone = "miss", children, className }: { tone?: Tone; children: ReactNode; className?: string }) {
  return (
    <div
      role={tone === "bad" ? "alert" : "status"}
      className={cn(
        "rounded-lg px-3.5 py-2.5 text-[13px] leading-snug",
        tone === "miss" && "bg-miss-soft text-foreground",
        tone === "bad" && "bg-bad-soft text-foreground",
        tone === "live" && "bg-live-soft text-foreground",
        tone === "have" && "bg-have-soft text-foreground",
        tone === "none" && "bg-muted",
        className,
      )}
    >
      {children}
    </div>
  );
}

export function Spinner({ className }: { className?: string }) {
  return <span aria-hidden className={cn("inline-block size-3.5 animate-spin rounded-full border-2 border-current border-t-transparent opacity-70", className)} />;
}

/** A dot carrying one state. */
export function Dot({ tone, pulse, className }: { tone: Tone; pulse?: boolean; className?: string }) {
  return (
    <span
      aria-hidden
      className={cn(
        "inline-block size-2 shrink-0 rounded-full",
        tone === "have" && "bg-have",
        tone === "miss" && "bg-miss",
        tone === "live" && "bg-live",
        tone === "bad" && "bg-bad",
        tone === "none" && "bg-muted-foreground/40",
        pulse && "motion-safe:animate-pulse",
        className,
      )}
    />
  );
}

/** Pill with a state dot and short text: "4 missing", "Downloading". */
export function State({ tone, children, className }: { tone: Tone; children: ReactNode; className?: string }) {
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-[12.5px] whitespace-nowrap", tone === "bad" ? "text-bad" : "text-foreground/85", className)}>
      <Dot tone={tone} />
      {children}
    </span>
  );
}

/** Thin progress bar. */
export function Bar({ value, className, tone = "live" }: { value: number; className?: string; tone?: Tone }) {
  return (
    <div className={cn("h-1 overflow-hidden rounded-full bg-foreground/10", className)}>
      <i
        className={cn(
          "block h-full rounded-full transition-[width] duration-500",
          tone === "live" && "bg-live",
          tone === "have" && "bg-have",
          tone === "miss" && "bg-miss",
          tone === "bad" && "bg-bad",
          tone === "none" && "bg-foreground",
        )}
        style={{ width: `${Math.max(0, Math.min(1, value)) * 100}%` }}
      />
    </div>
  );
}

export function Kbd({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <kbd className={cn("inline-flex h-5 min-w-5 items-center justify-center rounded-[4px] border border-b-2 bg-background px-1.5 font-mono text-[10.5px] text-muted-foreground", className)}>
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
        dim && "bg-transparent text-muted-foreground ring-1 ring-border ring-inset",
        className,
      )}
    >
      {children}
    </span>
  );
}

/** A labelled number for a header: "18 / 24  on disk". */
export function Figure({ value, label, tone }: { value: ReactNode; label: string; tone?: Tone }) {
  return (
    <span className="inline-flex items-baseline gap-1.5 whitespace-nowrap">
      <b className={cn("font-mono text-[15px] font-medium tabular", tone === "miss" && "text-miss", tone === "live" && "text-live", tone === "bad" && "text-bad", tone === "have" && "text-have")}>
        {value}
      </b>
      <span className="meta">{label}</span>
    </span>
  );
}
