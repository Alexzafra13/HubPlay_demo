import type { MediaItem } from "@/api/types";

/**
 * The shape of a Watch Tonight pick. Encapsulates the choice rule
 * (the "why" surfaces in the rendered subtitle) so the rendering
 * component stays dumb — just lays out backdrop, title, action.
 */
export interface WatchTonightPick {
  item: MediaItem;
  reason: "resume" | "recommended";
  /** When `reason === "resume"`, seconds-into-the-content the user
   *  was last at, for the "Resume from MM:SS" subtitle. Undefined
   *  for the recommended path. */
  resumeSeconds?: number;
}

/**
 * pickWatchTonight picks ONE thing the home page should put in
 * front of the user. Heuristic, in priority order:
 *
 *   1. Latest in-progress item from continue-watching, IF it was
 *      played within the last 14 days. The continue-watching
 *      endpoint already filters out abandoned items (>30 days,
 *      <50 % progress) and near-complete (>=90 %), so anything
 *      it returns is a sensible resume candidate. The 14-day cap
 *      on top is the "you didn't just take a long weekend off"
 *      signal — beyond that we'd rather recommend something fresh
 *      than nag you about a series you cooled off on.
 *   2. Otherwise, the highest-rated recently-added item with a
 *      backdrop (the tile is visually empty without one). Falls
 *      back to whatever has a backdrop if nothing is rated.
 *   3. nil — Home renders nothing in that slot. Better no tile
 *      than a tile of last resort.
 *
 * `now` is injected for testability — production passes `Date.now()`.
 */
export function pickWatchTonight(
  continueWatching: MediaItem[],
  latest: MediaItem[],
  now: number,
): WatchTonightPick | null {
  // Continue-watching items carry runtime state under non-MediaItem
  // keys (`position_ticks`, `last_played_at`) that the backend
  // synthesises in the handler. We treat them as optional augmentations
  // rather than tighten the MediaItem type — the rest of the app
  // doesn't care about these fields for the same items in other
  // contexts.
  type CW = MediaItem & { position_ticks?: number; last_played_at?: string | null };

  for (const candidate of continueWatching as CW[]) {
    const ts = candidate.last_played_at ? Date.parse(candidate.last_played_at) : NaN;
    if (!Number.isFinite(ts)) continue;
    const ageDays = (now - ts) / (1000 * 60 * 60 * 24);
    if (ageDays > 14) continue;
    return {
      item: candidate,
      reason: "resume",
      resumeSeconds:
        candidate.position_ticks != null ? candidate.position_ticks / 10_000_000 : undefined,
    };
  }

  // Recommendation path. Demand a backdrop because the tile is the
  // size of a billboard — a poster fallback looks broken at this scale.
  const recoCandidates = latest.filter((i) => !!i.backdrop_url);
  if (recoCandidates.length === 0) return null;
  const sorted = recoCandidates.toSorted(
    (a, b) => (b.community_rating ?? 0) - (a.community_rating ?? 0),
  );
  return { item: sorted[0], reason: "recommended" };
}
