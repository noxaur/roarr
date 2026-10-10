import { useEffect, useRef, useState } from "react";
import { streamSearch, type Batch, type IndexerPlan, type Release } from "./api";

export interface IndexerState {
  id: number;
  name: string;
  queries: number;
  answered: number;
  results: number;
  ms: number;
  cached: boolean;
  errors: string[];
  disabled?: string;
}

export interface SearchState {
  releases: Map<string, Release>;
  indexers: Map<number, IndexerState>;
  done: boolean;
  error?: string;
  startedAt: number;
  firstAt?: number;
  doneAt?: number;
  version: number;
}

const empty = (): SearchState => ({ releases: new Map(), indexers: new Map(), done: false, startedAt: performance.now(), version: 0 });

interface Stream {
  cancel: () => void;
  done: boolean;
}

/**
 * Streams searches for one title. Several streams (the show-wide prefetch,
 * then narrower season/episode queries as the user picks a scope) merge into
 * one result set. Changing `key` starts over; streams that disappear from
 * the list are closed, new ones are opened, unchanged ones keep running.
 */
export function useSearch(key: string, streams: Record<string, string>[], enabled = true): SearchState {
  const [state, setState] = useState<SearchState>(empty);
  const ref = useRef<SearchState>(state);
  const active = useRef(new Map<string, Stream>());
  const keyRef = useRef<string | null>(null);
  const raf = useRef(0);

  const flush = () => {
    raf.current = 0;
    ref.current = { ...ref.current, version: ref.current.version + 1 };
    setState(ref.current);
  };
  const schedule = () => {
    if (!raf.current) raf.current = requestAnimationFrame(flush);
  };
  const settle = () => {
    const all = [...active.current.values()];
    const s = ref.current;
    const done = all.length > 0 && all.every((x) => x.done);
    if (done && !s.done) {
      s.done = true;
      s.doneAt = performance.now();
    } else if (!done && s.done) {
      s.done = false;
      s.doneAt = undefined;
    }
  };

  const streamsKey = JSON.stringify(streams);
  useEffect(() => {
    if (keyRef.current !== key) {
      for (const st of active.current.values()) st.cancel();
      active.current.clear();
      keyRef.current = key;
      ref.current = empty();
      setState(ref.current);
    }
    if (!enabled) return;
    const want = new Map(streams.map((p) => [JSON.stringify(p), p]));
    for (const [k, st] of active.current) {
      if (!want.has(k)) {
        st.cancel();
        active.current.delete(k);
      }
    }
    for (const [k, params] of want) {
      if (active.current.has(k)) continue;
      const st: Stream = { cancel: () => {}, done: false };
      active.current.set(k, st);
      st.cancel = streamSearch(params, (ev) => {
        const s = ref.current;
        switch (ev.type) {
          case "plan":
            for (const p of ev.indexers ?? []) addPlan(s, p);
            break;
          case "batch":
            if (ev.batch) applyBatch(s, ev.batch, ev.releases?.length ?? 0);
            for (const r of ev.releases ?? []) {
              const cur = s.releases.get(r.key);
              if (!cur) s.releases.set(r.key, r);
              else {
                const indexers = Array.from(new Set([...cur.indexers, ...r.indexers]));
                s.releases.set(r.key, r.seeders >= cur.seeders ? { ...r, indexers } : { ...cur, indexers });
              }
            }
            if (!s.firstAt && (ev.releases?.length ?? 0) > 0) s.firstAt = performance.now();
            break;
          case "error":
            s.error = ev.error;
            st.done = true;
            break;
          case "done":
            st.done = true;
            break;
        }
        settle();
        schedule();
      });
    }
    settle();
    schedule();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, streamsKey, enabled]);

  useEffect(
    () => () => {
      // Streams keep running server-side into the cache; we only stop listening.
      for (const st of active.current.values()) st.cancel();
      active.current.clear();
      keyRef.current = null;
      if (raf.current) cancelAnimationFrame(raf.current);
    },
    [],
  );

  return state;
}

function addPlan(s: SearchState, p: IndexerPlan) {
  const cur = s.indexers.get(p.id);
  if (cur) {
    cur.queries += p.queries;
    return;
  }
  s.indexers.set(p.id, { id: p.id, name: p.name, queries: p.queries, answered: 0, results: 0, ms: 0, cached: true, errors: [], disabled: p.disabled });
}

function applyBatch(s: SearchState, b: Batch, n: number) {
  let cur = s.indexers.get(b.indexerId);
  if (!cur) {
    cur = { id: b.indexerId, name: b.indexer, queries: 1, answered: 0, results: 0, ms: 0, cached: true, errors: [] };
    s.indexers.set(b.indexerId, cur);
  }
  cur.answered++;
  cur.results += n;
  cur.ms = Math.max(cur.ms, b.ms);
  cur.cached = cur.cached && b.cached;
  if (b.error) cur.errors.push(b.error);
}
