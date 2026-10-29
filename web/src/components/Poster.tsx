import { useState, type ReactNode } from "react";
import { Link } from "@/lib/router";
import { cn } from "@/lib/utils";
import { Bar } from "./kit";

// When there is no artwork we print the title small, like a label on a tape
// spine, on a quiet dark tone instead of showing a broken image. The tone is
// stable per title. The label sits 24 px up so that a download bar (bottom
// 8 px) never covers it; the select box and the pin keep the top corners.
const PALETTE: [string, string][] = [
  ["#1f2a2e", "#c9ccd1"],
  ["#2a241f", "#c9ccd1"],
  ["#24221f", "#c9ccd1"],
  ["#2a1f1f", "#c9ccd1"],
  ["#22261f", "#c9ccd1"],
  ["#1f2330", "#c9ccd1"],
  ["#261f2c", "#c9ccd1"],
  ["#1c2626", "#c9ccd1"],
  ["#2c1f26", "#c9ccd1"],
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
    <div className={cn("@container relative flex size-full flex-col justify-end overflow-hidden p-2 pb-6", className)} style={{ background: bg, color: fg }} data-year={year}>
      <span className="line-clamp-2 text-[8px] leading-3 font-semibold tracking-[0.08em] uppercase @max-[60px]:hidden">{title}</span>
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
      <div className="relative aspect-[2/3] overflow-hidden rounded-[2px] bg-muted ring-ring/60 group-focus-visible:ring-2">
        <PosterImg src={poster} title={title} year={year} />
        {corner && <div className="absolute top-2 right-2">{corner}</div>}
        {progress !== undefined && progress > 0 && progress < 1 && <Bar value={progress} segments={12} stretch className="absolute inset-x-2 bottom-2" />}
      </div>
      <div className="mt-2 truncate text-[14px] font-medium" title={title}>
        {title}
      </div>
      {sub && <div className="meta truncate">{sub}</div>}
    </Link>
  );
}

/** Small square-ish thumbnail for list rows. */
export function Thumb({ src, title, className }: { src?: string; title: string; className?: string }) {
  return (
    <div className={cn("aspect-[2/3] w-12 shrink-0 overflow-hidden rounded-[2px] bg-muted", className)}>
      <PosterImg src={src} title={title} />
    </div>
  );
}
