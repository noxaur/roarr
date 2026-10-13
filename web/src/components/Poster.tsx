import { useState, type ReactNode } from "react";
import { Link } from "@/lib/router";
import { cn } from "@/lib/utils";

// When there is no artwork we set the title on a quiet, dark tone instead of
// showing a broken image. The tone is stable per title.
const PALETTE: [string, string][] = [
  ["#1f2a2e", "#f4f4f2"],
  ["#3a2a1e", "#f4f4f2"],
  ["#2b2621", "#f4f4f2"],
  ["#2e1e1e", "#f4f4f2"],
  ["#262b22", "#f4f4f2"],
  ["#1e2433", "#f4f4f2"],
  ["#2a1e33", "#f4f4f2"],
  ["#1c2b2b", "#f4f4f2"],
  ["#331e2a", "#f4f4f2"],
];

function hash(s: string): number {
  let h = 7;
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0;
  return h;
}

export function PosterImg({ src, title, year, className }: { src?: string; title: string; year?: number; className?: string }) {
  const [failed, setFailed] = useState(false);
  if (src && !failed)
    return <img src={src} alt="" loading="lazy" decoding="async" onError={() => setFailed(true)} className={cn("size-full object-cover", className)} />;
  const h = hash(title);
  const [bg, fg] = PALETTE[h % PALETTE.length];
  return (
    <div className={cn("@container relative flex size-full flex-col justify-between overflow-hidden p-[7%]", className)} style={{ background: bg, color: fg }}>
      <span className="relative font-mono text-[10px] tracking-wider opacity-70 @max-[60px]:hidden">{year ?? ""}</span>
      <span className="relative text-[clamp(10px,12cqw,28px)] leading-[1.02] font-semibold tracking-[-0.04em] text-balance @max-[60px]:hidden">{title}</span>
    </div>
  );
}

export function PosterCard({
  to,
  poster,
  title,
  year,
  sub,
  corner,
  progress,
}: {
  to: string;
  poster?: string;
  title: string;
  year?: number;
  sub?: ReactNode;
  corner?: ReactNode;
  progress?: number;
}) {
  return (
    <Link to={to} className="group block min-w-0 outline-none">
      <div className="relative aspect-[2/3] overflow-hidden rounded-lg bg-muted ring-ring/50 transition group-hover:-translate-y-0.5 group-focus-visible:ring-[3px]">
        <PosterImg src={poster} title={title} year={year} className="transition duration-300 group-hover:scale-[1.02]" />
        {corner && <div className="absolute top-2 right-2 rounded-full shadow-[0_0_0_1px_rgb(26_28_28/0.22)]">{corner}</div>}
        {progress !== undefined && progress > 0 && progress < 1 && (
          <div className="absolute inset-x-2 bottom-2 h-1 overflow-hidden rounded-full bg-ink/25">
            <i className="block h-full rounded-full bg-lime" style={{ width: `${progress * 100}%` }} />
          </div>
        )}
      </div>
      <div className="mt-2.5 truncate font-semibold tracking-tight" title={title}>
        {title}
      </div>
      {sub && <div className="truncate font-mono text-[11px] tracking-wide text-muted-foreground uppercase">{sub}</div>}
    </Link>
  );
}

/** Small square-ish thumbnail for list rows. */
export function Thumb({ src, title, className }: { src?: string; title: string; className?: string }) {
  return (
    <div className={cn("aspect-[2/3] w-12 shrink-0 overflow-hidden rounded-md bg-muted", className)}>
      <PosterImg src={src} title={title} />
    </div>
  );
}
