import type { FC } from "react";
import { Link } from "react-router";
import { useTranslation } from "react-i18next";
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
  const sorted = [...recoCandidates].sort(
    (a, b) => (b.community_rating ?? 0) - (a.community_rating ?? 0),
  );
  return { item: sorted[0], reason: "recommended" };
}

interface WatchTonightTileProps {
  pick: WatchTonightPick;
}

const WatchTonightTile: FC<WatchTonightTileProps> = ({ pick }) => {
  const { t } = useTranslation();
  const { item, reason, resumeSeconds } = pick;
  const href = item.type === "series" ? `/series/${item.id}` : `/movies/${item.id}`;

  return (
    <Link
      to={href}
      className="group relative block overflow-hidden rounded-[--radius-lg] border border-border bg-bg-card transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
      aria-label={`${t(reason === "resume" ? "watchTonight.resumeLabel" : "watchTonight.recommendedLabel")}: ${item.title}`}
    >
      {item.backdrop_url && (
        <img
          src={item.backdrop_url}
          alt=""
          className="h-full w-full object-cover transition-transform duration-700 group-hover:scale-[1.02]"
          style={{ aspectRatio: "21 / 9" }}
        />
      )}
      <div className="absolute inset-0 bg-gradient-to-r from-black/85 via-black/40 to-transparent" />
      <div className="absolute inset-0 bg-gradient-to-t from-black/70 via-transparent to-transparent" />

      <div className="absolute inset-x-0 bottom-0 p-6 sm:p-8 md:p-10">
        <p className="mb-1 text-xs font-bold uppercase tracking-[0.18em] text-accent">
          {t(reason === "resume" ? "watchTonight.resume" : "watchTonight.tonight")}
        </p>
        <h2 className="text-2xl font-bold text-white drop-shadow-lg sm:text-3xl md:text-4xl">
          {item.title}
        </h2>
        {reason === "resume" && resumeSeconds != null && (
          <p className="mt-1 text-sm text-white/80">
            {t("watchTonight.resumeAt", { time: formatHMS(resumeSeconds) })}
          </p>
        )}
        {reason === "recommended" && item.community_rating != null && (
          <p className="mt-1 text-sm text-white/80">
            ★ {item.community_rating.toFixed(1)}
            {item.year ? ` · ${item.year}` : ""}
          </p>
        )}
        <span className="mt-4 inline-flex items-center gap-2 rounded-full bg-white px-5 py-2 text-sm font-semibold text-black transition-colors group-hover:bg-white/90">
          <svg className="h-4 w-4" viewBox="0 0 24 24" fill="currentColor">
            <path d="M8 5v14l11-7z" />
          </svg>
          {t(reason === "resume" ? "watchTonight.resumeAction" : "watchTonight.playAction")}
        </span>
      </div>
    </Link>
  );
};

function formatHMS(s: number): string {
  if (!isFinite(s) || s < 0) return "0:00";
  const total = Math.floor(s);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const sec = total % 60;
  const pad = (n: number) => n.toString().padStart(2, "0");
  return h > 0 ? `${h}:${pad(m)}:${pad(sec)}` : `${m}:${pad(sec)}`;
}

export { WatchTonightTile };
