import { useEffect, useState } from "react";
import { api, type ActivityItem, type WatchJob } from "../lib/api";
import { Link } from "../lib/router";
import { useStore } from "../lib/store";
import { useToast } from "../components/Toasts";
import { PosterImg } from "../components/Poster";
import { XIcon } from "../components/Icons";
import { bytes, cls, epCode, pct, speed } from "../lib/format";

export function ActivityPage() {
  const { activity, reloadActivity } = useStore();
  const [errors, setErrors] = useState<Record<string, string>>({});
  const toast = useToast();

  // Faster refresh while this page is open.
  useEffect(() => {
    const t = setInterval(() => {
      reloadActivity();
      api
        .activity()
        .then((r) => setErrors(r.errors))
        .catch(() => {});
    }, 2000);
    return () => clearInterval(t);
  }, [reloadActivity]);

  const remove = async (a: ActivityItem, blocklist: boolean) => {
    if (!confirm(`${blocklist ? "Remove and blocklist" : "Remove"} “${a.release}”? The download is deleted from the client.`)) return;
    try {
      await api.removeActivity(a.id, blocklist);
      toast("Removed", "ok");
      reloadActivity();
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), "bad");
    }
  };

  return (
    <>
      <div className="section-title">Activity</div>
      {Object.entries(errors).map(([k, v]) => (
        <div key={k} className="notice warn" style={{ marginBottom: 10 }}>
          {k}: {v}
        </div>
      ))}
      {activity.length === 0 ? (
        <div className="empty">Nothing downloading.</div>
      ) : (
        <div className="panel">
          {activity.map((a) => (
            <div key={a.id} className="dl">
              <div className="thumb">
                <PosterImg src={a.poster} title="" />
              </div>
              <div style={{ minWidth: 0 }}>
                <div className="row">
                  {a.href ? (
                    <Link to={a.href}>
                      <b>{a.title}</b>
                    </Link>
                  ) : (
                    <b>{a.title}</b>
                  )}
                  <span className="muted small">{a.subtitle}</span>
                  <StatusTag s={a.status} />
                  {a.watch && <span className="tag good">watch-first</span>}
                </div>
                <div className="small faint ellipsis" title={a.release}>
                  {a.release}
                </div>
                <div className="row" style={{ marginTop: 8 }}>
                  <div className={cls("bar grow", a.status === "warning" && "warn", a.status === "failed" && "bad")}>
                    <i style={{ width: pct(a.progress) }} />
                  </div>
                  <span className="mono small" style={{ width: 40, textAlign: "right" }}>
                    {pct(a.progress)}
                  </span>
                </div>
                <div className="small muted mono" style={{ marginTop: 4 }}>
                  {a.size > 0 ? `${bytes(a.size - a.sizeLeft)} / ${bytes(a.size)}` : "size unknown yet"}
                  {a.speed ? ` · ${speed(a.speed)}` : ""}
                  {a.eta && a.status === "downloading" ? ` · ${a.eta} left` : ""}
                </div>
                {a.messages?.map((m, i) => (
                  <div key={i} className="small" style={{ color: "var(--warn)", marginTop: 4 }}>
                    {m}
                  </div>
                ))}
                {a.watch && <WatchStrip w={a.watch} />}
              </div>
              <div className="row" style={{ alignItems: "flex-start" }}>
                <button className="btn ghost xs" title="Remove from client" onClick={() => remove(a, false)}>
                  <XIcon width={12} height={12} />
                </button>
                <button className="btn ghost xs" title="Remove and never grab this release again" onClick={() => remove(a, true)}>
                  Blocklist
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </>
  );
}

function StatusTag({ s }: { s: string }) {
  const tone = s === "downloading" ? "good" : s === "importing" ? "info" : s === "warning" ? "warn" : s === "failed" ? "bad" : "";
  return <span className={cls("tag", tone)}>{s}</span>;
}

function WatchStrip({ w }: { w: WatchJob }) {
  const files = w.files ?? [];
  const focus = files.find((f) => !f.skipped && f.progress < 1);
  const imported = files.filter((f) => f.imported).length;
  return (
    <div>
      <div className="eps">
        {files.map((f) => (
          <span
            key={f.index}
            className={cls("ep-chip", f.imported && "done", f.skipped && "skip", f.error && "err", focus?.index === f.index && "focus")}
            title={f.error ?? f.name}
          >
            {epCode(f.season, f.episode).replace(/^S\d+/, "")}
            {!f.imported && !f.skipped && f.progress > 0 && f.progress < 1 && <i style={{ width: pct(f.progress) }} />}
          </span>
        ))}
      </div>
      <div className="tiny muted" style={{ marginTop: 5 }}>
        {files.length === 0 ? "waiting for the torrent's file list" : imported > 0 ? `${imported} episode${imported > 1 ? "s" : ""} already in your library` : "first episode is downloading first"}
        {w.message ? ` · ${w.message}` : ""}
      </div>
    </div>
  );
}
