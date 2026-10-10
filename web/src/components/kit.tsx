import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

// Small layout pieces shared by every page: the numbered mono labels, the
// big display headings and the stat columns.

export function Label({ children, mark, className }: { children: ReactNode; mark?: boolean; className?: string }) {
  return (
    <div className={cn("label", className)}>
      {mark && <span aria-hidden>┌─ </span>}
      {children}
    </div>
  );
}

export function PageHeader({ label, title, children, size = "xl" }: { label: ReactNode; title: ReactNode; children?: ReactNode; size?: "xl" | "lg" }) {
  return (
    <header className="flex flex-wrap items-end justify-between gap-x-8 gap-y-6">
      <div className="min-w-0">
        <Label mark>{label}</Label>
        <h1 className={cn("mt-4 font-display", size === "xl" ? "text-[clamp(3.5rem,9vw,7.25rem)]" : "text-[clamp(3rem,6vw,4.75rem)]")}>{title}</h1>
      </div>
      {children}
    </header>
  );
}

export function Stats({ children }: { children: ReactNode }) {
  return <dl className="flex flex-wrap gap-y-4">{children}</dl>;
}

export function Stat({ value, unit, label }: { value: ReactNode; unit?: string; label: string }) {
  return (
    <div className="border-l border-dashed border-dash px-5 first:border-l-0 first:pl-0 sm:first:border-l sm:first:pl-5">
      <dt className="font-display text-[2.5rem] leading-none tracking-[-0.04em]">
        {value}
        {unit && <small className="ml-1 font-mono text-sm font-medium tracking-normal text-muted-foreground">{unit}</small>}
      </dt>
      <dd className="label mt-1.5">{label}</dd>
    </div>
  );
}

export function SectionHead({ index, title, children, className }: { index?: number; title: string; children?: ReactNode; className?: string }) {
  return (
    <div className={cn("flex flex-wrap items-center gap-x-5 gap-y-3", className)}>
      <Label>
        {index !== undefined && `${String(index).padStart(2, "0")} — `}
        {title}
      </Label>
      {children}
    </div>
  );
}

export function Empty({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div className={cn("rounded-xl border border-dashed border-dash px-6 py-10 text-center text-muted-foreground", className)}>
      <div aria-hidden className="mb-2 font-mono text-xs tracking-widest">┌──────┐</div>
      {children}
    </div>
  );
}

export function Notice({ tone = "warn", children, className }: { tone?: "warn" | "bad" | "info"; children: ReactNode; className?: string }) {
  return (
    <div
      role={tone === "bad" ? "alert" : "status"}
      className={cn(
        "rounded-xl px-4 py-3 text-sm",
        tone === "warn" && "bg-pink/60 text-ink dark:bg-pink/15 dark:text-foreground",
        tone === "bad" && "border border-destructive/40 text-destructive",
        tone === "info" && "bg-ice text-ink dark:bg-ice/10 dark:text-foreground",
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

/** Thin progress bar; lime while moving, ink in the light theme's tiles. */
export function Bar({ value, className, tone = "lime" }: { value: number; className?: string; tone?: "lime" | "ink" | "warn" | "bad" }) {
  return (
    <div className={cn("h-1 overflow-hidden rounded-full bg-foreground/10", className)}>
      <i
        className={cn(
          "block h-full rounded-full transition-[width] duration-500",
          tone === "lime" && "bg-lime-strong",
          tone === "ink" && "bg-foreground",
          tone === "warn" && "bg-pink",
          tone === "bad" && "bg-destructive",
        )}
        style={{ width: `${Math.max(0, Math.min(1, value)) * 100}%` }}
      />
    </div>
  );
}

export function Kbd({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <kbd className={cn("inline-flex h-5 min-w-5 items-center justify-center rounded-[5px] border border-b-2 border-border bg-background px-1.5 font-mono text-[10.5px] text-muted-foreground", className)}>
      {children}
    </kbd>
  );
}
