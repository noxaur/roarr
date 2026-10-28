import type { ReleaseInfo } from "./api";

// What a release says about its languages, as the parser understood it.
// Release names only tag what is unusual, so "nothing tagged" is the normal
// case for English audio; the UI shows that as "EN?" rather than nothing.

const CODE: Record<string, string> = {
  English: "EN",
  French: "FR",
  German: "DE",
  Spanish: "ES",
  Italian: "IT",
  Russian: "RU",
  Portuguese: "PT",
  Polish: "PL",
  Dutch: "NL",
  Swedish: "SV",
  Danish: "DA",
  Norwegian: "NO",
  Finnish: "FI",
  Hindi: "HI",
  Tamil: "TA",
  Telugu: "TE",
  Korean: "KO",
  Chinese: "ZH",
  Japanese: "JA",
  Turkish: "TR",
  Hungarian: "HU",
  Czech: "CS",
  Greek: "EL",
  Arabic: "AR",
  Hebrew: "HE",
  Ukrainian: "UK",
  Multi: "MULTI",
};

export const langCode = (name: string) => CODE[name] ?? name.slice(0, 2).toUpperCase();

export interface Langs {
  /** Tagged audio languages, "Multi" first when the release is MULTi/dual. */
  audio: string[];
  /** False when the name says nothing about audio (usually English). */
  audioTagged: boolean;
  /** yes: English or MULTi tagged; likely: untagged; no: only other languages tagged. */
  english: "yes" | "likely" | "no";
  /** Tagged subtitle languages ("Multi" for multi-sub packs). */
  subs: string[];
  /** Subtitles mentioned, language unknown. */
  subbed: boolean;
  hardsub: boolean;
}

export function langsOf(i: ReleaseInfo): Langs {
  const tagged = i.languages ?? [];
  const multi = !!(i.multi || i.dualAudio);
  const audio = multi ? ["Multi", ...tagged.filter((l) => l !== "Multi")] : [...tagged];
  const english = multi || tagged.includes("English") ? "yes" : tagged.length === 0 ? "likely" : "no";
  return {
    audio,
    audioTagged: audio.length > 0,
    english,
    subs: i.subtitles ?? [],
    subbed: !!i.subbed || !!(i.subtitles?.length),
    hardsub: !!i.hardSubbed,
  };
}

export const resName = (r?: number) => (!r ? "" : r === 2160 ? "2160p" : `${r}p`);
export const codecName = (c?: string) => (c === "x265" ? "HEVC" : c === "x264" ? "H.264" : c === "av1" ? "AV1" : c === "xvid" ? "XviD" : (c ?? ""));
