// Audio track helpers for the video player picker.
//
// Two ways the picker is fed:
//
//   1. `enrichAudioTracks` cross-references hls.js audio tracks
//      against DB MediaStream rows to enrich a single in-stream
//      track's label with codec + channel info. Used when the
//      master.m3u8 actually exposes multiple tracks via EXT-X-MEDIA
//      (rare today — HubPlay transcodes one track per session).
//
//   2. `buildPickerTracksFromDB` builds the picker entries directly
//      from DB MediaStream rows. This is the normal HubPlay path:
//      the master only exposes one transcoded track, but the player
//      still wants to show ALL the file's audio options ("English
//      Atmos / Castellano DD+ 5.1 / Romanian DD+ 5.1") so the user
//      can switch on the fly. Selecting a different one re-issues
//      the master with `?audio=<perTypeIdx>` — the per-type index
//      `pickAudioStreamIndex` already returns and ffmpeg's
//      `-map 0:a:N` consumes.
//
// Lives outside PlayerControls.tsx so the file can stay a presentation
// component, and so this logic is unit-testable without a React render.

export interface AudioTrack {
  id: number;
  name: string;
  lang: string;
}

// Subset of `api.MediaStream` the picker actually needs. Re-stated
// here instead of imported so the player surface stays independent
// of the wire shape — if the API ever renames a field the change
// stops at the call site, not the picker.
export interface AudioStreamInfo {
  index: number;
  codec: string;
  language: string | null;
  title: string | null;
  channels: number | null;
}

// Channel-count → marketing layout name. Conservative on edge cases
// (mono with > 8 ch, missing data) — falls through to the raw number
// so the picker never lies about what's actually on the file.
export function channelLabel(ch: number | null): string {
  if (ch == null || ch <= 0) return "";
  switch (ch) {
    case 1:
      return "Mono";
    case 2:
      return "Stereo";
    case 6:
      return "5.1";
    case 7:
      return "6.1";
    case 8:
      return "7.1";
    default:
      return `${ch}ch`;
  }
}

// Pretty-print a codec name for the picker. ffprobe spits out terse
// identifiers ("ac3", "eac3", "truehd"); the user expects the
// marketing names they see on the release ("AC3", "Atmos / TrueHD").
export function codecLabel(codec: string): string {
  switch (codec.toLowerCase()) {
    case "aac":
      return "AAC";
    case "ac3":
      return "AC3";
    case "eac3":
      return "EAC3";
    case "dts":
      return "DTS";
    case "dts-hd":
    case "dts_hd":
    case "dca":
      return "DTS-HD";
    case "truehd":
      return "TrueHD";
    case "flac":
      return "FLAC";
    case "opus":
      return "Opus";
    case "mp3":
      return "MP3";
    case "vorbis":
      return "Vorbis";
    default:
      return codec.toUpperCase();
  }
}

// Pretty-print a 3-letter ISO 639-2 code as a human-readable
// language name. Same set as the Settings picker — adding a
// language here means we render its name on every existing audio
// row even if no user has it as a preference yet. Falls through
// to the raw code uppercased so we never lie about which language
// the file claims.
//
// Locale-driven: the caller passes the active i18n locale so a
// Spanish UI says "Inglés" / "Español" but an English UI says
// "English" / "Spanish". We don't depend on `i18next.t()` to keep
// the helper a pure function — easier to test and reuse outside
// the player.
const LANGUAGE_NAMES: Record<string, { es: string; en: string }> = {
  spa: { es: "Español", en: "Spanish" },
  eng: { es: "Inglés", en: "English" },
  fre: { es: "Francés", en: "French" },
  ger: { es: "Alemán", en: "German" },
  ita: { es: "Italiano", en: "Italian" },
  por: { es: "Portugués", en: "Portuguese" },
  jpn: { es: "Japonés", en: "Japanese" },
  kor: { es: "Coreano", en: "Korean" },
  chi: { es: "Chino", en: "Chinese" },
  ara: { es: "Árabe", en: "Arabic" },
  rus: { es: "Ruso", en: "Russian" },
  rum: { es: "Rumano", en: "Romanian" },
};

export function languageLabel(code: string | null | undefined, locale: "es" | "en"): string {
  if (!code) return "";
  const head = code.toLowerCase().split(/[-_]/, 1)[0]!;
  // Same alias map shape as utils/playbackPrefs.ts so a "es" file
  // tag still resolves to "Español". Kept inline because the
  // playback-prefs alias table is on the user-side; this one is on
  // the file-side. The set of source codes we actually see in the
  // wild is small enough that two short tables are clearer than
  // a shared one with extra plumbing.
  const aliasOf639_1: Record<string, string> = {
    es: "spa", en: "eng", fr: "fre", de: "ger", it: "ita", pt: "por",
    ja: "jpn", ko: "kor", zh: "chi", ar: "ara", ru: "rus", ro: "rum",
  };
  const alias639_2T: Record<string, string> = {
    fra: "fre", deu: "ger", zho: "chi", ron: "rum",
  };
  const canonical = alias639_2T[head] ?? aliasOf639_1[head] ?? head;
  const entry = LANGUAGE_NAMES[canonical];
  return entry ? entry[locale] : head.toUpperCase();
}

