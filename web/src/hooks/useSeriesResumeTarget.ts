import { useMemo } from "react";
import { useContinueWatching, useNextUp, useItemChildren } from "@/api/hooks";
import type { MediaItem } from "@/api/types";

/**
 * Resume mode for the series Play button.
 *
 *  - `resume`   — the user has an in-progress episode of THIS series.
 *                 The button label switches to "Seguir viendo SXXEYY".
 *  - `next-up`  — the user has finished one and the next is queued.
 *                 Button still says "Reproducir" but jumps to the queued
 *                 episode rather than the very first.
 *  - `start`    — no progress on this series. Falls back to the first
 *                 episode of the lowest-numbered season.
 *  - `none`     — no episodes available at all. Caller should disable
 *                 the Play button.
 */
export type SeriesResumeMode = "resume" | "next-up" | "start" | "none";

export interface SeriesResumeTarget {
  mode: SeriesResumeMode;
  episode: MediaItem | null;
  seasonNumber: number | null;
  episodeNumber: number | null;
  /**
   * 0-100 progress percentage on the resume target. `null` for non-resume
   * modes — the badge bar only renders when there's actual progress to
   * show.
   */
  progressPercent: number | null;
}

const EMPTY: SeriesResumeTarget = {
  mode: "none",
  episode: null,
  seasonNumber: null,
  episodeNumber: null,
  progressPercent: null,
};

/**
 * Scope tells the hook which entity the user is viewing — drives both
 * the continue-watching match field and the cold-start fallback path.
 *
 *  - "series": match continue-watching by `series_id`; cold-start
 *              picks the lowest-numbered season (caller routes to it).
 *  - "season": match continue-watching by `parent_id`; cold-start
 *              picks the lowest-numbered episode (caller plays it).
 */
export type ResumeScope = "series" | "season";

/**
 * useResumeTarget — given an entity scope + id, returns the canonical
 * "what happens when the user clicks Play?" answer for that entity.
 *
 * Resolution order, mirroring how Plex / Jellyfin pick a resume target:
 *
 *   1. If continue-watching has an in-progress episode under this
 *      entity (still under 95%), jump there ("resume").
 *   2. Else if next-up has a queued episode for this entity, jump
 *      there ("next-up").
 *   3. Else cold-start: pick the lowest-numbered child of the right
 *      type (season for a series, episode for a season).
 *
 * The children query is only needed for the cold-start path and is
 * already in the page's cache because SeasonGrid / SeasonEpisodeList
 * mounted just below this hook.
 */
export function useResumeTarget(scope: ResumeScope, id: string | null): SeriesResumeTarget {
  const { data: continueWatching } = useContinueWatching({ enabled: !!id });
  const { data: nextUp } = useNextUp({ enabled: !!id });
  const { data: children } = useItemChildren(id ?? "", { enabled: !!id });

  return useMemo<SeriesResumeTarget>(() => {
    if (!id) return EMPTY;

    const matches = (item: MediaItem) =>
      scope === "series"
        ? item.series_id === id
        : item.parent_id === id;

    const inProgress = continueWatching?.find(
      (item) => matches(item) && (item.user_data?.progress.percentage ?? 0) < 95,
    );
    if (inProgress) {
      return {
        mode: "resume",
        episode: inProgress,
        seasonNumber: inProgress.season_number,
        episodeNumber: inProgress.episode_number,
        progressPercent: inProgress.user_data?.progress.percentage ?? null,
      };
    }

    const queued = nextUp?.find(matches);
    if (queued) {
      return {
        mode: "next-up",
        episode: queued,
        seasonNumber: queued.season_number,
        episodeNumber: queued.episode_number,
        progressPercent: null,
      };
    }

    // Cold-start: scope-dependent fallback. For a series we route to
    // the first season (caller decides whether to navigate or auto-
    // resolve E01); for a season we go straight to the first episode.
    if (scope === "series") {
      const firstSeason = (children ?? [])
        .filter((c) => c.type === "season" && c.season_number != null)
        .sort((a, b) => (a.season_number ?? 0) - (b.season_number ?? 0))[0];
      if (firstSeason) {
        return {
          mode: "start",
          episode: firstSeason,
          seasonNumber: firstSeason.season_number,
          episodeNumber: 1,
          progressPercent: null,
        };
      }
    } else {
      const firstEpisode = (children ?? [])
        .filter((c) => c.type === "episode" && c.episode_number != null)
        .sort((a, b) => (a.episode_number ?? 0) - (b.episode_number ?? 0))[0];
      if (firstEpisode) {
        return {
          mode: "start",
          episode: firstEpisode,
          seasonNumber: firstEpisode.season_number,
          episodeNumber: firstEpisode.episode_number,
          progressPercent: null,
        };
      }
    }

    return EMPTY;
  }, [scope, id, continueWatching, nextUp, children]);
}

/**
 * Back-compat alias so existing callers keep working unchanged.
 * @deprecated Prefer `useResumeTarget("series", id)`.
 */
export function useSeriesResumeTarget(seriesId: string | null): SeriesResumeTarget {
  return useResumeTarget("series", seriesId);
}
