import type { ReactNode } from "react";
import { ArrowLeft, Check, Download } from "lucide-react";
import { bytes } from "@/lib/format";
import { codecName, resName } from "@/lib/langs";
import type { LivePlan } from "@/lib/releases";
import type { SearchState } from "@/lib/useSearch";
import { Link } from "@/lib/router";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Dot, Spec, Spinner } from "./kit";
import { PosterImg } from "./Poster";

/** Head | sticky "get" panel; body below the head. On phones the panel follows the head. */
export function TitleLayout({ head, aside, body }: { head: ReactNode; aside: ReactNode; body: ReactNode }) {
  return (
    <div className="grid items-start gap-x-10 gap-y-6 [grid-template-areas:'head'_'aside'_'body'] lg:grid-cols-[minmax(0,1fr)_340px] lg:[grid-template-areas:'head_aside'_'body_aside']">
      <div className="min-w-0 [grid-area:head]">{head}</div>
      <aside className="self-start [grid-area:aside] lg:sticky lg:top-[68px]">{aside}</aside>
      <div className="min-w-0 [grid-area:body]">{body}</div>
    </div>
  );
}

export function Crumbs({ section }: { section: string }) {
  return (
    <nav className="mb-4 flex items-center gap-2 text-[12.5px] text-muted-foreground">
      <Link to="/" className="inline-flex items-center gap-1.5 rounded-sm outline-none hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50">
        <ArrowLeft className="size-3.5" /> Library
      </Link>
      <span>/</span>
      <span>{section}</span>
    </nav>
  );
}

/** Poster thumb for identification, title, one line of facts, and the state figures. */
export function TitleHead({
  poster,
  title,
  year,
  meta,
  state,
  actions,
}: {
  poster?: string;
  title: string;
  year?: number;
  meta: (string | number | null | undefined | false)[];
  state: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <div className="flex gap-4 sm:gap-5">
      <div className="aspect-[2/3] w-[64px] shrink-0 overflow-hidden rounded-md bg-muted sm:w-[84px]">
        <PosterImg src={poster} title={title} year={year} />
      </div>
      <div className="min-w-0 flex-1">
        <h1 className="text-[clamp(1.5rem,3.2vw,2.1rem)] leading-[1.05] font-medium text-balance">{title}</h1>
        <p className="meta mt-1.5 flex flex-wrap gap-x-2">
          {meta.filter(Boolean).map((m, i) => (
            <span key={i}>{m}</span>
          ))}
        </p>
        <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1.5">{state}</div>
        {actions && <div className="mt-3 flex flex-wrap items-center gap-2">{actions}</div>}
      </div>
    </div>
  );
}

export interface GetOption {
  value: string;
  label: string;
  sub?: string;
  count: number | string;
  disabled?: boolean;
}

/**
 * The one question every title page asks: what do you want? Picking an
 * option re-scopes the search; the button grabs roarr's best pick for it.
 */
