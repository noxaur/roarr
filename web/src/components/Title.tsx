import type { ReactNode } from "react";
import { ArrowLeft, ArrowRight, Check, Download } from "lucide-react";
import { bytes } from "@/lib/format";
import type { LivePlan } from "@/lib/releases";
import type { SearchState } from "@/lib/useSearch";
import { Link } from "@/lib/router";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Label, Spinner } from "./kit";
import { PosterImg } from "./Poster";

/** Poster | heading + body | sticky "get" card. On phones the card follows the heading. */
export function TitleLayout({ poster, head, aside, body }: { poster: ReactNode; head: ReactNode; aside: ReactNode; body: ReactNode }) {
  return (
    <div className="grid items-start gap-x-10 gap-y-8 [grid-template-areas:'head'_'aside'_'body'] md:grid-cols-[minmax(0,1fr)_340px] md:[grid-template-areas:'head_aside'_'body_aside'] lg:grid-cols-[200px_minmax(0,1fr)_360px] lg:[grid-template-areas:'poster_head_aside'_'poster_body_aside']">
      <div className="hidden self-start [grid-area:poster] lg:block">{poster}</div>
      <div className="min-w-0 [grid-area:head]">{head}</div>
      <aside className="self-start [grid-area:aside] md:sticky md:top-24">{aside}</aside>
      <div className="min-w-0 [grid-area:body]">{body}</div>
    </div>
  );
}

export function Crumbs({ section }: { section: string }) {
  return (
    <nav className="mb-6 flex items-center gap-2.5 font-mono text-[11.5px] text-muted-foreground">
      <Link to="/" className="inline-flex items-center gap-2 hover:text-foreground">
        <ArrowLeft className="size-3.5" /> Library
      </Link>
      <span>/</span>
      <span>{section}</span>
    </nav>
  );
}

export function TitleHead({
  poster,
  title,
  year,
  meta,
  overview,
  badges,
}: {
  poster?: string;
  title: string;
  year?: number;
  meta: (string | number | null | undefined | false)[];
  overview?: string;
  badges: ReactNode;
}) {
  return (
    <div>
      <div className="mb-4 flex gap-4 lg:hidden">
        <div className="aspect-[2/3] w-24 shrink-0 overflow-hidden rounded-lg">
          <PosterImg src={poster} title={title} year={year} />
        </div>
      </div>
      <Label>{meta.filter(Boolean).join(" · ")}</Label>
      <h1 className="mt-3.5 font-display text-[clamp(3rem,7.5vw,6.75rem)] break-words">{title}</h1>
      {overview && <p className="mt-5 max-w-[60ch] text-[16.5px] leading-relaxed text-foreground/75">{overview}</p>}
      <div className="mt-4 flex flex-wrap gap-1.5">{badges}</div>
    </div>
  );
}

