import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Bookmark, Check, ChevronRight, Download, ListChecks, Radar, RefreshCw, Trash2 } from "lucide-react";
import { api, type EpisodeAction, type EpisodeDetail, type Release, type SeriesDetail } from "@/lib/api";
import { autoGet, run } from "@/lib/actions";
import { bytes, epCode, pad } from "@/lib/format";
import { useGrabber, useLivePlan, type GrabOpts } from "@/lib/releases";
import { useRouter } from "@/lib/router";
import { usePointerSelect } from "@/lib/select";
import { useStore } from "@/lib/store";
import { useSearch } from "@/lib/useSearch";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuLabel, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu";
import { DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator } from "@/components/ui/dropdown-menu";
import { Dot, Figure, Notice, State } from "@/components/kit";
import { useConfirm } from "@/components/Confirm";
import { FileLine, ManageMenu, MonitorToggle, RowMenu, SelectionBar } from "@/components/Manage";
import { ReleaseSheet } from "@/components/ReleaseSheet";
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
  const { reloadActivity, reloadLibrary } = useStore();
  const { navigate, search: params } = useRouter();
  const seasons = useMemo(() => d.seasons.filter((s) => s.episodes.length > 0 || d.synthetic), [d]);
  const allEps = useMemo(() => seasons.flatMap((s) => s.episodes), [seasons]);
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
  const [watchFirst, setWatchFirst] = useState(prefs.watchFirst);
  const [busy, setBusy] = useState(false);
  const [gotFor, setGotFor] = useState<string | null>(null);
  useEffect(() => setWatchFirst(prefs.watchFirst), [prefs.watchFirst]);
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
        if (ok) setGotFor(selKey);
      } else {
        const res = await autoGet({ kind: "tv", seriesId: d.inLibrary ? d.id : 0, tvdbId: d.tvdbId, tmdbId: d.tmdbId, episodeIds: selArr }, scopeTitle);
        if (res) {
          setGotFor(selKey);
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
      options: [{ id: "pick", label: "Choose the release myself", checked: pick }],
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
          {e.file && <span className="mt-0.5 block break-all text-muted-foreground">{e.file.path}</span>}
        </C.Label>
        {e.hasFile ? (
          <>
            <C.Item onSelect={() => replace([e.id])}>
              <RefreshCw /> Replace
            </C.Item>
            <C.Item onSelect={() => replace([e.id], true)}>
              <ListChecks /> Replace, choose release
            </C.Item>
          </>
        ) : (
          <>
            <C.Item onSelect={() => getNow([e.id])}>
              <Download /> Get
            </C.Item>
            <C.Item onSelect={() => choose([e.id])}>
              <ListChecks /> Choose release
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
          <Check /> {sel.has(e.id) && scope === "custom" ? "Untick" : "Tick"}
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
      <C.Label className="text-xs font-normal text-muted-foreground">{plural(tickedArr.length, "episode")} ticked</C.Label>
      {tickedMissing.length > 0 && (
        <C.Item onSelect={() => getNow(tickedMissing)}>
          <Download /> Get {plural(tickedMissing.length, "missing episode")}
        </C.Item>
      )}
      <C.Item onSelect={() => setSheet(true)}>
        <ListChecks /> Choose releases
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
        <Check /> Untick all
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
  const queued = allEps.filter((e) => e.season > 0 && e.queued).length;
  const n = selArr.length;
  const scopeTitle =
    scope === "all" ? "the whole show" : scope === "missing" ? `${n} missing episode${n === 1 ? "" : "s"}` : n === 1 ? label(selArr[0]) : `${n} episodes of ${d.title}`;
  const sheetTitle = d.title;
  const sheetDetail = n === 1 ? label(selArr[0]) : scope === "season" ? `Season ${focusSeason}, ${n} episodes` : n <= 8 ? selArr.map(label).join(", ") : `${rangeLabel(selArr.map(label))}, ${n} episodes`;
  const focusAired = seasons.find((s) => s.number === focusSeason)?.episodes.filter((e) => e.aired) ?? [];
  const focusMissing = focusAired.filter((e) => !e.hasFile && !e.queued).length;
  const regularSeasons = seasons.filter((s) => s.number > 0).length;

  const options: GetOption[] = [
    ...(d.inLibrary ? [{ value: "missing", label: "Missing", sub: missing.length ? rangeLabel(missing.map(label)) : "nothing missing", count: missing.length, disabled: missing.length === 0 }] : []),
    ...(regularSeasons > 1 || !d.inLibrary
      ? [
          {
            value: "season",
            label: `Season ${focusSeason}`,
            sub: d.inLibrary && focusMissing ? `${focusMissing} missing of ${focusAired.length}` : `${focusAired.length} episodes`,
            count: seasonWanted(focusSeason).length,
          },
        ]
      : []),
    { value: "all", label: "Whole show", sub: `${regularSeasons} season${regularSeasons === 1 ? "" : "s"}`, count: everything.length },
    { value: "custom", label: "Ticked episodes", sub: "click cells below", count: scope === "custom" ? ticked.size || "–" : "–" },
  ];

  return (
    <>
      <Crumbs section="Shows" />
      <TitleLayout
        head={
          <TitleHead
            poster={d.poster}
            title={d.title}
            year={d.year}
            meta={[d.year && `${d.year}–`, d.network, d.status, d.anime && "anime", d.runtime && `${d.runtime} min`]}
            state={
              d.inLibrary ? (
                <>
                  <Figure value={`${have}/${everything.length}`} label="on disk" tone="have" />
                  {queued > 0 && <Figure value={queued} label="downloading" tone="live" />}
                  {missing.length > 0 && <Figure value={missing.length} label="missing" tone="miss" />}
                  {d.sizeOnDisk ? <Figure value={bytes(d.sizeOnDisk)} label="" /> : null}
                  {!d.monitored && <State tone="none">Not monitored</State>}
                </>
              ) : (
                <State tone="none">Not in Sonarr. Added when you get something.</State>
              )
            }
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
            cta={n === 0 ? "Tick episodes" : n === 1 ? `Get ${label(selArr[0])}` : `Get ${n} episodes`}
            onGet={get}
            busy={busy}
            done={gotFor === selKey}
            disabled={n === 0}
            state={search}
            plan={plan}
            wantedCount={n}
            onChoose={() => setSheet(true)}
            watchFirst={watchFirst}
            onWatchFirst={setWatchFirst}
            note={lib && onDisk(selArr).length > 0 ? `${plural(onDisk(selArr).length, "episode")} of these already on disk.` : undefined}
          />
        }
        body={
          <div className="select-none" onMouseDown={pointer.onMouseDown}>
            <div className="mb-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-[12px] text-muted-foreground">
              <span className="inline-flex items-center gap-1.5">
                <i className="size-2.5 rounded-[3px] bg-have" /> on disk
              </span>
              <span className="inline-flex items-center gap-1.5">
                <i className="size-2.5 rounded-[3px] bg-live" /> downloading
              </span>
              <span className="inline-flex items-center gap-1.5">
                <i className="size-2.5 rounded-[3px] bg-miss-soft ring-1 ring-miss/60 ring-inset" /> missing
              </span>
              <span className="inline-flex items-center gap-1.5">
                <i className="size-2.5 rounded-[3px] ring-1 ring-border ring-inset" /> not aired
              </span>
              <span className="ml-auto hidden sm:inline">Click a cell to tick, shift-click for a run.</span>
            </div>
            {seasons.map((s) => {
              const aired = s.episodes.filter((e) => e.aired);
              const haveS = aired.filter((e) => e.hasFile).length;
              const queuedS = aired.filter((e) => !e.hasFile && e.queued).length;
              const isOpen = openSeasons.has(s.number);
              const allTicked = aired.length > 0 && aired.every((e) => sel.has(e.id));
              const seasonName = s.number === 0 ? "Specials" : `Season ${s.number}`;
              const seasonMon = s.episodes.length > 0 ? s.episodes.some(isMon) : s.monitored;
              const seasonMissing = aired.filter((e) => !e.hasFile && !e.queued).map((e) => e.id);
              const seasonSize = s.episodes.reduce((n, e) => n + (e.file?.size ?? 0), 0);
              return (
                <section key={s.number} className="border-t py-3">
                  <div className="flex min-h-9 items-center gap-3">
                    <button
                      className="flex min-w-0 flex-1 cursor-pointer items-center gap-2.5 py-1 text-left outline-none focus-visible:underline"
                      aria-expanded={isOpen}
                      onClick={() => {
                        const next = new Set(openSeasons);
                        if (next.has(s.number)) next.delete(s.number);
                        else next.add(s.number);
                        setOpenSeasons(next);
                      }}
                    >
                      <ChevronRight className={cn("size-4 shrink-0 text-muted-foreground transition-transform", isOpen && "rotate-90")} />
                      <b className="text-[15px] font-medium whitespace-nowrap">{seasonName}</b>
                      {!d.synthetic && aired.length > 0 ? (
                        <span className="flex items-center gap-x-3 font-mono text-[12px] whitespace-nowrap text-muted-foreground tabular">
                          <span className={cn(haveS === aired.length && "text-have")}>
                            {haveS}/{aired.length}
                          </span>
                          {queuedS > 0 && (
                            <span className="text-live">
                              {queuedS}
                              <span className="hidden sm:inline"> downloading</span>
                            </span>
                          )}
                          {seasonMissing.length > 0 && (
                            <span className="text-miss">
                              {seasonMissing.length}
                              <span className="hidden sm:inline"> missing</span>
                            </span>
                          )}
                          {seasonSize > 0 && <span className="hidden md:inline">{bytes(seasonSize)}</span>}
                        </span>
                      ) : (
                        <span className="font-mono text-[12px] text-muted-foreground">{s.episodes.length} eps</span>
                      )}
                    </button>
                    {aired.length > 0 && (
                      <Button variant="ghost" size="xs" className="text-muted-foreground" onClick={() => tickSeason(s.number)}>
                        {allTicked ? "Untick" : "Tick season"}
                      </Button>
                    )}
                    {lib && (
                      <>
                        <MonitorToggle on={seasonMon} label={seasonName} onToggle={() => setSeasonMonitored(s.number, !seasonMon)} />
                        <RowMenu label={`${seasonName} actions`}>
                          <DropdownMenuLabel className="font-mono text-[11px] font-normal text-muted-foreground">
                            {seasonName}, {haveS}/{aired.length} on disk{seasonSize ? `, ${bytes(seasonSize)}` : ""}
                          </DropdownMenuLabel>
                          <DropdownMenuItem disabled={aired.length === 0} onSelect={() => tickOnly(aired.map((e) => e.id))}>
                            <ListChecks /> Tick only this season
                          </DropdownMenuItem>
                          <DropdownMenuItem disabled={seasonMissing.length === 0} onSelect={() => getNow(seasonMissing)}>
                            <Download /> Get the {seasonMissing.length || ""} missing
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
                    <div className="mt-1.5 grid gap-1 pl-[26px]" style={{ gridTemplateColumns: "repeat(auto-fill, minmax(26px, 1fr))" }}>
                      {s.episodes.map((e) => {
                        const st = epState(e);
                        const on = sel.has(e.id);
                        const code = label(e.id);
                        return (
                          <button
                            key={e.id}
                            type="button"
                            data-sel={e.aired ? String(e.id) : undefined}
                            disabled={!e.aired}
                            aria-pressed={on}
                            aria-label={`${code} ${e.title}: ${st === "have" ? "on disk" : st === "live" ? "downloading" : st === "miss" ? "missing" : "not aired"}`}
                            title={`${code} ${e.title}${e.file ? `\n${[e.file.quality, e.file.codec, bytes(e.file.size)].filter(Boolean).join(" ")}` : ""}`}
                            onClick={(ev) => {
                              if (pointer.swallowClick()) return;
                              toggle(e, ev.shiftKey);
                            }}
                            className={cn(
                              "cell focus-visible:ring-2 focus-visible:ring-ring",
                              st === "have" && "bg-have text-background",
                              st === "live" && "bg-live text-background",
                              st === "miss" && "bg-miss-soft text-miss ring-1 ring-miss/60 ring-inset",
                              st === "unaired" && "cursor-default bg-transparent text-muted-foreground/60 ring-1 ring-border ring-inset",
                              on && "shadow-[0_0_0_2px_var(--background),0_0_0_4px_var(--foreground)]",
                            )}
                          >
                            {d.anime && e.absolute ? e.absolute : pad(e.number)}
                          </button>
                        );
                      })}
                    </div>
                  )}

                  {isOpen && s.episodes.length > 0 && (
                    <div className="mt-3 border-t">
                      {s.episodes.map((e) => {
                        const on = sel.has(e.id);
                        const code = label(e.id);
                        const st = epState(e);
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
                                  "group flex min-h-10 cursor-pointer items-center gap-3 border-b px-1.5 py-1 select-none hover:bg-card",
                                  on && "bg-live-soft/60 hover:bg-live-soft",
                                  !e.aired && "cursor-default",
                                )}
                              >
                                <Dot tone={st === "have" ? "have" : st === "live" ? "live" : st === "miss" ? "miss" : "none"} />
                                <span className={cn("w-9 shrink-0 font-mono text-[12px] text-muted-foreground tabular", !e.aired && "opacity-55")}>
                                  {e.absolute && d.anime ? `#${e.absolute}` : `E${pad(e.number)}`}
                                </span>
                                <span className={cn("min-w-0 flex-1", !e.aired && "opacity-55")}>
                                  <span className="block truncate text-[13.5px]">{e.title}</span>
                                  {e.file && <FileLine file={e.file} className="hidden sm:block" />}
                                </span>
                                <span className="font-mono text-[11.5px] whitespace-nowrap text-muted-foreground tabular">
                                  {st === "have" ? (e.file ? bytes(e.file.size) : "on disk") : st === "live" ? <span className="text-live">downloading</span> : st === "miss" ? <span className="text-miss">missing</span> : (e.airDate ?? "TBA")}
                                </span>
                                {lib && <MonitorToggle on={isMon(e)} label={code} onToggle={() => setMonitored([e.id], !isMon(e))} />}
                                {e.aired ? (
                                  <RowMenu label={`${code} actions`} className="opacity-60 group-hover:opacity-100 focus-visible:opacity-100">
                                    {epMenu(e, DM)}
                                  </RowMenu>
                                ) : (
                                  <span className="w-7 shrink-0" />
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
      />
      <SelectionBar count={tickedArr.length} onClear={clearTicks} summary={tickedOnDisk.length > 0 && `${tickedOnDisk.length} on disk, ${bytes(sizeOf(tickedOnDisk))}`}>
        {tickedMissing.length > 0 && (
          <Button size="sm" className="shrink-0" onClick={() => getNow(tickedMissing)}>
            <Download /> Get {tickedMissing.length === tickedArr.length ? "" : `${tickedMissing.length} missing`}
          </Button>
        )}
        <Button variant="ghost" size="sm" className="shrink-0" onClick={() => setSheet(true)}>
          <ListChecks /> Choose
        </Button>
        {lib && tickedOnDisk.length > 0 && (
          <Button variant="ghost" size="sm" className="shrink-0" onClick={() => replace(tickedOnDisk.map((e) => e.id))}>
            <RefreshCw /> Replace{tickedOnDisk.length < tickedArr.length ? ` ${tickedOnDisk.length}` : ""}
          </Button>
        )}
        {lib && (
          <Button variant="ghost" size="sm" className="shrink-0" onClick={() => setMonitored(tickedArr, !allMon)}>
            {allMon ? "Unmonitor" : "Monitor"}
          </Button>
        )}
        {lib && tickedOnDisk.length > 0 && (
          <Button variant="ghost" size="sm" className="shrink-0 text-destructive hover:text-destructive" onClick={() => deleteFiles(tickedArr)}>
            <Trash2 /> Delete{tickedOnDisk.length < tickedArr.length ? ` ${tickedOnDisk.length}` : ""}
          </Button>
        )}
      </SelectionBar>
    </>
  );
}

const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? "" : "s"}`;

/** "S02E08–S02E10" for a sorted list of episode codes. */
function rangeLabel(codes: string[]): string {
  if (codes.length === 0) return "";
  if (codes.length === 1) return codes[0];
  return `${codes[0]}–${codes[codes.length - 1]}`;
}

type MenuParts = { Item: typeof DropdownMenuItem | typeof ContextMenuItem; Label: typeof DropdownMenuLabel | typeof ContextMenuLabel; Sep: typeof DropdownMenuSeparator | typeof ContextMenuSeparator };
const DM: MenuParts = { Item: DropdownMenuItem, Label: DropdownMenuLabel, Sep: DropdownMenuSeparator };
const CM: MenuParts = { Item: ContextMenuItem, Label: ContextMenuLabel, Sep: ContextMenuSeparator };
