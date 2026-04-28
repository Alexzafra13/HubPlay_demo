// Audio track helpers for the video player picker.
//
// `enrichAudioTracks` cross-references the bare `hls.js` audio track
// list (just name + lang) against the DB-side MediaStream rows
// (codec + channel layout) and returns a richer label like
// "English · TrueHD 7.1" or "Spanish · AAC Stereo".
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
