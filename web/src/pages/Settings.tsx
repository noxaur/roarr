import { useEffect, useState } from "react";
import { api, type ServiceStatus, type Settings } from "../lib/api";
import { useToast } from "../components/Toasts";
import { RefreshIcon } from "../components/Icons";
import { cls } from "../lib/format";

const SERVICES: { key: keyof Settings & ("sonarr" | "radarr" | "prowlarr" | "seerr" | "jellyfin" | "qbittorrent"); name: string; env: string; hint: string; user?: boolean }[] = [
  { key: "prowlarr", name: "Prowlarr", env: "PROWLARR", hint: "Required. All searches go through Prowlarr's indexers." },
  { key: "sonarr", name: "Sonarr", env: "SONARR", hint: "Shows. Found automatically from Prowlarr's app links if left empty." },
  { key: "radarr", name: "Radarr", env: "RADARR", hint: "Movies. Found automatically from Prowlarr's app links if left empty." },
  { key: "seerr", name: "Seerr", env: "SEERR", hint: "Optional. Fast search with posters, and the request inbox." },
  { key: "jellyfin", name: "Jellyfin", env: "JELLYFIN", hint: "Optional. Sign-in, and instant library refresh after imports." },
  { key: "qbittorrent", name: "qBittorrent", env: "QBIT", hint: "Optional. Enables watch-first packs and “download anyway”.", user: true },
];

