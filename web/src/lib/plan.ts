// Port of internal/release/plan.go so the "best plan" can update live as
// indexers answer, without a round trip. Keep the constants in sync.

export interface Option {
  key: string;
  score: number;
  rejected: boolean;
  covers: number[];
}

export interface Pick {
  key: string;
  fills: number[];
  extras: number;
}

export interface Plan {
  picks: Pick[];
  missing: number[];
}

const PER_GRAB = 8.0;
const WASTE_FACTOR = 0.35;

export function planEpisodes(wanted: number[], opts: Option[]): Plan {
  const remaining = new Set(wanted);
  const want = new Set(wanted);
  const used = new Set<string>();
  const picks: Pick[] = [];
  while (remaining.size > 0) {
    let best = -1;
    let bestRate = 0;
    let bestFills: number[] = [];
    let bestExtras = 0;
    for (let i = 0; i < opts.length; i++) {
      const o = opts[i];
      if (o.rejected || used.has(o.key) || o.score <= 0) continue;
      const fills: number[] = [];
      let extras = 0;
      for (const id of o.covers) {
        if (remaining.has(id)) fills.push(id);
        else if (!want.has(id)) extras++;
      }
      if (fills.length === 0) continue;
      const n = fills.length;
      const rate = (o.score * n - WASTE_FACTOR * o.score * extras - PER_GRAB) / n;
      if (best < 0 || rate > bestRate || (rate === bestRate && fills.length > bestFills.length)) {
        best = i;
        bestRate = rate;
        bestFills = fills;
        bestExtras = extras;
      }
    }
    if (best < 0 || bestRate <= 0) break;
    used.add(opts[best].key);
    for (const id of bestFills) remaining.delete(id);
    picks.push({ key: opts[best].key, fills: bestFills, extras: bestExtras });
  }
  return { picks, missing: [...remaining].sort((a, b) => a - b) };
}
