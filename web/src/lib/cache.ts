// A small in-memory cache for API reads. Pages render what is cached at once
// and revalidate in the background; hovering a link fills the cache before
// the click, so most page changes need no network wait at all.

interface Entry<T> {
  at: number;
  value?: T;
  promise?: Promise<T>;
}

const entries = new Map<string, Entry<unknown>>();
const MAX = 200;

/** The cached value for a key, however old. */
export function peek<T>(key: string): T | undefined {
  return entries.get(key)?.value as T | undefined;
}

/** Milliseconds since the value for a key was stored; Infinity when there is none. */
export function age(key: string): number {
  const e = entries.get(key);
  return e && e.value !== undefined ? Date.now() - e.at : Infinity;
}

/**
 * Resolves the value for a key. A value younger than maxAge comes from the
 * cache; a request already in flight is shared, never repeated.
 */
export function cached<T>(key: string, load: () => Promise<T>, maxAge = 0): Promise<T> {
  const e = entries.get(key) as Entry<T> | undefined;
  if (e?.promise) return e.promise;
  if (e && e.value !== undefined && Date.now() - e.at < maxAge) return Promise.resolve(e.value);
  const promise = load().then(
    (value) => {
      put(key, value);
      return value;
    },
    (err) => {
      const cur = entries.get(key);
      if (cur?.promise === promise) {
        if (cur.value === undefined) entries.delete(key);
        else cur.promise = undefined;
      }
      throw err;
    },
  );
  entries.set(key, { at: e?.at ?? 0, value: e?.value, promise });
  return promise;
}

/** Stores a value, for example the answer to a forced reload. */
export function put<T>(key: string, value: T) {
  entries.delete(key);
  entries.set(key, { at: Date.now(), value });
  // Oldest first: Map keeps insertion order.
  while (entries.size > MAX) entries.delete(entries.keys().next().value!);
}

/** Drops every key that starts with the prefix. */
export function forget(prefix: string) {
  for (const k of [...entries.keys()]) if (k.startsWith(prefix)) entries.delete(k);
}
