/**
 * A strip of at most `max` cells for `total` episodes, grouped evenly when
 * there are more: how many cells to draw and how many of them are on disk.
 * Grouping never rounds a title to empty or to complete: anything on disk
 * lights a cell, and only a complete title lights them all.
 */
export function cellCounts(have: number, total: number, max: number): { cells: number; have: number } {
  total = Math.max(1, total);
  const per = Math.ceil(total / max);
  const cells = Math.ceil(total / per);
  if (have <= 0) return { cells, have: 0 };
  if (have >= total) return { cells, have: cells };
  return { cells, have: Math.max(1, Math.min(cells - 1, Math.floor(have / per))) };
}
