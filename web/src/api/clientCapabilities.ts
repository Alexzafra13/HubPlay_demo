// Probes the browser's MediaSource to discover which codecs it can
// decode natively, then formats the result as the wire shape the
// server's stream.ParseCapabilitiesHeader expects.
//
// Why probe at request time instead of guessing from the user agent:
//   - HEVC / H.265 support depends on whether the OS ships a hardware
//     decoder (Safari yes; Chrome/Edge with VAAPI yes; Firefox usually
//     no). User-agent sniffing gets this wrong constantly.
//   - AV1 support is rolling out per-browser-version + per-platform.
//   - Future codec rollouts ship to MediaSource registries first; the
//     probe Just Works without an update here.
//
// When `MediaSource` isn't available (SSR, very old browser, secure
// context where MSE is disabled), this returns null. The server's
// fallback is "no header → default web caps", so the worst case is
// today's behaviour, never worse.

// Codec → MIME-with-codecs string used to ask MediaSource if it can
// decode. Each entry covers the CODEC the server would name in the
// wire response, even if the browser actually wants a richer string
// (e.g. h264 = avc1.42E01E "Baseline 3.0", representative for the
// whole family from MediaSource's perspective).
const VIDEO_PROBES: Array<[string, string]> = [
  ["h264", 'video/mp4; codecs="avc1.42E01E"'],
  ["hevc", 'video/mp4; codecs="hev1.1.6.L93.B0"'],
  // ffprobe reports HEVC as "hevc"; some clients see "h265" — emit
  // both so a server map keyed on either matches.
  ["h265", 'video/mp4; codecs="hev1.1.6.L93.B0"'],
  ["vp8", 'video/webm; codecs="vp8"'],
  ["vp9", 'video/webm; codecs="vp9"'],
  ["av1", 'video/mp4; codecs="av01.0.04M.08"'],
];

const AUDIO_PROBES: Array<[string, string]> = [
  ["aac", 'audio/mp4; codecs="mp4a.40.2"'],
  ["mp3", "audio/mpeg"],
  ["opus", 'audio/webm; codecs="opus"'],
  ["vorbis", 'audio/webm; codecs="vorbis"'],
  ["flac", "audio/flac"],
  // EAC3 / AC3 are gated by HW decoders on most platforms; probe
  // anyway — Safari with the right HW decodes EAC3 5.1.
  ["eac3", 'audio/mp4; codecs="ec-3"'],
  ["ac3", 'audio/mp4; codecs="ac-3"'],
];

// Containers the browser can actually demux (not just transmux).
// Hard-coded because MediaSource doesn't expose this directly — the
// list is "what mainstream browsers support natively". MKV intentionally
// absent: browsers don't demux Matroska, but the server's DirectStream
// can remux it to MP4 for them.
const CONTAINERS = ["mp4", "webm", "mov"];

// Cache the result of the probes — they're deterministic per browser
// session and cheap to redo, but the request hot path benefits from
// memoising.
let cachedHeaderValue: string | null | undefined;

/**
 * Returns the value for the X-Hubplay-Client-Capabilities header, or
 * null when the browser environment can't be probed (SSR, no
 * MediaSource). The string is cached for the lifetime of the page;
 * codec support doesn't change without a reload.
 *
 * Wire shape (matches the server's ParseCapabilitiesHeader):
 *
 *   video=h264,vp9,av1; audio=aac,mp3,opus; container=mp4,webm
 */
export function getClientCapabilitiesHeader(): string | null {
  if (cachedHeaderValue !== undefined) return cachedHeaderValue;
  cachedHeaderValue = computeHeader();
  return cachedHeaderValue;
}

/** Reset the cache. Test-only — production code should not call this. */
export function resetClientCapabilitiesCacheForTests() {
  cachedHeaderValue = undefined;
}

function computeHeader(): string | null {
  if (
    typeof window === "undefined" ||
    typeof window.MediaSource === "undefined" ||
    typeof window.MediaSource.isTypeSupported !== "function"
  ) {
    return null;
  }

  const isSupported = (mime: string) => {
    try {
      return window.MediaSource.isTypeSupported(mime);
    } catch {
      // Some browsers throw on malformed MIME instead of returning false.
      return false;
    }
  };

  const probe = (entries: Array<[string, string]>) =>
    entries
      .filter(([, mime]) => isSupported(mime))
      .map(([codec]) => codec);

  const video = probe(VIDEO_PROBES);
  const audio = probe(AUDIO_PROBES);

  // No video probe matched — almost certainly a misconfigured environment;
  // returning null preserves server-side defaults rather than locking
  // the user out of every codec.
  if (video.length === 0 && audio.length === 0) return null;

  const parts: string[] = [];
  if (video.length > 0) parts.push(`video=${video.join(",")}`);
  if (audio.length > 0) parts.push(`audio=${audio.join(",")}`);
  parts.push(`container=${CONTAINERS.join(",")}`);
  return parts.join("; ");
}
