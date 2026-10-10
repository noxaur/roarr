import { useEffect, useState } from "react";
import { api, type RequestItem } from "../lib/api";
import { Link } from "../lib/router";
import { useToast } from "../components/Toasts";
import { PosterImg } from "../components/Poster";
import { cls } from "../lib/format";

const FILTERS = ["pending", "approved", "all"] as const;

export function RequestsPage() {
  const [filter, setFilter] = useState<(typeof FILTERS)[number]>("pending");
  const [items, setItems] = useState<RequestItem[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const toast = useToast();

  const load = () =>
    api
      .requests(filter)
      .then((r) => {
        setItems(r.items);
        setError(null);
      })
      .catch((e) => setError(String(e.message ?? e)));
  useEffect(() => {
    setItems(null);
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filter]);

  const act = async (r: RequestItem, action: "approve" | "decline") => {
    try {
      await api.requestAction(r.id, action);
      toast(`${action === "approve" ? "Approved" : "Declined"} ${r.title}`, "ok");
      load();
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), "bad");
    }
  };

  const href = (r: RequestItem) => {
    if (r.inLibrary && r.libraryId) return r.kind === "tv" ? `/tv/${r.libraryId}` : `/movie/${r.libraryId}`;
    return r.kind === "tv" ? `/tv/tvdb/${r.tvdbId ?? 0}?tmdb=${r.tmdbId}` : `/movie/tmdb/${r.tmdbId}`;
  };

  return (
    <>
      <div className="section-title">
        Requests
        <span className="spacer" />
        <div className="seg" style={{ textTransform: "none", letterSpacing: 0 }}>
          {FILTERS.map((f) => (
            <button key={f} className={cls(filter === f && "on")} onClick={() => setFilter(f)}>
              {f[0].toUpperCase() + f.slice(1)}
            </button>
          ))}
        </div>
      </div>
      {error && <div className="notice warn">{error.includes("not configured") ? "Seerr is not configured. Set SEERR_URL and SEERR_API_KEY to see requests here." : error}</div>}
      {items && items.length === 0 && <div className="empty">No {filter === "all" ? "" : filter} requests.</div>}
      {items && items.length > 0 && (
        <div className="panel">
          {items.map((r) => (
            <div key={r.id} className="dl">
              <div className="thumb">
                <PosterImg src={r.poster} title="" />
              </div>
              <div style={{ minWidth: 0 }}>
                <div className="row wrap">
                  <Link to={href(r)}>
                    <b>{r.title}</b>
                  </Link>
                  {r.year && <span className="muted small">{r.year}</span>}
                  <span className="tag">{r.kind === "tv" ? "Show" : "Movie"}</span>
                  {r.seasons && r.seasons.length > 0 && <span className="tag">Season {r.seasons.join(", ")}</span>}
                  <span className={cls("tag", r.status === "pending" ? "warn" : r.status === "declined" ? "bad" : "good")}>{r.status}</span>
                  {r.available === "available" && <span className="tag good">available</span>}
                  {r.available === "partial" && <span className="tag info">partly available</span>}
                </div>
                <div className="small muted" style={{ marginTop: 4 }}>
                  {r.requestedBy} · {new Date(r.createdAt).toLocaleDateString()}
                </div>
              </div>
              <div className="row" style={{ alignItems: "flex-start" }}>
                <Link to={href(r)} className="btn sm">
                  Open
                </Link>
                {r.status === "pending" && (
                  <>
                    <button className="btn sm primary" onClick={() => act(r, "approve")}>
                      Approve
                    </button>
                    <button className="btn sm ghost" onClick={() => act(r, "decline")}>
                      Decline
                    </button>
                  </>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </>
  );
}
