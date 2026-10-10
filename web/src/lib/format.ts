export function bytes(n: number): string {
  if (!n || n < 0) return "—";
  const u = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let v = n;
  while (v >= 1000 && i < u.length - 1) {
    v /= 1000;
    i++;
  }
  return `${v >= 100 || i === 0 ? v.toFixed(0) : v.toFixed(1)} ${u[i]}`;
}

export function speed(n?: number): string {
  if (!n) return "";
  return `${bytes(n)}/s`;
}

export function age(hours: number): string {
  if (!hours && hours !== 0) return "";
  if (hours < 1) return `${Math.max(1, Math.round(hours * 60))}m`;
  if (hours < 48) return `${Math.round(hours)}h`;
  const d = hours / 24;
  if (d < 60) return `${Math.round(d)}d`;
  const mo = d / 30.4;
  if (mo < 24) return `${Math.round(mo)}mo`;
  return `${(d / 365).toFixed(1)}y`;
}

export function pad(n: number): string {
  return n < 10 ? `0${n}` : `${n}`;
}

export function epCode(season: number, ep: number): string {
  return `S${pad(season)}E${pad(ep)}`;
}

export function pct(p: number): string {
  return `${Math.floor(Math.max(0, Math.min(1, p)) * 100)}%`;
}

export function cls(...xs: (string | false | null | undefined)[]): string {
  return xs.filter(Boolean).join(" ");
}

/** Lowercase ASCII fold used for instant client-side library search. */
export function fold(s: string): string {
  return s
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .toLowerCase()
    .replace(/&/g, " and ")
    .replace(/['’]/g, "")
    .replace(/[^a-z0-9]+/g, " ")
    .trim();
}
