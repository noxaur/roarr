import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Bookmark, Check, ChevronRight, Download, ListChecks, Radar, RefreshCw, RotateCcw, ShieldCheck, Trash2 } from "lucide-react";
import type { CSSProperties } from "react";
import { api, type EpisodeAction, type EpisodeDetail, type Release, type SeriesDetail, type VerifyRecord } from "@/lib/api";
import { autoGet, run } from "@/lib/actions";
import { bytes, epCode, pad, pct } from "@/lib/format";
import { useGrabber, useLivePlan, type GrabOpts } from "@/lib/releases";
import { useRouter } from "@/lib/router";
import { usePointerSelect } from "@/lib/select";
import { useStore } from "@/lib/store";
import { baseName, jobTag, useConverting } from "@/lib/transcode";
import { forEpisode, isBlocking, isFlagged } from "@/lib/verify";
import { useSearch } from "@/lib/useSearch";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuLabel, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu";
import { DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator } from "@/components/ui/dropdown-menu";
import { Bar, Notice, SameWidth, Spec, State } from "@/components/kit";
import { Checkbox } from "@/components/ui/checkbox";
import { useConfirm } from "@/components/Confirm";
import { ManageMenu, MonitorToggle, RowMenu, SelectionBar } from "@/components/Manage";
import { ReleaseSheet } from "@/components/ReleaseSheet";
import { VerdictBadge, useVerdictActions } from "@/components/Verdict";
import { Crumbs, GetCard, TitleHead, TitleLayout, TitleSkeleton, type GetOption } from "@/components/Title";

export function SeriesPage({ id, tvdb, tmdb }: { id?: number; tvdb?: number; tmdb?: number }) {
  const [d, setD] = useState<SeriesDetail | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [prefs, setPrefs] = useState({ prefetch: true, watchFirst: true });

  const load = useCallback(
    (fresh = false) =>
      (id ? api.series(id, fresh) : api.seriesByTvdb(tvdb ?? 0, tmdb))
        .then((x) => setD({ ...x, seasons: (x.seasons ?? []).map((s) => ({ ...s, episodes: s.episodes ?? [] })) }))
        .catch((e) => setError(String(e.message ?? e))),
    [id, tvdb, tmdb],
  );
  useEffect(() => {
    load();
    api
      .settings()
      .then((s) => setPrefs({ prefetch: s.settings.behaviour.prefetch, watchFirst: s.settings.behaviour.watchFirst }))
      .catch(() => {});
  }, [load]);

  if (error) return <Notice tone="bad">{error}</Notice>;
  if (!d) return <TitleSkeleton />;
  return <SeriesView d={d} reload={() => load(true)} prefs={prefs} />;
}

type Scope = "missing" | "all" | "season" | "custom";

/** What one episode cell shows. */
function epState(e: EpisodeDetail): "have" | "live" | "miss" | "unaired" {
  if (e.hasFile) return "have";
  if (e.queued) return "live";
  if (e.aired) return "miss";
  return "unaired";
}

