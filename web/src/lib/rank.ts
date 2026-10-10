import type { DiscoverItem, LibraryItem } from "./api";
import { fold } from "./format";

/** Ranks library hits: exact > prefix > word prefix > substring. */
export function rankLibrary(items: LibraryItem[], q: string): LibraryItem[] {
  const f = fold(q);
  if (!f) return [];
  const scored: [number, LibraryItem][] = [];
  for (const it of items) {
    let best = 0;
    for (const t of [it.title, ...(it.aliases ?? [])]) {
      const ft = fold(t);
      let s = 0;
      if (ft === f) s = 100;
      else if (ft.startsWith(f)) s = 80;
      else if ((" " + ft).includes(" " + f)) s = 60;
      else if (ft.includes(f)) s = 40;
      else if (f.split(" ").every((w) => ft.includes(w))) s = 25;
      best = Math.max(best, s);
    }
    if (best > 0) scored.push([best, it]);
  }
  scored.sort((a, b) => b[0] - a[0] || a[1].title.localeCompare(b[1].title));
  return scored.map((x) => x[1]);
}

export function libraryHref(it: Pick<LibraryItem, "kind" | "id">): string {
  return it.kind === "tv" ? `/tv/${it.id}` : `/movie/${it.id}`;
}

export function discoverHref(r: DiscoverItem): string {
  if (r.inLibrary && r.libraryId) return r.kind === "tv" ? `/tv/${r.libraryId}` : `/movie/${r.libraryId}`;
  if (r.kind === "movie") return `/movie/tmdb/${r.tmdbId}`;
  return `/tv/tvdb/${r.tvdbId ?? 0}${r.tmdbId ? `?tmdb=${r.tmdbId}` : ""}`;
}
