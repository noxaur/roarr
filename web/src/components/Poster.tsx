import { useState, type ReactNode } from "react";
import { Link } from "../lib/router";

export function PosterImg({ src, title }: { src?: string; title: string }) {
  const [failed, setFailed] = useState(false);
  return (
    <>
      {src && !failed ? <img src={src} alt="" loading="lazy" decoding="async" onError={() => setFailed(true)} /> : <div className="ph">{title}</div>}
    </>
  );
}

export function PosterCard({
  to,
  poster,
  title,
  sub,
  corner,
  progress,
}: {
  to: string;
  poster?: string;
  title: string;
  sub?: ReactNode;
  corner?: ReactNode;
  progress?: number;
}) {
  return (
    <Link to={to} className="poster-card">
      <div className="poster">
        <PosterImg src={poster} title={title} />
        {corner && <div className="corner">{corner}</div>}
        {progress !== undefined && progress > 0 && progress < 1 && (
          <div className="bar">
            <i style={{ width: `${progress * 100}%` }} />
          </div>
        )}
      </div>
      <div>
        <div className="poster-title ellipsis" title={title}>
          {title}
        </div>
        {sub && <div className="small muted ellipsis">{sub}</div>}
      </div>
    </Link>
  );
}