function SeriesView({ d, reload, prefs }: { d: SeriesDetail; reload: () => void; prefs: { prefetch: boolean; watchFirst: boolean } }) {
  const { reloadActivity, reloadLibrary, verify, activity } = useStore();
  const { navigate, search: params } = useRouter();
  const seasons = useMemo(() => d.seasons.filter((s) => s.episodes.length > 0 || d.synthetic), [d]);
  const allEps = useMemo(() => seasons.flatMap((s) => s.episodes), [seasons]);
  // Check verdicts worth a badge, by episode id.
  const checks = useMemo(() => {
    const m = new Map<number, VerifyRecord>();
    if (!d.inLibrary) return m;
    for (const e of allEps) {
      const r = forEpisode(verify, d.id, e.id);
      if (r && isFlagged(r)) m.set(e.id, r);
    }
    return m;
  }, [verify, allEps, d.id, d.inLibrary]);
  const verdict = useVerdictActions();
  const converting = useConverting("tv", d.inLibrary ? d.id : 0);
  const byId = useMemo(() => new Map(allEps.map((e) => [e.id, e])), [allEps]);
  const missing = useMemo(() => allEps.filter((e) => e.season > 0 && e.aired && !e.hasFile && !e.queued).map((e) => e.id), [allEps]);
  const everything = useMemo(() => allEps.filter((e) => e.season > 0 && e.aired).map((e) => e.id), [allEps]);

  // The season offered as a one-click option: where the gaps start, or the
  // latest season; for shows not in Sonarr yet, the first one.
  const focusSeason = useMemo(() => {
    const regular = seasons.filter((s) => s.number > 0);
    if (!d.inLibrary) return regular[0]?.number ?? seasons[0]?.number ?? 1;
    const gap = missing.length ? byId.get(missing[0])?.season : undefined;
    return gap ?? regular[regular.length - 1]?.number ?? 1;
  }, [seasons, missing, byId, d.inLibrary]);

  const seasonWanted = useCallback(
    (n: number) => {
      const s = seasons.find((x) => x.number === n);
      if (!s) return [];
      const aired = s.episodes.filter((e) => e.aired);
      const miss = aired.filter((e) => !e.hasFile && !e.queued);
      return (miss.length ? miss : aired).map((e) => e.id);
    },
    [seasons],
  );

  const [scope, setScope] = useState<Scope>(() => {
    if (!d.inLibrary) return seasons.filter((s) => s.number > 0).length > 1 ? "season" : "all";
    return missing.length ? "missing" : "custom";
  });
  const [ticked, setTicked] = useState<Set<number>>(new Set());
  const [openSeasons, setOpenSeasons] = useState<Set<number>>(() => new Set(seasons.length === 1 ? [seasons[0].number] : [focusSeason]));
  const [sheet, setSheet] = useState(params.get("pick") === "1");
  const watchFirst = prefs.watchFirst;
  const [busy, setBusy] = useState(false);
  // The "got it" moment: the grabbed cells turn live in one 300 ms sweep, and
  // the button confirms for 2 s.
  const [got, setGot] = useState<{ ids: number[]; label: string } | null>(null);
  const gotTimer = useRef(0);
  const markGot = (ids: number[], what: string) => {
    setGot({ ids, label: `Got ${what}` });
    clearTimeout(gotTimer.current);
    gotTimer.current = window.setTimeout(() => setGot((g) => (g ? { ...g, label: "" } : g)), 2000);
  };
  useEffect(() => () => clearTimeout(gotTimer.current), []);
  const gotSet = useMemo(() => new Set(got?.ids ?? []), [got]);
  // Live progress per episode code, from the download queue.
  const liveOf = useMemo(() => {
    const m = new Map<string, { progress: number; eta?: string }>();
    if (!d.inLibrary) return m;
    for (const a of activity) if (a.href === `/tv/${d.id}` && a.subtitle) m.set(a.subtitle, { progress: a.progress, eta: a.eta });
    return m;
  }, [activity, d.id, d.inLibrary]);
  const confirm = useConfirm();
  const lib = d.inLibrary;
  // Monitoring flips show at once; the reload that follows confirms them.
  const [monOverride, setMonOverride] = useState<Map<number, boolean>>(new Map());
  useEffect(() => setMonOverride(new Map()), [d]);
  const isMon = (e: EpisodeDetail) => monOverride.get(e.id) ?? e.monitored;
  const anchor = useRef<number | null>(null);

  const selArr = useMemo(() => {
    const ids = scope === "missing" ? missing : scope === "all" ? everything : scope === "season" ? seasonWanted(focusSeason) : [...ticked];
    return [...ids].sort((a, b) => a - b);
  }, [scope, missing, everything, seasonWanted, focusSeason, ticked]);
  const sel = useMemo(() => new Set(selArr), [selArr]);
  const selKey = selArr.join(",");

  const ref: Record<string, string> = d.inLibrary ? { id: String(d.id) } : { tvdb: String(d.tvdbId ?? 0), tmdb: String(d.tmdbId ?? 0) };

  // Streams: the whole show (prefetched as soon as the page opens) plus
  // exact episode queries when only a few episodes are selected.
  const streams = useMemo(() => {
    const out: Record<string, string>[] = [];
    if (prefs.prefetch || selArr.length > 3) out.push({ kind: "tv", ...ref, show: "1" });
    if (selArr.length > 0 && selArr.length <= 3) {
      out.push({ kind: "tv", ...ref, episodes: selArr.filter((x) => x > 0).join(",") });
      if (!prefs.prefetch) {
        const ss = [...new Set(selArr.map((x) => byId.get(x)?.season).filter((x) => x !== undefined))];
        out.push({ kind: "tv", ...ref, seasons: ss.join(",") });
      }
    }
    return out.filter((s) => s.episodes !== "");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selKey, prefs.prefetch, d.key]);
  const search = useSearch(d.key, streams);

  const relevant = useCallback(
    (r: Release) => {
      if (!r.covers || r.covers.length === 0) return d.synthetic === true && r.match;
      return r.covers.some((id) => sel.has(id));
    },
    [sel, d.synthetic],
  );
  const plan = useLivePlan(search, relevant, selArr, "tv");

  const later = useRef(0);
  const afterGrab = (seriesId?: number) => {
    reloadActivity();
    clearTimeout(later.current);
    later.current = window.setTimeout(() => {
      if (!d.inLibrary && seriesId) {
        reloadLibrary();
        navigate(`/tv/${seriesId}`, { replace: true });
      } else reload();
    }, 1200);
  };

  const grabber = useGrabber(
    useCallback(
      async (r: Release, opts: GrabOpts) => {
        const fills = (r.covers ?? []).filter((x) => sel.has(x));
        const res = await api.grab({
          kind: "tv",
          seriesId: d.inLibrary ? d.id : 0,
          tvdbId: d.tvdbId,
          episodeIds: fills.length ? fills : selArr,
          release: r,
          force: opts.force,
          watchFirst: opts.watchFirst,
        });
        if (res.ok) afterGrab(res.seriesId);
        return res;
      },
      // eslint-disable-next-line react-hooks/exhaustive-deps
      [selKey, d],
    ),
  );

  const label = (id: number) => {
    const e = byId.get(id);
    if (!e) return `#${id}`;
    return d.anime && e.absolute ? `#${e.absolute}` : epCode(e.season, e.number);
  };

  // One button: grab exactly the plan we are showing once every indexer has
  // answered; until then let the server wait for a stable plan.
  const get = async () => {
    const have = lib ? onDisk(selArr) : [];
    if (have.length) {
      const rest = selArr.filter((id) => !byId.get(id)?.hasFile);
      const all = have.length === selArr.length;
      const a = await confirm({
        title: all ? (have.length === 1 ? `${label(have[0].id)} is already on disk` : "These are already on disk") : `${have.length} of these are already on disk`,
        body: <p>Replacing deletes the current {have.length === 1 ? "file" : `${have.length} files`} ({bytes(sizeOf(have))}) and downloads again.</p>,
        confirm: have.length === 1 ? "Replace it" : `Replace ${have.length}`,
        alt: rest.length ? `Only get the ${rest.length} missing` : undefined,
        danger: true,
      });
      if (!a) return;
      if (a.action === "alt") {
        tickOnly(rest);
        setBusy(true);
        const res = await autoGet({ kind: "tv", seriesId: d.id, episodeIds: rest }, plural(rest.length, "missing episode"));
        setBusy(false);
        if (res) afterGrab();
        return;
      }
      if (!(await run(`Deleting ${describe(have)}…`, () => api.episodes(d.id, "delete", have.map((e) => e.id))))) return;
    }
    setBusy(true);
    try {
      if (search.done && plan.picks.length) {
        const ok = await grabber.grabAll(
          plan.picks.map((x) => x.release),
          { watchFirst },
        );
        if (ok) markGot(selArr, ctaWhat);
      } else {
        const res = await autoGet({ kind: "tv", seriesId: d.inLibrary ? d.id : 0, tvdbId: d.tvdbId, tmdbId: d.tmdbId, episodeIds: selArr }, scopeTitle);
        if (res) {
          markGot(selArr, ctaWhat);
          afterGrab(res.grabs?.find((g) => g.result.ok)?.result.seriesId);
        }
      }
    } finally {
      setBusy(false);
    }
  };

  // Click ticks one episode; shift-click ticks (or unticks) the whole run
  // from the last one clicked, across seasons.
  const toggle = (e: EpisodeDetail, range = false) => {
    const n = new Set(scope === "custom" ? ticked : sel);
    const on = !n.has(e.id);
    let ids = [e.id];
    if (range && anchor.current !== null) {
      const order = allEps.filter((x) => x.aired).map((x) => x.id);
      const i = order.indexOf(anchor.current);
      const j = order.indexOf(e.id);
      if (i >= 0 && j >= 0) ids = order.slice(Math.min(i, j), Math.max(i, j) + 1);
    }
    for (const id of ids) {
      if (on) n.add(id);
      else n.delete(id);
    }
    anchor.current = e.id;
    setTicked(n);
    setScope("custom");
  };
  const tickOnly = (ids: number[]) => {
    setTicked(new Set(ids));
    setScope("custom");
  };
  const clearTicks = useCallback(() => {
    setTicked(new Set());
    anchor.current = null;
  }, []);
  const tickSeason = (num: number) => {
    const ids = seasons.find((s) => s.number === num)?.episodes.filter((e) => e.aired).map((e) => e.id) ?? [];
    const all = ids.every((x) => sel.has(x));
    const n = new Set(scope === "custom" ? ticked : sel);
    for (const x of ids) {
      if (all) n.delete(x);
      else n.add(x);
    }
    setTicked(n);
    setScope("custom");
  };

  // ---- Managing what is on disk -------------------------------------------

  const onDisk = (ids: number[]) => ids.map((id) => byId.get(id)).filter((e): e is EpisodeDetail => !!e?.hasFile);
  const sizeOf = (eps: EpisodeDetail[]) => eps.reduce((n, e) => n + (e.file?.size ?? 0), 0);
  const describe = (eps: EpisodeDetail[]) => (eps.length === 1 ? label(eps[0].id) : plural(eps.length, "episode"));
  const changed = () => {
    reload();
    reloadLibrary();
  };

  const episodesDo = async (action: EpisodeAction, ids: number[], pending: string) => {
    if (await run(pending, () => api.episodes(d.id, action, ids))) changed();
  };

  const setMonitored = (ids: number[], on: boolean) => {
    setMonOverride((m) => new Map([...m, ...ids.map((id) => [id, on] as const)]));
    episodesDo(on ? "monitor" : "unmonitor", ids, on ? "Monitoring…" : "Unmonitoring…");
  };

  const setSeasonMonitored = async (num: number, on: boolean) => {
    const ids = seasons.find((s) => s.number === num)?.episodes.map((e) => e.id) ?? [];
    setMonOverride((m) => new Map([...m, ...ids.map((id) => [id, on] as const)]));
    const ok = await run(
      on ? `Monitoring season ${num}…` : `Unmonitoring season ${num}…`,
      async () => {
        await api.patchTitle("tv", d.id, { monitored: on, season: num });
        return api.episodes(d.id, on ? "monitor" : "unmonitor", ids);
      },
      on ? `Season ${num} monitored` : `Season ${num} unmonitored`,
    );
    if (ok) changed();
  };

  const deleteFiles = async (ids: number[]) => {
    const eps = onDisk(ids);
    if (!eps.length) return;
    const a = await confirm({
      title: `Delete ${describe(eps)} from disk?`,
      body: <p>Frees {bytes(sizeOf(eps))}. Sonarr uses its recycle bin when one is set up; otherwise the files are gone.</p>,
      options: [{ id: "unmonitor", label: "Stop monitoring", hint: "so Sonarr does not download them again", checked: true }],
      confirm: eps.length === 1 ? "Delete file" : `Delete ${eps.length} files`,
      danger: true,
    });
    if (!a) return;
    const ok = await run(`Deleting ${describe(eps)}…`, async () => {
      const res = await api.episodes(d.id, "delete", eps.map((e) => e.id));
      if (a.checked.unmonitor) await api.episodes(d.id, "unmonitor", eps.map((e) => e.id));
      return res;
    });
    if (ok) {
      clearTicks();
      changed();
    }
  };

  // Replace = delete what is there, then get it again: roarr's best pick,
  // or the release sheet to choose from.
  const replace = async (ids: number[], pick = false) => {
    const eps = onDisk(ids);
    if (!eps.length) return;
    const a = await confirm({
      title: `Replace ${describe(eps)}?`,
      body: <p>Deletes the current {eps.length === 1 ? "file" : "files"} ({bytes(sizeOf(eps))}) and downloads again.</p>,
      options: [{ id: "pick", label: "Pick the release manually", checked: pick }],
      confirm: "Replace",
      danger: true,
    });
    if (!a) return;
    if (!(await run(`Deleting ${describe(eps)}…`, () => api.episodes(d.id, "delete", eps.map((e) => e.id))))) return;
    tickOnly(ids);
    if (a.checked.pick) {
      setSheet(true);
      changed();
      return;
    }
    const res = await autoGet({ kind: "tv", seriesId: d.id, episodeIds: ids }, describe(eps));
    if (res) clearTicks();
    afterGrab();
  };

  const getNow = async (ids: number[]) => {
    tickOnly(ids);
    const res = await autoGet({ kind: "tv", seriesId: lib ? d.id : 0, tvdbId: d.tvdbId, tmdbId: d.tmdbId, episodeIds: ids }, ids.length === 1 ? label(ids[0]) : plural(ids.length, "episode"));
    if (res) {
      clearTicks();
      afterGrab(res.grabs?.find((g) => g.result.ok)?.result.seriesId);
    }
  };

  const choose = (ids: number[]) => {
    tickOnly(ids);
    setSheet(true);
  };

  // One episode's actions, shared by its ⋯ menu and its right-click menu.
  const epMenu = (e: EpisodeDetail, C: MenuParts) => {
    const code = label(e.id);
    return (
      <>
        <C.Label className="font-mono text-[11px] font-normal">
          {code}
          {e.file && <span className="mt-1 block break-all text-muted-foreground">{e.file.path}</span>}
        </C.Label>
        {checks.has(e.id) && (
          <>
            <C.Item onSelect={() => verdict.regrab(checks.get(e.id)!)}>
              <RotateCcw /> Re-grab
            </C.Item>
            <C.Item onSelect={() => verdict.recheck(checks.get(e.id)!)}>
              <RefreshCw /> Re-check
            </C.Item>
            <C.Item onSelect={() => verdict.keep(checks.get(e.id)!)}>
              <ShieldCheck /> Keep anyway
            </C.Item>
            <C.Sep />
          </>
        )}
        {e.hasFile ? (
          <>
            <C.Item onSelect={() => replace([e.id])}>
              <RefreshCw /> Replace
            </C.Item>
            <C.Item onSelect={() => replace([e.id], true)}>
              <ListChecks /> Replace, pick release manually
            </C.Item>
          </>
        ) : (
          <>
            <C.Item onSelect={() => getNow([e.id])}>
              <Download /> Get
            </C.Item>
            <C.Item onSelect={() => choose([e.id])}>
              <ListChecks /> Pick release manually
            </C.Item>
          </>
        )}
        {lib && (
          <C.Item onSelect={() => episodesDo("search", [e.id], "Asking Sonarr…")}>
            <Radar /> Sonarr search
          </C.Item>
        )}
        <C.Sep />
        <C.Item onSelect={() => toggle(e)}>
          <Check /> {sel.has(e.id) && scope === "custom" ? "Deselect" : "Select"}
        </C.Item>
        {lib && (
          <C.Item onSelect={() => setMonitored([e.id], !isMon(e))}>
            <Bookmark /> {isMon(e) ? "Unmonitor" : "Monitor"}
          </C.Item>
        )}
        {e.hasFile && (
          <>
            <C.Sep />
            <C.Item variant="destructive" onSelect={() => deleteFiles([e.id])}>
              <Trash2 /> Delete file
            </C.Item>
          </>
        )}
      </>
    );
  };

  // Actions for every ticked episode, when one of several is right-clicked.
  const bulkMenu = (C: MenuParts) => (
    <>
      <C.Label className="text-xs font-normal text-muted-foreground">{plural(tickedArr.length, "episode")} selected</C.Label>
      {tickedMissing.length > 0 && (
        <C.Item onSelect={() => getNow(tickedMissing)}>
          <Download /> Get {plural(tickedMissing.length, "missing episode")}
        </C.Item>
      )}
      <C.Item onSelect={() => setSheet(true)}>
        <ListChecks /> Pick releases manually
      </C.Item>
      {lib && tickedOnDisk.length > 0 && (
        <C.Item onSelect={() => replace(tickedOnDisk.map((e) => e.id))}>
          <RefreshCw /> Replace {plural(tickedOnDisk.length, "file")}
        </C.Item>
      )}
      {lib && (
        <C.Item onSelect={() => setMonitored(tickedArr, !allMon)}>
          <Bookmark /> {allMon ? "Unmonitor" : "Monitor"}
        </C.Item>
      )}
      <C.Item onSelect={clearTicks}>
        <Check /> Deselect all
      </C.Item>
      {lib && tickedOnDisk.length > 0 && (
        <>
          <C.Sep />
          <C.Item variant="destructive" onSelect={() => deleteFiles(tickedArr)}>
            <Trash2 /> Delete {plural(tickedOnDisk.length, "file")}
          </C.Item>
        </>
      )}
    </>
  );

  const tickedArr = scope === "custom" ? selArr : [];
  const tickedOnDisk = onDisk(tickedArr);
  const tickedMissing = tickedArr.filter((id) => {
    const e = byId.get(id);
    return e && !e.hasFile && !e.queued;
  });
  const allMon = tickedArr.every((id) => {
    const e = byId.get(id);
    return e ? isMon(e) : true;
  });

  // Drag down the list (or press and hold, then move) to tick a run of episodes.
  const tickedKeys = useMemo(() => new Set(tickedArr.map(String)), [tickedArr]);
  const onPaint = useCallback((next: Set<string>) => {
    setTicked(new Set([...next].map(Number)));
    setScope("custom");
  }, []);
  const pointer = usePointerSelect({ selected: tickedKeys, onChange: onPaint, paintOnDrag: true, clearOnBlankClick: false });

  const have = allEps.filter((e) => e.season > 0 && e.hasFile).length;
  const n = selArr.length;
  const scopeTitle =
    scope === "all" ? "the whole show" : scope === "missing" ? `${n} missing episode${n === 1 ? "" : "s"}` : n === 1 ? label(selArr[0]) : `${n} episodes of ${d.title}`;
  const sheetTitle = d.title;
  const sheetDetail = n === 1 ? label(selArr[0]) : scope === "season" ? `Season ${focusSeason}, ${n} episodes` : n <= 8 ? selArr.map(label).join(", ") : `${rangeLabel(selArr.map(label))}, ${n} episodes`;
  const regularSeasons = seasons.filter((s) => s.number > 0).length;
  // The release sheet's covers strip: every episode of the seasons the selection touches.
  const sheetCells = useMemo(() => {
    const nums = [...new Set(selArr.map((id) => byId.get(id)?.season).filter((x): x is number => x !== undefined))].sort((a, b) => a - b);
    const tone = { have: "have", live: "live", miss: "miss", unaired: "none" } as const;
    const cells = seasons.filter((s) => nums.includes(s.number)).flatMap((s) => s.episodes.map((e) => ({ id: e.id, tone: tone[epState(e)] })));
    const label = nums.length === 0 ? "" : nums.length === 1 ? `S${pad(nums[0])}` : `S${pad(nums[0])}–S${pad(nums[nums.length - 1])}`;
    return { cells, label };
  }, [selArr, byId, seasons]);
  // What the button names: "S02E08–E10", "Season 2", "the whole show".
  const ctaWhat =
    scope === "all"
      ? "the whole show"
      : scope === "season" && n === seasonWanted(focusSeason).length && n === (seasons.find((s) => s.number === focusSeason)?.episodes.filter((e) => e.aired).length ?? -1)
        ? `Season ${focusSeason}`
        : d.anime
          ? n === 1
            ? label(selArr[0])
            : `${n} episodes`
          : episodeRange(selArr.map((id) => byId.get(id)).filter((e): e is EpisodeDetail => !!e));
  const cta = got?.label && got.ids.join(",") === selKey ? got.label : n === 0 ? "Select episodes" : `Get ${ctaWhat}`;

  const options: GetOption[] = [
    ...(d.inLibrary ? [{ value: "missing", label: "Missing", disabled: missing.length === 0 }] : []),
    ...(regularSeasons > 1 || !d.inLibrary ? [{ value: "season", label: `Season ${focusSeason}` }] : []),
    { value: "all", label: "Whole show" },
    { value: "custom", label: "Selected episodes" },
  ];

  // "2022–present" for a running show, "2015–2022" for an ended one.
  const lastAired = allEps.filter((e) => e.aired && e.airDate).at(-1)?.airDate?.slice(0, 4);
  const years = d.year ? (d.status === "ended" ? (lastAired && lastAired !== String(d.year) ? `${d.year}–${lastAired}` : String(d.year)) : `${d.year}–present`) : undefined;

  return (
    <>
      <Crumbs section="Shows" />
      <TitleLayout
        head={
          <TitleHead
            poster={d.poster}
            title={d.title}
            year={d.year}
            meta={[years, d.anime && "anime", d.runtime && `${d.runtime} min`, lib && d.sizeOnDisk ? bytes(d.sizeOnDisk) : null]}
            state={!d.inLibrary ? <State tone="none">Not in Sonarr</State> : !d.monitored ? <State tone="none">Not monitored</State> : undefined}
            actions={
              lib && (
                <ManageMenu kind="tv" id={d.id} title={d.title} monitored={d.monitored} profileId={d.qualityProfileId} path={d.path} onChanged={changed}>
                  <DropdownMenuItem disabled={have === 0} onSelect={() => deleteFiles(allEps.filter((e) => e.hasFile).map((e) => e.id))}>
                    <Trash2 /> Delete every file
                  </DropdownMenuItem>
                </ManageMenu>
              )
            }
          />
        }
        aside={
          <GetCard
            options={options}
            value={scope}
            onValueChange={(v) => {
              if (v === "custom" && scope !== "custom") setTicked(new Set(sel));
              setScope(v as Scope);
            }}
            cta={cta}
            onGet={get}
            busy={busy}
            disabled={n === 0}
            plan={plan}
            wantedCount={n}
            onChoose={() => setSheet(true)}
          />
        }
        body={
          <div className="select-none" onMouseDown={pointer.onMouseDown}>
            {seasons.map((s) => {
              const aired = s.episodes.filter((e) => e.aired);
              const haveS = aired.filter((e) => e.hasFile).length;
              const isOpen = openSeasons.has(s.number);
              const allTicked = aired.length > 0 && aired.every((e) => sel.has(e.id));
              const seasonName = s.number === 0 ? "Specials" : `Season ${s.number}`;
              const seasonMon = s.episodes.length > 0 ? s.episodes.some(isMon) : s.monitored;
              const seasonMissing = aired.filter((e) => !e.hasFile && !e.queued).map((e) => e.id);
              const seasonSize = s.episodes.reduce((n, e) => n + (e.file?.size ?? 0), 0);
              const format = seasonFormat(s.episodes);
              // The sweep: each grabbed cell lights after its share of 300 ms.
              const gotHere = s.episodes.filter((e) => gotSet.has(e.id));
              return (
                <section key={s.number} className="border-t py-4">
                  <div className="flex min-h-8 items-center gap-2">
                    <button
                      className="flex min-w-0 flex-1 cursor-pointer items-center gap-2 text-left outline-none focus-visible:underline"
                      aria-expanded={isOpen}
                      onClick={() => {
                        const next = new Set(openSeasons);
                        if (next.has(s.number)) next.delete(s.number);
                        else next.add(s.number);
                        setOpenSeasons(next);
                      }}
                    >
                      <ChevronRight className={cn("size-4 shrink-0 text-muted-foreground transition-transform", isOpen && "rotate-90")} />
                      <b className="text-[16px] font-medium whitespace-nowrap">{seasonName}</b>
                      {format && <span className="ml-1 truncate font-mono text-[12px] text-muted-foreground">{format}</span>}
                    </button>
                    {aired.length > 0 && (
                      <Button variant="ghost" size="xs" className="h-8 px-2 text-[13px] font-normal text-muted-foreground" onClick={() => tickSeason(s.number)}>
                        <SameWidth shown={allTicked ? "Deselect" : "Select"} other={allTicked ? "Select" : "Deselect"} />
                      </Button>
                    )}
                    {lib && (
                      <>
                        <MonitorToggle on={seasonMon} label={seasonName} onToggle={() => setSeasonMonitored(s.number, !seasonMon)} />
                        <RowMenu label={`${seasonName} actions`}>
                          <DropdownMenuLabel className="font-mono text-[11px] font-normal text-muted-foreground">
                            {seasonName}
                            {seasonSize ? `, ${bytes(seasonSize)}` : ""}
                          </DropdownMenuLabel>
                          <DropdownMenuItem disabled={aired.length === 0} onSelect={() => tickOnly(aired.map((e) => e.id))}>
                            <ListChecks /> Select only this season
                          </DropdownMenuItem>
                          <DropdownMenuItem disabled={seasonMissing.length === 0} onSelect={() => getNow(seasonMissing)}>
                            <Download /> Get the missing
                          </DropdownMenuItem>
                          <DropdownMenuItem disabled={haveS === 0} onSelect={() => replace(aired.map((e) => e.id))}>
                            <RefreshCw /> Replace the season
                          </DropdownMenuItem>
                          <DropdownMenuItem disabled={seasonMissing.length === 0} onSelect={() => episodesDo("search", seasonMissing, "Asking Sonarr…")}>
                            <Radar /> Sonarr search
                          </DropdownMenuItem>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem variant="destructive" disabled={haveS === 0} onSelect={() => deleteFiles(s.episodes.map((e) => e.id))}>
                            <Trash2 /> Delete season files
                          </DropdownMenuItem>
                        </RowMenu>
                      </>
                    )}
                  </div>

                  {/* The strip: one cell per episode. */}
                  {s.episodes.length > 0 && (
                    <div className="mt-4 flex flex-wrap gap-x-2 gap-y-3 pl-6">
                      {s.episodes.map((e) => {
                        const swept = gotSet.has(e.id);
                        const st = swept && e.aired && !e.hasFile ? "live" : epState(e);
                        const on = sel.has(e.id) && !swept;
                        const code = label(e.id);
                        const rec = checks.get(e.id);
                        const failed = !!rec && isBlocking(rec);
                        const delay = swept ? (gotHere.indexOf(e) / Math.max(1, gotHere.length)) * 300 : 0;
                        return (
                          <button
                            key={e.id}
                            type="button"
                            data-sel={e.aired ? String(e.id) : undefined}
                            disabled={!e.aired}
                            aria-pressed={on}
                            aria-label={`${code} ${e.title}: ${st === "have" ? "on disk" : st === "live" ? "downloading" : st === "miss" ? "missing" : "not aired"}${failed ? ", fails check" : ""}`}
                            title={`${code} ${e.title}${e.file ? `\n${[e.file.quality, e.file.codec, bytes(e.file.size)].filter(Boolean).join(" ")}` : ""}`}
                            onClick={(ev) => {
                              if (pointer.swallowClick()) return;
                              toggle(e, ev.shiftKey);
                            }}
                            style={{ "--sweep": `${delay}ms` } as CSSProperties}
                            className={cn(
                              "cell transition-colors duration-0 [transition-delay:var(--sweep)] focus-visible:ring-2 focus-visible:ring-ring/60 motion-reduce:[transition-delay:0ms]",
                              "after:absolute after:inset-x-0 after:-bottom-[6px] after:h-[2px] after:bg-white after:opacity-0",
                              st === "have" && "bg-have text-black/80",
                              st === "live" && "bg-live text-black/80",
                              st === "miss" && "bg-miss text-black/80",
                              st === "unaired" && "cursor-default text-muted-foreground/60",
                              failed && "bg-bad text-black/80",
                              on && "after:opacity-100",
                            )}
                          >
                            {d.anime && e.absolute ? e.absolute : pad(e.number)}
                          </button>
                        );
                      })}
                    </div>
                  )}

                  {isOpen && s.episodes.length > 0 && (
                    <div className="mt-4 border-t">
                      {s.episodes.map((e) => {
                        const swept = gotSet.has(e.id) && e.aired && !e.hasFile;
                        const on = sel.has(e.id) && !swept;
                        const code = label(e.id);
                        const st = swept ? "live" : epState(e);
                        const live = liveOf.get(epCode(e.season, e.number));
                        return (
                          <ContextMenu key={e.id}>
                            <ContextMenuTrigger asChild disabled={!e.aired}>
                              <div
                                data-sel={e.aired ? String(e.id) : undefined}
                                onClick={(ev) => {
                                  if (pointer.swallowClick()) return;
                                  if (e.aired) toggle(e, ev.shiftKey);
                                }}
                                className={cn(
                                  "group flex h-10 cursor-pointer items-center gap-4 border-b pr-1 pl-2 select-none hover:bg-card",
                                  on && "bg-card",
                                  !e.aired && "cursor-default",
                                )}
                              >
                                <Checkbox checked={on} disabled={!e.aired} tabIndex={-1} aria-label={`Select ${code}`} className="pointer-events-none" />
                                <span className={cn("w-8 shrink-0 font-mono text-[12px] text-muted-foreground tabular", !e.aired && "opacity-60")}>
                                  {e.absolute && d.anime ? `#${e.absolute}` : `E${pad(e.number)}`}
                                </span>
                                <span className={cn("min-w-0 flex-1 truncate text-[14px]", !e.aired && "text-muted-foreground")}>{e.title}</span>
                                {checks.has(e.id) && <VerdictBadge r={checks.get(e.id)!} />}
                                {e.file && converting.get(baseName(e.file.path)) && (
                                  <Spec tone="live" className="hidden sm:inline-flex" title="Converting to AV1">
                                    {jobTag(converting.get(baseName(e.file.path))!)}
                                  </Spec>
                                )}
                                <span className="flex items-center gap-2 font-mono text-[12px] whitespace-nowrap text-muted-foreground tabular">
                                  {st === "have" ? (
                                    e.file ? bytes(e.file.size) : "on disk"
                                  ) : st === "live" ? (
                                    <span className="flex items-center gap-2 text-live">
                                      {live ? (
                                        <>
                                          <Bar value={live.progress} segments={8} className="hidden sm:flex" />
                                          {pct(live.progress)}
                                          {live.eta && <span className="opacity-60">·</span>}
                                          {live.eta}
                                        </>
                                      ) : (
                                        "downloading"
                                      )}
                                    </span>
                                  ) : st === "miss" ? (
                                    <span className="text-miss">missing</span>
                                  ) : (
                                    (e.airDate ?? "TBA")
                                  )}
                                </span>
                                {lib && <MonitorToggle on={isMon(e)} label={code} onToggle={() => setMonitored([e.id], !isMon(e))} />}
                                {e.aired ? (
                                  <RowMenu label={`${code} actions`} className="opacity-0 group-hover:opacity-100 focus-visible:opacity-100 data-[state=open]:opacity-100">
                                    {epMenu(e, DM)}
                                  </RowMenu>
                                ) : (
                                  <span className="w-8 shrink-0" />
                                )}
                              </div>
                            </ContextMenuTrigger>
                            <ContextMenuContent className="w-64">{tickedKeys.has(String(e.id)) && tickedArr.length > 1 ? bulkMenu(CM) : epMenu(e, CM)}</ContextMenuContent>
                          </ContextMenu>
                        );
                      })}
                    </div>
                  )}
                </section>
              );
            })}
          </div>
        }
      />
      <ReleaseSheet
        open={sheet}
        onOpenChange={setSheet}
        title={sheetTitle}
        detail={sheetDetail}
        kind="tv"
        state={search}
        plan={plan}
        wanted={selArr}
        grabber={grabber}
        watchFirst={watchFirst}
        epLabel={label}
        cells={sheetCells.cells}
        cellsLabel={sheetCells.label}
      />
      <SelectionBar count={tickedArr.length} onClear={clearTicks} summary={tickedOnDisk.length > 0 && `${tickedOnDisk.length} on disk, ${bytes(sizeOf(tickedOnDisk))}`}>
        <Button size="sm" className="shrink-0" disabled={!tickedMissing.length} onClick={() => getNow(tickedMissing)}>
          <Download /> Get missing
        </Button>
        <Button variant="ghost" size="sm" className="shrink-0" onClick={() => setSheet(true)}>
          <ListChecks /> Pick manually
        </Button>
        {lib && (
          <>
            <Button variant="ghost" size="sm" className="shrink-0" disabled={!tickedOnDisk.length} onClick={() => replace(tickedOnDisk.map((e) => e.id))}>
              <RefreshCw /> Replace
            </Button>
            <Button variant="ghost" size="sm" className="shrink-0" onClick={() => setMonitored(tickedArr, !allMon)}>
              <SameWidth shown={allMon ? "Unmonitor" : "Monitor"} other={allMon ? "Monitor" : "Unmonitor"} />
            </Button>
            <Button variant="ghost" size="sm" className="shrink-0 text-destructive hover:text-destructive" disabled={!tickedOnDisk.length} onClick={() => deleteFiles(tickedArr)}>
              <Trash2 /> Delete
            </Button>
          </>
        )}
      </SelectionBar>
    </>
  );
}