export function GetCard({
  options,
  value,
  onValueChange,
  cta,
  onGet,
  busy,
  done,
  disabled,
  state,
  plan,
  wantedCount,
  onChoose,
  watchFirst,
  onWatchFirst,
  note,
}: {
  options: GetOption[];
  value: string;
  onValueChange: (v: string) => void;
  cta: string;
  onGet: () => void;
  busy: boolean;
  done: boolean;
  disabled: boolean;
  state: SearchState;
  plan: LivePlan;
  wantedCount: number;
  onChoose: () => void;
  watchFirst?: boolean;
  onWatchFirst?: (v: boolean) => void;
  note?: ReactNode;
}) {
  const indexers = [...state.indexers.values()].filter((i) => !i.disabled);
  const answered = indexers.filter((i) => i.answered >= i.queries).length;
  const pending = !state.done && wantedCount > 0;
  const ready = plan.picks.length > 0;
  const first = plan.picks[0]?.release;

  return (
    <div className="rounded-lg border bg-card p-4">
      {options.length === 1 ? (
        <div className="flex items-center gap-3 rounded-md bg-muted px-3 py-2">
          <span className="min-w-0 flex-1">
            <span className="block font-medium">{options[0].label}</span>
            {options[0].sub && <span className="meta block truncate">{options[0].sub}</span>}
          </span>
          <span className="font-mono text-[14px] tabular">{options[0].count}</span>
        </div>
      ) : (
        <RadioGroup value={value} onValueChange={onValueChange} className="gap-0.5">
          {options.map((o) => (
            <label
              key={o.value}
              className={cn(
                "flex cursor-pointer items-center gap-3 rounded-md px-2.5 py-2 hover:bg-muted/60 has-[[data-state=checked]]:bg-muted",
                o.disabled && "pointer-events-none opacity-45",
              )}
            >
              <RadioGroupItem value={o.value} disabled={o.disabled} className="size-4 border-[1.5px] data-[state=checked]:border-[5px] data-[state=checked]:border-foreground [&_svg]:hidden" />
              <span className="min-w-0 flex-1">
                <span className="block font-medium">{o.label}</span>
                {o.sub && <span className="meta block truncate">{o.sub}</span>}
              </span>
              <span className="font-mono text-[14px] tabular">{o.count}</span>
            </label>
          ))}
        </RadioGroup>
      )}

      <Button size="lg" className="mt-3 w-full" disabled={disabled || busy || done} onClick={onGet}>
        {busy ? <Spinner className="size-4" /> : done ? <Check /> : <Download />}
        {done ? "Sent" : cta}
      </Button>
      <Button variant="outline" className="mt-1.5 w-full" onClick={onChoose} disabled={wantedCount === 0}>
        Choose release
        {plan.all.length > 0 && <span className="font-mono text-[11.5px] text-muted-foreground tabular">{plan.all.length}</span>}
      </Button>

      {wantedCount > 0 && (
        <div className="mt-3 border-t pt-3">
          <div className="flex items-center gap-2 text-[12.5px]">
            {pending && !ready ? <Spinner className="size-3" /> : <Dot tone={ready ? "have" : pending ? "live" : "miss"} />}
            <span>{ready ? (pending ? "Best pick so far" : "Best pick") : pending ? "Searching" : "Nothing suitable yet"}</span>
            <span className="meta ml-auto font-mono tabular">
              {answered}/{indexers.length || "…"} indexers
            </span>
          </div>
          {first && (
            <>
              <div className="mt-1.5 font-mono text-[11.5px] leading-snug break-all text-foreground/80" title={first.title}>
                {plan.picks.length > 1 ? `${plan.picks.length} releases, first: ` : ""}
                {first.title}
              </div>
              <div className="mt-1.5 flex flex-wrap items-center gap-1">
                {[resName(first.info.resolution), first.info.source, codecName(first.info.codec), first.info.hdr].filter(Boolean).map((t) => (
                  <Spec key={String(t)}>{t}</Spec>
                ))}
                <span className="meta ml-auto font-mono tabular">{bytes(plan.size)}</span>
              </div>
            </>
          )}
          {plan.missing.length > 0 && state.done && ready && <div className="meta mt-1.5">{plan.missing.length} not available yet.</div>}
        </div>
      )}

      {onWatchFirst && (
        <label className="mt-3 flex cursor-pointer items-center gap-2.5 border-t pt-3 text-[12.5px] text-foreground/80">
          <Switch checked={!!watchFirst} onCheckedChange={onWatchFirst} />
          Import episodes as they finish
        </label>
      )}
      {note && <div className="meta mt-2.5">{note}</div>}
    </div>
  );
}

export function TitleSkeleton() {
  return (
    <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_340px]">
      <div className="flex gap-5">
        <Skeleton className="aspect-[2/3] w-[84px] rounded-md" />
        <div className="flex-1 space-y-3">
          <Skeleton className="h-8 w-2/3" />
          <Skeleton className="h-3 w-1/3" />
          <Skeleton className="h-3 w-1/2" />
        </div>
      </div>
      <Skeleton className="h-[300px] rounded-lg" />
    </div>
  );
}
