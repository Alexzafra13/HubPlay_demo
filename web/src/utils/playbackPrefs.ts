// Well-known keys the playback surfaces store under
// `/me/preferences/{key}`. Pulled out of the components that consume
// them so the HeroSection, SeriesHero and Settings page share a single
// source of truth — a typo on either side would silently disconnect
// the toggle from the reading surface.

/**
 * Boolean. When true (default), the detail-page hero auto-plays the
 * trailer ~3s after the page settles. When false, the static backdrop
 * image stays put and no trailer iframe is ever loaded. Persisted per
 * user via `/me/preferences` so the choice follows the account across
 * devices, unlike the session-only "Skip" button on the trailer
 * itself which only suppresses for the current tab.
 */
export const TRAILERS_ENABLED_PREF_KEY = "playback.trailers_enabled";

/**
 * ISO 639-2 (3-letter) code for the user's preferred audio language.
 * Empty string = "use the file's default audio track" (current
 * behaviour for everyone before they pick a preference). Resolved by
 * the player on stream start: it scans the item's media_streams for
 * the first audio row whose `language` matches and appends
 * `?audio=<index>` to the manifest URL so the backend transcoder maps
 * that specific track. Same model Plex / Jellyfin use for "Audio
 * language" in the account preferences pane.
 */
export const PREFERRED_AUDIO_LANG_PREF_KEY = "playback.preferred_audio_lang";

/**
 * ISO 639-2 code for preferred subtitle language. Empty = no subs
 * (current default). The player auto-enables a matching text track
 * on first manifest load when this is set; the user can still
 * disable mid-playback from the subtitle picker — that override is
 * session-scoped and doesn't write back to the pref so the next
 * episode reverts to the configured default.
 */
export const PREFERRED_SUBTITLE_LANG_PREF_KEY = "playback.preferred_subtitle_lang";

/**
 * Curated list of languages exposed in the Settings pickers. Three-
 * letter ISO 639-2 codes — what ffprobe writes into media_streams.
 * language and what the source mkv tags emit. Intentionally short:
 * the picker is for the user to set a default, NOT for them to
 * discover every possible language. New entries added by request as
 * users hit them.
 */
export interface PlaybackLanguage {
  code: string;
  labelEs: string;
  labelEn: string;
}
export const PLAYBACK_LANGUAGES: PlaybackLanguage[] = [
  { code: "spa", labelEs: "Español", labelEn: "Spanish" },
  { code: "eng", labelEs: "Inglés", labelEn: "English" },
  { code: "fre", labelEs: "Francés", labelEn: "French" },
  { code: "ger", labelEs: "Alemán", labelEn: "German" },
  { code: "ita", labelEs: "Italiano", labelEn: "Italian" },
  { code: "por", labelEs: "Portugués", labelEn: "Portuguese" },
  { code: "jpn", labelEs: "Japonés", labelEn: "Japanese" },
  { code: "kor", labelEs: "Coreano", labelEn: "Korean" },
  { code: "chi", labelEs: "Chino", labelEn: "Chinese" },
  { code: "ara", labelEs: "Árabe", labelEn: "Arabic" },
  { code: "rus", labelEs: "Ruso", labelEn: "Russian" },
];

/**
 * Maps ISO 639-1 (2-letter) codes to ISO 639-2/B (3-letter) so the
 * file-side language tag and the user-side preference can be compared
 * without forcing both ends to agree on a single standard. The MKV
 * spec mandates 639-2, but plenty of encoders (and ffprobe pulling
 * them through) emit 639-1 instead. Covers the languages exposed in
 * the Settings picker plus a few common ISO 639-2/T (terminological)
 * synonyms — `fra`/`fre`, `deu`/`ger`, `zho`/`chi`, `ron`/`rum` —
 * because real-world rips use both halves of the dual codes.
 */
const LANGUAGE_ALIASES: Record<string, string> = {
  // ISO 639-1 → ISO 639-2/B
  es: "spa",
  en: "eng",
  fr: "fre",
  de: "ger",
  it: "ita",
  pt: "por",
  ja: "jpn",
  ko: "kor",
  zh: "chi",
  ar: "ara",
  ru: "rus",
  ro: "rum",
  // ISO 639-2/T → ISO 639-2/B (the codes the picker uses)
  fra: "fre",
  deu: "ger",
  zho: "chi",
  ron: "rum",
};

/**
 * normaliseLanguage collapses a language tag to the canonical ISO
 * 639-2/B 3-letter form the picker uses. Strips region/script
 * suffixes (`spa-419`, `es-MX`, `zh-Hant`) and resolves 2-letter +
 * alternate-639-2 codes via LANGUAGE_ALIASES. Returns the lowercase
 * input untouched when it doesn't fit any of the known shapes — the
 * caller still gets a deterministic key it can match on.
 */
export function normaliseLanguage(raw: string | null | undefined): string {
  if (!raw) return "";
  // BCP-47 / Matroska region tags: keep the first subtag only.
  const head = raw.toLowerCase().split(/[-_]/, 1)[0]!;
  return LANGUAGE_ALIASES[head] ?? head;
}

/**
 * pickAudioStreamIndex returns the 0-based index of the first audio
 * stream in `streams` whose `language` matches `preferredLang`, or
 * -1 if no match (or no preference). Index is per-type — i.e. the
 * Nth audio stream when filtered to type='audio' — which is what
 * ffmpeg's `-map 0:a:<N>` expects. Source-array order is preserved
 * (ffprobe writes streams in container order; the container's
 * decision about which audio is "default" is irrelevant once we
 * pin a specific index).
 *
 * Language matching is lenient: ISO 639-1 ("es") matches the picker's
 * 639-2 ("spa") via LANGUAGE_ALIASES, and region-tagged codes
 * ("spa-419", "en-GB") match their bare form. Without this a
 * Daredevil rip whose Spanish track is tagged "es" or "spa-419"
 * never matched a "spa" preference — playback fell back to the file
 * default (English) on the first play.
 */
export function pickAudioStreamIndex(
  // The runtime shape from the API is snake_case (`stream_type`,
  // `stream_index`) — mirrors sqlc's column names. The TS interface
  // claims `type`/`index` but never matched reality. We accept both
  // here so the helper works regardless of which side is fixed first.
  streams:
    | ReadonlyArray<{
        type?: string | null;
        stream_type?: string | null;
        language?: string | null;
      }>
    | undefined
    | null,
  preferredLang: string,
): number {
  if (!streams || !preferredLang) return -1;
  const want = normaliseLanguage(preferredLang);
  if (!want) return -1;
  let audioIdx = -1;
  for (const s of streams) {
    const kind = s.stream_type ?? s.type;
    if (kind !== "audio") continue;
    audioIdx++;
    if (normaliseLanguage(s.language) === want) {
      return audioIdx;
    }
  }
  return -1;
}
