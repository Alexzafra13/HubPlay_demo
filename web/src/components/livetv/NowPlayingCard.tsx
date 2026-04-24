import { useTranslation } from "react-i18next";
import type { Channel, EPGProgram } from "@/api/types";
import { formatTime } from "./epgHelpers";

interface NowPlayingCardProps {
  channel: Channel;
  nowPlaying: EPGProgram | null;
  upNext: EPGProgram | null;
  /** Ms-since-epoch from the parent's clock tick; used for progress math. */
  now: number;
}

/**
 * NowPlayingCard — summary of what's on the selected channel right now.
 *
 * Top of the PlayerOverlay side panel. Two states:
 *   - EPG available: big title, description (clamped 3 lines), time
 *     range + duration + category, progress bar derived from `now`, and
 *     an up-next hint.
 *   - No EPG: fallback showing "Sin guía disponible — {channel.name}".
 *
 * `now` comes from the parent's `useNowTick`; we take it as a prop
 * rather than call `Date.now()` here so the progress bar stays in sync
 * with the other 30-s-cadence surfaces (EPGGrid, HeroSpotlight).
 */
export function NowPlayingCard({
  channel,
  nowPlaying,
  upNext,
  now,
}: NowPlayingCardProps) {
  const { t } = useTranslation();

  if (!nowPlaying) {
    return (
      <div className="border-b border-tv-line p-4">
        <div className="text-[11px] font-semibold uppercase tracking-widest text-tv-fg-3">
          {t("liveTV.nowOnAir", { defaultValue: "Ahora en antena" })}
        </div>
        <div className="mt-2 text-sm text-tv-fg-2">
          {t("liveTV.noEPG", { defaultValue: "Sin guía disponible" })} — {channel.name}
        </div>
      </div>
    );
  }

  const start = new Date(nowPlaying.start_time).getTime();
  const end = new Date(nowPlaying.end_time).getTime();
  const durationMin = Math.max(1, Math.round((end - start) / 60_000));
  const progress = Math.max(
    0,
    Math.min(1, (now - start) / Math.max(end - start, 1)),
  );

  return (
    <div className="border-b border-tv-line p-4">
      <div className="text-[11px] font-semibold uppercase tracking-widest text-tv-fg-3">
        {t("liveTV.nowOnAir", { defaultValue: "Ahora en antena" })}
      </div>
      <h2 className="mt-1 text-lg font-semibold text-tv-fg-0">
        {nowPlaying.title}
      </h2>
      {nowPlaying.description && (
        <p className="mt-2 line-clamp-3 text-sm text-tv-fg-2">
          {nowPlaying.description}
        </p>
      )}
      <div className="mt-3 flex items-center gap-2 font-mono text-[11px] tabular-nums text-tv-fg-2">
        <span>
          {formatTime(nowPlaying.start_time)} –{" "}
          {formatTime(nowPlaying.end_time)}
        </span>
        <span className="h-1 w-1 rounded-full bg-tv-fg-3" aria-hidden="true" />
        <span>
          {durationMin} {t("liveTV.min", { defaultValue: "min" })}
        </span>
        {nowPlaying.category && (
          <>
            <span
              className="h-1 w-1 rounded-full bg-tv-fg-3"
              aria-hidden="true"
            />
            <span>{nowPlaying.category}</span>
          </>
        )}
      </div>

      <div className="mt-3 h-1 overflow-hidden rounded-full bg-tv-bg-3">
        <div
          className="h-full rounded-full bg-tv-accent transition-[width] duration-1000"
          style={{ width: `${progress * 100}%` }}
        />
      </div>

      {upNext && (
        <div className="mt-3 truncate text-[11px] text-tv-fg-3">
          <span className="mr-1.5 uppercase tracking-widest">
            {t("liveTV.upNextShort", { defaultValue: "Después" })}
          </span>
          <span className="font-mono tabular-nums text-tv-fg-2">
            {formatTime(upNext.start_time)}
          </span>
          <span className="ml-2 text-tv-fg-1">{upNext.title}</span>
        </div>
      )}
    </div>
  );
}
