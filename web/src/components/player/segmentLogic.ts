// Pure helpers for the skip-intro / skip-credits floating button.
// Lifted out of SkipSegmentButton.tsx so the lint rule that wants
// component files to only export components stays happy AND so the
// logic stays trivially testable in isolation from React.

import type { EpisodeSegment } from "@/api/types";

// Confidence floor for auto-surfacing the button. Chapter-derived
// segments are always 0.95 (well above this); the bound exists so
// that future low-confidence fingerprint hits don't pop a button at
// the wrong moment. Tunable per-deployment without code changes is
// out of scope — 0.7 is a sensible default everywhere.
export const MIN_CONFIDENCE = 0.7;

// Tail-trim on the active window so the button doesn't flicker into
// view in the last half-second of the segment. Without this, the
// "intro" range that ends at currentTime + 0.4s briefly disappears
// and reappears as the timer ticks — bad UX.
export const TAIL_TRIM_SECONDS = 0.5;

// pickActiveSegment returns the segment whose [start, end - tail]
// window contains the current playback time, after filtering by
// confidence and the nextUpAvailable gate for outros.
//
// When two segments would qualify (theoretically possible if the
// detector is buggy and they overlap) the one that starts later
// wins — the user is almost certainly inside that one. Stable for
// the realistic case where segments don't overlap at all.
export function pickActiveSegment(
  segments: EpisodeSegment[] | undefined,
  currentTime: number,
  nextUpAvailable: boolean,
): EpisodeSegment | null {
  if (!segments || segments.length === 0) return null;
  let best: EpisodeSegment | null = null;
  for (const s of segments) {
    if (s.confidence < MIN_CONFIDENCE) continue;
    if (s.kind === "outro" && !nextUpAvailable) continue;
    const start = s.start_seconds;
    const end = s.end_seconds - TAIL_TRIM_SECONDS;
    if (currentTime < start || currentTime >= end) continue;
    if (best === null || s.start_seconds > best.start_seconds) {
      best = s;
    }
  }
  return best;
}