/**
 * Build picker entries directly from the DB-side MediaStream rows.
 * Returns one AudioTrack per audio stream, with the per-type index
 * (the Nth audio stream when filtered to type='audio') as the id —
 * exactly what `?audio=N` and ffmpeg's `-map 0:a:N` consume.
 *
 * Label shape matches what Jellyfin shows on the same release:
 *   "Español · DD+ 5.1"
 *   "English · Dolby Atmos · Predeterminado"
 *
 * `defaultLabel` is the localized "default" badge ("Predeterminado"
 * in es, "Default" in en) — passed in instead of computed because
 * it's a translated string the picker already has via i18next.
 *
 * Streams the file marks as `is_default` get the badge appended.
 */
export function buildPickerTracksFromDB(
  streams: ReadonlyArray<{
    type?: string | null;
    stream_type?: string | null;
    codec: string;
    language: string | null;
    title: string | null;
    channels: number | null;
    is_default?: boolean | null;
  }>,
  locale: "es" | "en",
  defaultLabel: string,
): AudioTrack[] {
  const out: AudioTrack[] = [];
  let perTypeIdx = -1;
  for (const s of streams) {
    const kind = s.stream_type ?? s.type;
    if (kind !== "audio") continue;
    perTypeIdx++;

    const parts: string[] = [];
    const lang = languageLabel(s.language, locale);
    if (lang) parts.push(lang);
    const codec = codecLabel(s.codec);
    const ch = channelLabel(s.channels);
    const detail = ch ? `${codec} ${ch}` : codec;
    if (detail) parts.push(detail);
    if (s.title && !parts.some((p) => p.toLowerCase() === s.title!.toLowerCase())) {
      // Title sometimes carries an extra hint the codec line doesn't —
      // e.g. "Director's commentary" or "Castellano". Append it
      // unless it's already in the label.
      parts.push(s.title);
    }
    if (s.is_default) parts.push(defaultLabel);

    out.push({
      id: perTypeIdx,
      name: parts.join(" · ") || `Audio ${perTypeIdx + 1}`,
      lang: s.language ?? "",
    });
  }
  return out;
}

/**
 * Cross-references the bare hls.js audio tracks against the DB-side
 * MediaStream rows to produce a richer picker label.
 *
 * Match strategy:
 *   1. Within each language code, pair tracks in the order they
 *      appear. So if the file has [eng-AAC, eng-TrueHD] and hls.js
 *      reports [eng#1, eng#2], they line up 1↔1. Order from the
 *      DB matches ffprobe's stream ordering, which the muxer
 *      preserves into the HLS manifest.
 *   2. If no DB stream matches the language, the original hls.js
 *      label survives — better partial enrichment than wrong.
 *
 * Result label shape: "English · TrueHD 7.1" or "Spanish · AAC Stereo".
 * Falls back to just the codec when the bare name is missing.
 */
export function enrichAudioTracks(
  hlsTracks: AudioTrack[],
  dbStreams: AudioStreamInfo[],
): AudioTrack[] {
  if (hlsTracks.length === 0) return hlsTracks;

  // Index DB streams by language code, preserving file order.
  const byLang = new Map<string, AudioStreamInfo[]>();
  for (const s of dbStreams) {
    const k = (s.language ?? "").toLowerCase();
    const arr = byLang.get(k) ?? [];
    arr.push(s);
    byLang.set(k, arr);
  }

  // Cursor per language so we pop in order.
  const cursors = new Map<string, number>();

  return hlsTracks.map((track) => {
    const langKey = (track.lang ?? "").toLowerCase();
    const candidates = byLang.get(langKey);
    if (!candidates || candidates.length === 0) return track;

    const cursor = cursors.get(langKey) ?? 0;
    cursors.set(langKey, cursor + 1);
    const stream = candidates[cursor];
    if (!stream) return track;

    const parts: string[] = [];
    if (track.name) parts.push(track.name);
    else if (track.lang) parts.push(track.lang.toUpperCase());

    const codec = codecLabel(stream.codec);
    const ch = channelLabel(stream.channels);
    const detail = ch ? `${codec} ${ch}` : codec;
    if (detail) parts.push(detail);

    return {
      ...track,
      name: parts.join(" · "),
    };
  });
}