const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? "" : "s"}`;

/** "S02E08", "S02E08–E10" for a run in one season, else "3 episodes". */
function episodeRange(eps: EpisodeDetail[]): string {
  if (eps.length === 0) return "";
  const a = eps[0];
  const b = eps[eps.length - 1];
  if (eps.length === 1) return epCode(a.season, a.number);
  const run = eps.every((e, i) => e.season === a.season && e.number === a.number + i);
  return run ? `${epCode(a.season, a.number)}–E${pad(b.number)}` : `${eps.length} episodes`;
}

/** The season's usual file format: "WEBDL-1080p · x265". */
function seasonFormat(eps: EpisodeDetail[]): string {
  const n = new Map<string, number>();
  for (const e of eps) {
    if (!e.file) continue;
    const k = [e.file.quality, e.file.codec].filter(Boolean).join(" · ");
    if (k) n.set(k, (n.get(k) ?? 0) + 1);
  }
  return [...n].sort((x, y) => y[1] - x[1])[0]?.[0] ?? "";
}

/** "S02E08–S02E10" for a sorted list of episode codes. */
function rangeLabel(codes: string[]): string {
  if (codes.length === 0) return "";
  if (codes.length === 1) return codes[0];
  return `${codes[0]}–${codes[codes.length - 1]}`;
}

type MenuParts = { Item: typeof DropdownMenuItem | typeof ContextMenuItem; Label: typeof DropdownMenuLabel | typeof ContextMenuLabel; Sep: typeof DropdownMenuSeparator | typeof ContextMenuSeparator };
const DM: MenuParts = { Item: DropdownMenuItem, Label: DropdownMenuLabel, Sep: DropdownMenuSeparator };
const CM: MenuParts = { Item: ContextMenuItem, Label: ContextMenuLabel, Sep: ContextMenuSeparator };