export function SettingsPage() {
  const [s, setS] = useState<Settings | null>(null);
  const [locked, setLocked] = useState<Record<string, boolean>>({});
  const [status, setStatus] = useState<Record<string, ServiceStatus>>({});
  const [version, setVersion] = useState("");
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const toast = useToast();

  const loadStatus = (fresh = false) =>
    api
      .status(fresh)
      .then((r) => {
        setStatus(r.services);
        setVersion(r.version);
      })
      .catch(() => {});
  useEffect(() => {
    api.settings().then((r) => {
      setS(r.settings);
      setLocked(r.locked);
    });
    loadStatus();
  }, []);

  if (!s) return <div className="skeleton" style={{ height: 300 }} />;

  const update = (f: (x: Settings) => void) => {
    const next = structuredClone(s);
    f(next);
    setS(next);
    setDirty(true);
  };
  const save = async () => {
    setSaving(true);
    try {
      const r = await api.saveSettings(s);
      setS(r.settings);
      setLocked(r.locked);
      setDirty(false);
      toast("Saved. Changes apply immediately.", "ok");
      setTimeout(() => loadStatus(true), 600);
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), "bad");
    } finally {
      setSaving(false);
    }
  };

  const p = s.prefs;
  const b = s.behaviour;
  const resOrder = [2160, 1080, 720, 576, 480];

  return (
    <div style={{ maxWidth: 920 }}>
      <div className="section-title">
        Connections
        <span className="spacer" />
        <button className="btn ghost xs" onClick={() => loadStatus(true)} style={{ textTransform: "none", letterSpacing: 0 }}>
          <RefreshIcon width={12} height={12} /> Re-check
        </button>
      </div>
      <div className="seasons">
        {SERVICES.map((svc) => {
          const st = status[svc.key];
          const cfg = s[svc.key];
          const lockedUrl = locked[`${svc.env}_URL`];
          return (
            <div key={svc.key} className="panel">
              <div className="panel-head">
                <span className={cls("dot", st?.ok ? "ok" : st?.configured ? "bad" : "")} />
                <b>{svc.name}</b>
                <span className="small muted">{st?.ok ? `v${st.version} · ${st.ms}ms` : st?.configured ? st.error : svc.hint}</span>
              </div>
              <div className="panel-body form-grid">
                <label>URL</label>
                <input
                  className="field"
                  placeholder={st?.url && !cfg.url ? `${st.url} (discovered)` : "http://host:port"}
                  disabled={lockedUrl}
                  value={cfg.url}
                  onChange={(e) => update((x) => (x[svc.key].url = e.target.value))}
                />
                {svc.user ? (
                  <>
                    <label>Username</label>
                    <input className="field" disabled={locked[`${svc.env}_USERNAME`]} value={cfg.username ?? ""} onChange={(e) => update((x) => (x[svc.key].username = e.target.value))} />
                    <label>Password</label>
                    <input className="field" type="password" disabled={locked[`${svc.env}_PASSWORD`]} value={cfg.password ?? ""} onChange={(e) => update((x) => (x[svc.key].password = e.target.value))} />
                  </>
                ) : (
                  <>
                    <label>API key</label>
                    <input className="field" type="password" disabled={locked[`${svc.env}_API_KEY`]} value={cfg.apiKey ?? ""} onChange={(e) => update((x) => (x[svc.key].apiKey = e.target.value))} />
                  </>
                )}
              </div>
            </div>
          );
        })}
      </div>
      <div className="small faint" style={{ marginTop: 6 }}>
        Fields greyed out are set by environment variables in your docker-compose file.
      </div>

      <div className="section-title">Picking releases</div>
      <div className="panel panel-body form-grid">
        <label>Resolutions</label>
        <div className="row wrap">
          {p.resolutions.map((r, i) => (
            <span key={r} className="chip on" title="click to remove">
              <span className="mono">{i + 1}.</span> {r === 2160 ? "4K" : `${r}p`}
              <button className="btn ghost xs" style={{ height: 16, padding: 0 }} onClick={() => update((x) => (x.prefs.resolutions = x.prefs.resolutions.filter((y) => y !== r)))}>
                ×
              </button>
            </span>
          ))}
          {resOrder
            .filter((r) => !p.resolutions.includes(r))
            .map((r) => (
              <button key={r} className="chip" onClick={() => update((x) => x.prefs.resolutions.push(r))}>
                + {r === 2160 ? "4K" : `${r}p`}
              </button>
            ))}
        </div>
        <label>Audio language</label>
        <input className="field" value={p.language} onChange={(e) => update((x) => (x.prefs.language = e.target.value))} placeholder="English" />
        <label>Max size</label>
        <div className="row">
          <input className="field" style={{ width: 90 }} type="number" min={0} step={0.5} value={p.maxGbPerHour} onChange={(e) => update((x) => (x.prefs.maxGbPerHour = +e.target.value))} />
          <span className="muted small">GB per hour of video (0 = no limit)</span>
        </div>
        <label>Min seeders</label>
        <input className="field" style={{ width: 90 }} type="number" min={0} value={p.minSeeders} onChange={(e) => update((x) => (x.prefs.minSeeders = +e.target.value))} />
        <label>Prefer</label>
        <div className="row wrap" style={{ gap: 16 }}>
          <label className="check">
            <input type="checkbox" checked={p.preferHevc} onChange={(e) => update((x) => (x.prefs.preferHevc = e.target.checked))} /> HEVC / x265
          </label>
          <label className="check">
            <input type="checkbox" checked={p.preferDualAudio} onChange={(e) => update((x) => (x.prefs.preferDualAudio = e.target.checked))} /> Dual audio
          </label>
          <label className="check">
            <input type="checkbox" checked={p.allowHdr} onChange={(e) => update((x) => (x.prefs.allowHdr = e.target.checked))} /> Allow HDR / DV
          </label>
          <label className="check">
            <input type="checkbox" checked={p.allowRemux} onChange={(e) => update((x) => (x.prefs.allowRemux = e.target.checked))} /> Allow remuxes
          </label>
        </div>
        <label>Preferred groups</label>
        <input className="field" placeholder="e.g. NTb, FLUX" value={(p.preferredGroups ?? []).join(", ")} onChange={(e) => update((x) => (x.prefs.preferredGroups = splitList(e.target.value)))} />
        <label>Blocked words</label>
        <input className="field" placeholder="e.g. HDCAM, YIFY" value={(p.blockedTerms ?? []).join(", ")} onChange={(e) => update((x) => (x.prefs.blockedTerms = splitList(e.target.value)))} />
      </div>

      <div className="section-title">Behaviour</div>
      <div className="panel panel-body form-grid">
        <label>Default mode</label>
        <div className="seg">
          {(["manual", "auto"] as const).map((m) => (
            <button key={m} className={cls(b.defaultMode === m && "on")} onClick={() => update((x) => (x.behaviour.defaultMode = m))}>
              {m === "manual" ? "Manual: I pick" : "Auto: roarr picks"}
            </button>
          ))}
        </div>
        <label>Watch-first packs</label>
        <label className="check">
          <input type="checkbox" checked={b.watchFirst} onChange={(e) => update((x) => (x.behaviour.watchFirst = e.target.checked))} />
          <span className="small muted">Download season packs in episode order and import each episode as soon as it finishes (needs qBittorrent).</span>
        </label>
        <label>Prefetch</label>
        <label className="check">
          <input type="checkbox" checked={b.prefetch} onChange={(e) => update((x) => (x.behaviour.prefetch = e.target.checked))} />
          <span className="small muted">Start searching the whole show as soon as its page opens.</span>
        </label>
        <label>Seerr requests</label>
        <label className="check">
          <input type="checkbox" checked={b.seerrAutoFetch} disabled={locked.SEERR_AUTO_FETCH} onChange={(e) => update((x) => (x.behaviour.seerrAutoFetch = e.target.checked))} />
          <span className="small muted">Fetch approved requests automatically with roarr's search (set Seerr's “prevent search” on its Sonarr/Radarr servers to avoid double grabs).</span>
        </label>
        <label>Indexer timeout</label>
        <div className="row">
          <input className="field" style={{ width: 90 }} type="number" min={5} value={b.indexerTimeout} disabled={locked.INDEXER_TIMEOUT} onChange={(e) => update((x) => (x.behaviour.indexerTimeout = +e.target.value))} />
          <span className="muted small">seconds before a slow indexer is given up on</span>
        </div>
        <label>Result cache</label>
        <div className="row">
          <input className="field" style={{ width: 90 }} type="number" min={1} value={b.cacheMinutes} disabled={locked.CACHE_MINUTES} onChange={(e) => update((x) => (x.behaviour.cacheMinutes = +e.target.value))} />
          <span className="muted small">minutes</span>
        </div>
      </div>

      <div className="section-title">Sign-in</div>
      <div className="panel panel-body form-grid">
        <label>Mode</label>
        <select className="field" style={{ width: 240 }} value={s.authMode} disabled={locked.AUTH_MODE} onChange={(e) => update((x) => (x.authMode = e.target.value))}>
          <option value="jellyfin">Jellyfin accounts</option>
          <option value="password">Single password</option>
          <option value="none">None (trusted network only)</option>
        </select>
        {s.authMode === "jellyfin" && (
          <>
            <label>Admins only</label>
            <label className="check">
              <input type="checkbox" checked={s.jellyfinAdminOnly} disabled={locked.JELLYFIN_ADMIN_ONLY} onChange={(e) => update((x) => (x.jellyfinAdminOnly = e.target.checked))} />
              <span className="small muted">Only Jellyfin administrators can sign in.</span>
            </label>
          </>
        )}
        {s.authMode === "password" && (
          <>
            <label>Password</label>
            <input className="field" type="password" disabled={locked.AUTH_PASSWORD} value={s.authPassword ?? ""} onChange={(e) => update((x) => (x.authPassword = e.target.value))} />
          </>
        )}
      </div>

      <div className="row" style={{ position: "sticky", bottom: 0, padding: "16px 0", background: "linear-gradient(transparent, var(--bg) 40%)" }}>
        <span className="small faint">roarr {version}</span>
        <span className="spacer" />
        <button className="btn primary" disabled={!dirty || saving} onClick={save}>
          {saving ? <span className="spinner" /> : null} Save
        </button>
      </div>
    </div>
  );
}

function splitList(v: string): string[] {
  return v
    .split(",")
    .map((x) => x.trim())
    .filter(Boolean);
}
