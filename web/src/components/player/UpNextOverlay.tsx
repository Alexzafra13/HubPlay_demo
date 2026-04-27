import { useEffect, useRef, useState, useCallback } from "react";
import type { FC } from "react";
import { useTranslation } from "react-i18next";

export interface UpNextInfo {
  title: string;
  seasonNumber?: number | null;
  episodeNumber?: number | null;
  posterUrl?: string | null;
  backdropUrl?: string | null;
}

interface UpNextOverlayProps {
  nextUp: UpNextInfo;
  /** Auto-advance: call this when the user confirms or the timer expires. */
  onPlayNow: () => void;
  /** Cancel: dismiss the overlay; the player stays on the ended frame. */
  onCancel: () => void;
  /** Countdown duration in seconds. Defaults to 5 (Plex-like). */
  durationSeconds?: number;
}

const DEFAULT_DURATION = 5;
const TICK_MS = 100; // smooth circular progress without melting CPU

/**
 * Episode label like `S2 · E5` when both numbers are available, just
 * the season or episode otherwise. Pure helper so the JSX stays clean.
 */
function formatEpisodeCode(season?: number | null, episode?: number | null): string | null {
  if (season != null && episode != null) {
    return `S${season} · E${episode}`;
  }
  if (season != null) return `S${season}`;
  if (episode != null) return `E${episode}`;
  return null;
}

/**
 * UpNextOverlay — countdown card prompting auto-advance to the next
 * episode. Shown when the previous video fires `ended` and the parent
 * supplied `nextUp` metadata. The overlay owns the timer; the parent
 * only sees the binary outcome (confirm or cancel).
 *
 * Behaviour:
 *  - 5-second countdown by default.
 *  - "Play now" or timer expiry → onPlayNow().
 *  - "Cancel" or Escape key → onCancel(); parent leaves the player on
 *    the ended frame.
 *  - The whole card is keyboard-focused on mount so screen readers
 *    announce the prompt and Esc / Tab work without an extra click.
 */
const UpNextOverlay: FC<UpNextOverlayProps> = ({
  nextUp,
  onPlayNow,
  onCancel,
  durationSeconds = DEFAULT_DURATION,
}) => {
  const { t } = useTranslation();
  const [remaining, setRemaining] = useState(durationSeconds);
  const playRef = useRef(onPlayNow);
  // Keep the latest callback in a ref so the timer effect can stay
  // dependency-free without going stale. The countdown should not
  // restart just because the parent re-renders.
  playRef.current = onPlayNow;

  useEffect(() => {
    const start = Date.now();
    const interval = window.setInterval(() => {
      const elapsed = (Date.now() - start) / 1000;
      const left = Math.max(0, durationSeconds - elapsed);
      setRemaining(left);
      if (left <= 0) {
        window.clearInterval(interval);
        playRef.current();
      }
    }, TICK_MS);
    return () => window.clearInterval(interval);
  }, [durationSeconds]);

  const handleEsc = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onCancel();
      }
    },
    [onCancel],
  );

  useEffect(() => {
    document.addEventListener("keydown", handleEsc);
    return () => document.removeEventListener("keydown", handleEsc);
  }, [handleEsc]);

  const code = formatEpisodeCode(nextUp.seasonNumber, nextUp.episodeNumber);
  const progress = 1 - Math.min(1, Math.max(0, remaining / durationSeconds));
  const seconds = Math.ceil(remaining);

  return (
    <div
      role="dialog"
      aria-live="polite"
      aria-label={t("upNext.title")}
      className="flex w-full max-w-md gap-4 rounded-[--radius-lg] border border-border bg-bg-card/95 p-4 shadow-2xl shadow-black/50 backdrop-blur-md"
    >
      {/* Thumb. Falls back to a tinted block when no art available so
          the layout doesn't collapse and the overlay still reads as a
          discrete card. */}
      <div className="relative h-24 w-40 shrink-0 overflow-hidden rounded-[--radius-md] bg-bg-elevated">
        {nextUp.backdropUrl || nextUp.posterUrl ? (
          <img
            src={nextUp.backdropUrl ?? nextUp.posterUrl ?? ""}
            alt=""
            className="h-full w-full object-cover"
          />
        ) : (
          <div className="absolute inset-0 bg-gradient-to-br from-bg-elevated to-bg-card" />
        )}
        <div className="absolute inset-0 bg-gradient-to-t from-black/60 to-transparent" />
        {code && (
          <span className="absolute bottom-1 left-1.5 rounded-[--radius-sm] bg-black/70 px-1.5 py-0.5 text-[10px] font-semibold text-white">
            {code}
          </span>
        )}
      </div>

      <div className="flex min-w-0 flex-1 flex-col justify-between gap-2">
        <div className="min-w-0">
          <p className="text-xs font-medium uppercase tracking-wide text-text-muted">
            {t("upNext.label")}
          </p>
          <p className="truncate text-sm font-semibold text-text-primary" title={nextUp.title}>
            {nextUp.title}
          </p>
        </div>

        <div className="flex items-center gap-2">
          <button
            type="button"
            autoFocus
            onClick={onPlayNow}
            className="flex flex-1 items-center justify-center gap-2 rounded-[--radius-md] bg-accent px-3 py-1.5 text-sm font-semibold text-white transition-colors hover:bg-accent/90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-bg-card cursor-pointer"
          >
            <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="currentColor">
              <path d="M8 5v14l11-7z" />
            </svg>
            {t("upNext.playIn", { seconds })}
          </button>

          <button
            type="button"
            onClick={onCancel}
            className="flex h-8 w-8 items-center justify-center rounded-full border border-border text-text-secondary transition-colors hover:bg-bg-elevated hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent cursor-pointer"
            aria-label={t("upNext.cancel")}
            title={t("upNext.cancel")}
          >
            <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        {/* Slim progress bar at the bottom of the card mirrors the
            countdown so the user can tell "how soon" at a glance. */}
        <div
          className="h-1 overflow-hidden rounded-full bg-bg-elevated"
          role="progressbar"
          aria-valuenow={Math.round(progress * 100)}
          aria-valuemin={0}
          aria-valuemax={100}
        >
          <div
            className="h-full bg-accent transition-[width] duration-100 ease-linear"
            style={{ width: `${progress * 100}%` }}
          />
        </div>
      </div>
    </div>
  );
};

export { UpNextOverlay };
