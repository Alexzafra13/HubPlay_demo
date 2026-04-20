import type { Channel, EPGProgram } from "@/api/types";
import { ChannelLogo } from "./ChannelLogo";
import { formatTime, getProgramProgress } from "./epgHelpers";

interface ChannelCardProps {
  channel: Channel;
  nowPlaying?: EPGProgram | null;
  upNext?: EPGProgram | null;
  isFavorite?: boolean;
  onClick?: () => void;
  onToggleFavorite?: () => void;
}

/**
 * ChannelCard — the unit of the Live TV "Discover" rails.
 *
 * Layout (top to bottom):
 *   1. Thumbnail box (16:9). A live-preview gradient derived from the
 *      channel's logo color stands in for a real preview frame — cheap,
 *      deterministic, and matches the design system until we introduce
 *      real stream thumbnails.
 *   2. Channel number + LIVE chip (top-left) and favorite toggle (top-right).
 *   3. Logo badge (bottom-left), overlapping the thumbnail edge.
 *   4. Body: name + now-playing + progress + up-next time/title.
 *
 * Tokens come from the TV theme scope (`data-theme="tv"` on an ancestor).
 */
export function ChannelCard({
  channel,
  nowPlaying,
  upNext,
  isFavorite = false,
  onClick,
  onToggleFavorite,
}: ChannelCardProps) {
  const progress = nowPlaying ? getProgramProgress(nowPlaying) : 0;

  // Match the prototype's "preview" gradient: bias toward the logo hue so
  // each card reads as a distinct channel even without a real preview.
  const thumbBg = `linear-gradient(135deg, ${channel.logo_bg}cc 0%, var(--tv-bg-2) 120%)`;

  return (
    <button
      type="button"
      onClick={onClick}
      className="group flex w-full flex-col overflow-hidden rounded-tv-md border border-tv-line bg-tv-bg-1 text-left transition hover:-translate-y-0.5 hover:border-tv-line-strong hover:shadow-tv-lg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-tv-accent"
      aria-label={
        nowPlaying ? `${channel.name} — ${nowPlaying.title}` : channel.name
      }
    >
      {/* Thumbnail ---------------------------------------------------- */}
      <div className="relative aspect-[16/9] w-full overflow-hidden">
        <div className="absolute inset-0" style={{ background: thumbBg }} />

        {/* Top-left: channel number + LIVE pill */}
        <div className="absolute left-2 top-2 flex items-center gap-1.5">
          <span className="rounded-tv-xs bg-black/40 px-1.5 py-0.5 text-[10px] font-mono font-semibold tracking-wider text-tv-fg-0 backdrop-blur">
            CH {channel.number}
          </span>
          <span className="flex items-center gap-1 rounded-tv-xs bg-tv-live/90 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wider text-white">
            <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-white" />
            Live
          </span>
        </div>

        {/* Top-right: favorite heart */}
        {onToggleFavorite && (
          <span
            role="button"
            tabIndex={0}
            aria-label={
              isFavorite ? "Quitar de favoritos" : "Añadir a favoritos"
            }
            aria-pressed={isFavorite}
            onClick={(e) => {
              e.stopPropagation();
              onToggleFavorite();
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                e.stopPropagation();
                onToggleFavorite();
              }
            }}
            className={[
              "absolute right-2 top-2 flex h-7 w-7 cursor-pointer items-center justify-center rounded-full bg-black/40 backdrop-blur transition hover:bg-black/60",
              isFavorite ? "text-tv-live" : "text-tv-fg-1",
            ].join(" ")}
          >
            <HeartIcon filled={isFavorite} />
          </span>
        )}

        {/* Bottom-left: logo badge, overlapping */}
        <div className="absolute -bottom-3 left-3">
          <ChannelLogo
            logoUrl={channel.logo_url}
            initials={channel.logo_initials}
            bg={channel.logo_bg}
            fg={channel.logo_fg}
            name={channel.name}
            className="h-10 w-10 rounded-tv-sm ring-2 ring-tv-bg-1"
            textClassName="text-[11px] font-bold"
          />
        </div>
      </div>

      {/* Body --------------------------------------------------------- */}
      <div className="flex flex-col gap-1.5 px-3 pb-3 pt-5">
        <div className="truncate text-sm font-semibold text-tv-fg-0">
          {channel.name}
        </div>

        <div className="truncate text-xs text-tv-fg-2">
          {nowPlaying ? (
            <>
              <span className="mr-1.5 uppercase tracking-wider text-tv-fg-3">
                Ahora
              </span>
              {nowPlaying.title}
            </>
          ) : (
            <span className="text-tv-fg-3">Sin guía disponible</span>
          )}
        </div>

        {/* Progress */}
        <div className="h-1 w-full overflow-hidden rounded-full bg-tv-bg-3">
          <div
            className="h-full rounded-full bg-tv-accent transition-[width] duration-1000"
            style={{ width: `${progress}%` }}
          />
        </div>

        {/* Up next */}
        <div className="truncate text-[11px] text-tv-fg-3">
          {upNext ? (
            <>
              <span className="mr-1.5 font-mono tabular-nums text-tv-fg-2">
                {formatTime(upNext.start_time)}
              </span>
              {upNext.title}
            </>
          ) : (
            <span>&nbsp;</span>
          )}
        </div>
      </div>
    </button>
  );
}

function HeartIcon({ filled }: { filled: boolean }) {
  return (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill={filled ? "currentColor" : "none"}
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z" />
    </svg>
  );
}
