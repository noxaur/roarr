import type { Kind, LibraryKeepers } from "./api";

export const ARR_KEEPERS: LibraryKeepers = { tv: "sonarr", movie: "radarr" };

const NAMES = { roarr: "roarr", sonarr: "Sonarr", radarr: "Radarr" };

/** Who keeps titles of this kind, as the name to show. */
export const appName = (keepers: LibraryKeepers, kind: Kind) => NAMES[keepers[kind]];

/** The distinct apps that keep these kinds, as one phrase: "Sonarr and Radarr", or "roarr" when it keeps both. */
export const appNames = (keepers: LibraryKeepers, kinds: Kind[] = ["tv", "movie"]) => [...new Set(kinds.map((k) => appName(keepers, k)))].join(" and ");
