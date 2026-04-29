import type { FC } from "react";
import { useTranslation } from "react-i18next";
import type { MediaItem } from "@/api/types";
import { thumb } from "@/utils/imageUrl";

// Row stills are 260-320px wide on lg; 720 keeps detail under 2x DPR.
const ROW_THUMB_WIDTH = 720;

interface EpisodeRowProps {
  item: MediaItem;
  /**
   * Fired when the user clicks the row — caller wires this to its inline
   * VideoPlayer launch path so the season detail page never has to
   * navigate. Receives the episode id; resume position comes from the
   * stream-info endpoint when the player mounts.
   */
  onPlay: (itemId: string) => void;
}

const TICKS_PER_SECOND = 10_000_000;

function formatRuntime(ticks: number | null): string | null {
  if (!ticks) return null;
  const totalMin = Math.round(ticks / TICKS_PER_SECOND / 60);
  if (totalMin < 60) return `${totalMin}m`;
  const h = Math.floor(totalMin / 60);
  const m = totalMin % 60;
  return m > 0 ? `${h}h ${m}m` : `${h}h`;
}

/**
 * formatEndsAt computes the wall-clock end time if the user starts
 * playback now. Pure local-time math — same as Jellyfin's "Termina a
 * las HH:MM" affordance under each episode card. Returns null when we
 * don't know the duration (probe missed, cold row).
 *
 * `Date` is read fresh on every render rather than memoised — the
 * value is shown statically and a 1-minute drift is invisible to the
 * user, so we trade a tiny re-computation for not having to set up a
 * ticker.
 */
function formatEndsAt(durationTicks: number | null, locale: string | undefined): string | null {
  if (!durationTicks) return null;
  const seconds = durationTicks / TICKS_PER_SECOND;
  const end = new Date(Date.now() + seconds * 1000);
  return end.toLocaleTimeString(locale, { hour: "2-digit", minute: "2-digit" });
}

/**
 * EpisodeRow — Jellyfin-style horizontal episode card used on the
 * season-detail page. Replaces the smaller EpisodeCard for surfaces
 * where we want the full per-episode metadata in one glance:
 *
 *  - Still on the left (16:9), with a hover Play overlay.
 *  - Title + episode code (S01E03) above the meta line.
 *  - Meta line: air date, runtime, rating, "Termina a las HH:MM".
 *  - Synopsis paragraph clamped to 2 lines so the rows stay uniform.
 *  - In-progress bar at the bottom of the still when applicable.
 *
 * Click anywhere on the row fires `onPlay(item.id)` — no navigation,
 * the parent page launches the inline VideoPlayer with that target.
 */
const EpisodeRow: FC<EpisodeRowProps> = ({ item, onPlay }) => {
  const { t, i18n } = useTranslation();
  const code =
    item.season_number != null && item.episode_number != null
      ? `S${String(item.season_number).padStart(2, "0")}E${String(item.episode_number).padStart(2, "0")}`
      : null;
  const runtime = formatRuntime(item.duration_ticks);
  const endsAt = formatEndsAt(item.duration_ticks, i18n.language);
  const date = item.premiere_date
    ? new Date(item.premiere_date).toLocaleDateString(i18n.language, {
        day: "numeric",
        month: "short",
        year: "numeric",
      })
    : null;
  const progress = item.user_data?.progress.percentage ?? 0;
  const stillUrl = item.backdrop_url ?? item.poster_url;

  return (
    <button
      type="button"
      onClick={() => onPlay(item.id)}
      className="group flex w-full flex-col gap-3 rounded-[--radius-lg] p-2 text-left outline-none transition-colors hover:bg-bg-elevated/60 focus-visible:bg-bg-elevated/60 focus-visible:ring-2 focus-visible:ring-accent sm:flex-row sm:items-start sm:gap-4 cursor-pointer"
    >
      {/* Still — fixed aspect on every viewport so a row of episodes
          with mixed-quality artwork keeps the same baseline. */}
      <div className="relative aspect-video w-full shrink-0 overflow-hidden rounded-[--radius-md] bg-bg-elevated sm:w-[260px] lg:w-[320px]">
        {stillUrl ? (
          <img
            src={thumb(stillUrl, ROW_THUMB_WIDTH) ?? stillUrl}
            alt=""
            loading="lazy"
            className="h-full w-full object-cover transition-transform duration-300 group-hover:scale-[1.03]"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center bg-gradient-to-br from-bg-card to-bg-elevated">
            <span className="text-xl font-bold text-text-muted">{code ?? item.title}</span>
          </div>
        )}

        {/* Hover Play overlay — same affordance as the smaller
            EpisodeCard so the interaction reads consistently across
            surfaces. */}
        <div className="absolute inset-0 flex items-center justify-center bg-black/40 opacity-0 transition-opacity duration-200 group-hover:opacity-100">
          <div className="flex h-12 w-12 items-center justify-center rounded-full border-2 border-white bg-white/10 backdrop-blur-sm">
            <svg className="h-5 w-5 text-white" viewBox="0 0 24 24" fill="currentColor">
              <path d="M8 5v14l11-7z" />
            </svg>
          </div>
        </div>

        {progress > 0 && progress < 95 && (
          <div className="absolute bottom-0 left-0 right-0 h-1 bg-black/40">
            <div
              className="h-full bg-accent"
              style={{ width: `${Math.min(100, Math.max(0, progress))}%` }}
            />
          </div>
        )}
      </div>

      {/* Meta column */}
      <div className="flex min-w-0 flex-1 flex-col gap-1.5">
        <div className="flex items-baseline gap-2">
          {code && (
            <span className="shrink-0 rounded-[--radius-sm] bg-bg-elevated px-1.5 py-0.5 text-xs font-semibold text-text-secondary">
              {code}
            </span>
          )}
          <h3 className="truncate text-base font-semibold text-text-primary sm:text-lg">
            {item.title}
          </h3>
        </div>

        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-text-muted">
          {date && <span>{date}</span>}
          {runtime && <span>{runtime}</span>}
          {item.community_rating != null && (
            <span className="inline-flex items-center gap-0.5 text-warning">
              <svg className="h-3 w-3" viewBox="0 0 24 24" fill="currentColor">
                <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z" />
              </svg>
              {item.community_rating.toFixed(1)}
            </span>
          )}
          {endsAt && (
            <span className="text-text-secondary">
              {t("itemDetail.endsAt", { time: endsAt })}
            </span>
          )}
        </div>

        {item.overview && (
          <p className="line-clamp-2 text-sm leading-relaxed text-text-secondary">
            {item.overview}
          </p>
        )}
      </div>
    </button>
  );
};

export { EpisodeRow };
export type { EpisodeRowProps };