export function PosterColumn({ poster, title, year }: { poster?: string; title: string; year?: number }) {
  return (
    <div className="aspect-[2/3] overflow-hidden rounded-xl bg-muted">
      <PosterImg src={poster} title={title} year={year} />
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
  const secs = state.doneAt ? `${((state.doneAt - state.startedAt) / 1000).toFixed(2)} s` : "";

  return (
    <div className="rounded-2xl border bg-card p-5">
      <Label mark className="mb-2.5">
        What do you want?
      </Label>
      {options.length === 1 ? (
        <div className="-mx-1.5 flex items-center gap-3 rounded-xl bg-muted px-3 py-2.5">
          <span className="min-w-0 flex-1">
            <span className="block font-semibold">{options[0].label}</span>
            {options[0].sub && <span className="block truncate font-mono text-[11px] text-muted-foreground">{options[0].sub}</span>}
          </span>
          <span className="font-mono text-[15px] tabular">{options[0].count}</span>
        </div>
      ) : (
        <RadioGroup value={value} onValueChange={onValueChange} className="gap-0.5">
          {options.map((o) => (
            <label
              key={o.value}
              className={cn(
                "-mx-1.5 flex cursor-pointer items-center gap-3 rounded-xl px-3 py-2.5 transition-colors hover:bg-muted/60 has-[[data-state=checked]]:bg-muted",
                o.disabled && "pointer-events-none opacity-45",
              )}
            >
              <RadioGroupItem
                value={o.value}
                disabled={o.disabled}
                className="size-[18px] border-[1.5px] border-dash data-[state=checked]:border-[5.5px] data-[state=checked]:border-foreground [&_svg]:hidden"
              />
              <span className="min-w-0 flex-1">
                <span className="block font-semibold">{o.label}</span>
                {o.sub && <span className="block truncate font-mono text-[11px] text-muted-foreground">{o.sub}</span>}
              </span>
              <span className="font-mono text-[15px] tabular">{o.count}</span>
            </label>
          ))}
        </RadioGroup>
      )}

      <Button size="lg" className="mt-4 w-full" disabled={disabled || busy || done} onClick={onGet}>
        {busy ? <Spinner className="size-4" /> : done ? <Check /> : <Download />}
        {done ? "On its way" : cta}
      </Button>

      {wantedCount > 0 && (
        <div className="mt-3 rounded-xl border border-dashed border-dash px-3.5 py-3">
          <div className="flex items-center gap-2 font-mono text-xs">
            {pending && !ready ? <Spinner className="size-3" /> : <i className={cn("size-2 rounded-full", ready ? "bg-lime ring-1 ring-ink/25" : "bg-pink")} />}
            <span>{ready ? (pending ? "Best pick so far" : "Best pick ready") : pending ? "Searching indexers…" : "Nothing suitable yet"}</span>
            <span className="ml-auto text-[10.5px] tracking-wide text-muted-foreground uppercase">
              {answered}/{indexers.length || "…"} indexers{secs && ` · ${secs}`}
            </span>
          </div>
          {first && (
            <>
              <div className="mt-2 font-mono text-[11.5px] leading-snug break-all text-foreground/80">
                {plan.picks.length > 1 ? `${plan.picks.length} releases, e.g. ` : ""}
                {first.title}
              </div>
              <div className="mt-2 flex flex-wrap items-center gap-1">
                {[first.info.resolution && (first.info.resolution === 2160 ? "4K" : `${first.info.resolution}p`), first.info.source, first.info.codec]
                  .filter(Boolean)
                  .map((t) => (
                    <Badge key={String(t)}>{t}</Badge>
                  ))}
                <span className="ml-auto font-mono text-xs text-muted-foreground">{bytes(plan.size)}</span>
              </div>
            </>
          )}
          {plan.missing.length > 0 && state.done && ready && (
            <div className="mt-2 text-xs text-muted-foreground">
              {plan.missing.length} not available yet; roarr gets the rest.
            </div>
          )}
        </div>
      )}

      <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
        {onWatchFirst ? (
          <label className="flex cursor-pointer items-center gap-2.5 text-[13px] text-foreground/80">
            <Switch checked={!!watchFirst} onCheckedChange={onWatchFirst} />
            Play in order as they land
          </label>
        ) : (
          <span />
        )}
        <Button variant="link" size="sm" onClick={onChoose} disabled={wantedCount === 0}>
          Choose release myself <ArrowRight />
        </Button>
      </div>
      {note && <div className="mt-3 text-xs text-muted-foreground">{note}</div>}
    </div>
  );
}

export function TitleSkeleton() {
  return (
    <div className="grid gap-10 lg:grid-cols-[200px_minmax(0,1fr)_360px]">
      <Skeleton className="hidden aspect-[2/3] rounded-xl lg:block" />
      <div className="space-y-4">
        <Skeleton className="h-3 w-1/3" />
        <Skeleton className="h-24 w-3/4" />
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-4 w-2/3" />
      </div>
      <Skeleton className="h-[420px] rounded-2xl" />
    </div>
  );
}

export function sizeLabel(n?: number) {
  return n ? bytes(n) : null;
}
