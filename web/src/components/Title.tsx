import type { ReactNode } from "react";
import { ArrowLeft } from "lucide-react";
import { bytes } from "@/lib/format";
import type { LivePlan } from "@/lib/releases";
import { Link } from "@/lib/router";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "./kit";
import { PosterImg } from "./Poster";

/** Head | sticky "get" panel; body below the head. On phones the panel follows the head. */
export function TitleLayout({ head, aside, body }: { head: ReactNode; aside: ReactNode; body: ReactNode }) {
  return (
    <div className="grid items-start gap-x-8 gap-y-8 [grid-template-areas:'head'_'aside'_'body'] lg:grid-cols-[minmax(0,1fr)_320px] lg:[grid-template-areas:'head_aside'_'body_aside']">
      <div className="min-w-0 [grid-area:head]">{head}</div>
      <aside className="self-start [grid-area:aside] lg:sticky lg:top-12">{aside}</aside>
      <div className="min-w-0 [grid-area:body]">{body}</div>
    </div>
  );
}

export function Crumbs({ section }: { section: "Shows" | "Movies" }) {
  return (
    <nav className="mb-6 flex items-center gap-2 text-[13px] text-muted-foreground">
      <Link to={section === "Shows" ? "/shows" : "/movies"} className="inline-flex items-center gap-2 rounded-[2px] outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/60">
        <ArrowLeft className="size-4" /> Library
      </Link>
      <span>/</span>
      <span>{section}</span>
    </nav>
  );
}

/** Poster thumb for identification, title, and one line of facts in mono. */
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
  state?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <div className="flex gap-6">
      <div className="aspect-[2/3] w-16 shrink-0 overflow-hidden rounded-[2px] bg-muted sm:w-[84px]">
        <PosterImg src={poster} title={title} year={year} />
      </div>
      <div className="min-w-0 flex-1 pt-1">
        <div className="flex items-start gap-4">
          <h1 className="min-w-0 flex-1 text-[clamp(1.5rem,3vw,2.25rem)] leading-[1.1] font-medium text-balance">{title}</h1>
          {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
        </div>
        <p className="mt-2 flex flex-wrap gap-x-2 font-mono text-[12px] text-muted-foreground">
          {meta.filter(Boolean).map((m, i) => (
            <span key={i} className="whitespace-nowrap">
              {i > 0 && <span className="mr-2 opacity-60">·</span>}
              {m}
            </span>
          ))}
        </p>
        {state && <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2">{state}</div>}
      </div>
    </div>
  );
}

export interface GetOption {
  value: string;
  label: string;
  disabled?: boolean;
}

/**
 * The one question every title page asks: what do you want? Picking an
 * option re-scopes the search; the button grabs roarr's best pick for it.
 * The card holds only the action. The reason for the pick is one click away,
 * in "Pick releases manually".
 */
export function GetCard({
  options,
  value,
  onValueChange,
  cta,
  onGet,
  busy,
  disabled,
  plan,
  wantedCount,
  onChoose,
  chooseLabel = "Pick releases manually",
}: {
  options: GetOption[];
  value: string;
  onValueChange: (v: string) => void;
  cta: string;
  onGet: () => void;
  busy: boolean;
  disabled: boolean;
  plan: LivePlan;
  wantedCount: number;
  onChoose: () => void;
  chooseLabel?: string;
}) {
  return (
    <div className="rounded-[2px] bg-card p-4">
      {options.length > 1 && (
        <RadioGroup value={value} onValueChange={onValueChange} className="mb-4 gap-1">
          {options.map((o) => (
            <label
              key={o.value}
              className={cn(
                "flex h-9 cursor-pointer items-center gap-3 rounded-[2px] px-3 text-[14px] hover:bg-accent/60 has-[[data-state=checked]]:bg-accent",
                o.disabled && "pointer-events-none opacity-40",
              )}
            >
              <RadioGroupItem value={o.value} disabled={o.disabled} />
              {o.label}
            </label>
          ))}
        </RadioGroup>
      )}

      <Button size="lg" className="w-full justify-between" disabled={disabled || busy} onClick={onGet}>
        <span className="flex min-w-0 items-center gap-2 truncate">
          {busy && <Spinner className="size-4" />}
          {cta}
        </span>
        {plan.picks.length > 0 && plan.size > 0 && <span className="font-mono text-[12px] tabular">{bytes(plan.size)}</span>}
      </Button>
      <Button variant="outline" className="mt-2 w-full justify-start" onClick={onChoose} disabled={wantedCount === 0}>
        {chooseLabel}
      </Button>
    </div>
  );
}

/**
 * What a title page shows while its details load. The library already knows
 * the poster and the name, so they appear at once, in the place the page puts
 * them; only the parts that wait on Sonarr or Radarr are placeholders.
 */
export function TitleSkeleton({ section, known }: { section: "Shows" | "Movies"; known?: { poster?: string; title: string; year?: number } }) {
  return (
    <>
      <Crumbs section={section} />
      <TitleLayout
        head={
          known ? (
            <TitleHead poster={known.poster} title={known.title} year={known.year} meta={[known.year]} />
          ) : (
            <div className="flex gap-6">
              <Skeleton className="aspect-[2/3] w-16 shrink-0 sm:w-[84px]" />
              <div className="flex-1 space-y-3 pt-1">
                <Skeleton className="h-8 w-2/3" />
                <Skeleton className="h-3 w-1/3" />
              </div>
            </div>
          )
        }
        aside={<Skeleton className="h-[248px]" />}
        body={null}
      />
    </>
  );
}
