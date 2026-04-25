import { useEffect, useRef, useState } from "react";
import type { Channel, EPGProgram } from "@/api/types";
import { ChannelLogo } from "./ChannelLogo";
import { StreamPreview } from "./StreamPreview";
import { formatTime, getProgramProgress } from "./epgHelpers";

interface ChannelCardProps {
  channel: Channel;
  nowPlaying?: EPGProgram | null;
  upNext?: EPGProgram | null;
  isFavorite?: boolean;
  onClick?: () => void;
  onToggleFavorite?: () => void;
  /**
   * When true (default), hovering the card for ~250 ms attaches a muted
   * HLS preview of the live stream over the thumbnail. Auto-stops after
   * 15 s even if the cursor stays put, so an idle hover doesn't burn
   * bandwidth forever. Set to false to disable previews entirely.
   */
  previewOnHover?: boolean;
  /**
   * Renders the card in a "the channel is off the air" treatment: low
   * opacity, desaturated logo, an "Apagado" badge where LIVE usually
   * sits, no hover preview. Click still works so the admin can probe
   * if the upstream came back.
   */
  dimmed?: boolean;
}

/**
 * ChannelCard — the unit of the Live TV "Discover" rails.
 *
 * Layout deliberately separates the "poster" (the thumbnail, which is
 * the visual card) from the metadata (text on the page background
 * below). Earlier versions wrapped both in one bordered container
 * which produced a busy, "boxed-in" look — every card shouted
 * "I'M A TILE". Stripping the outer wrapper makes the rails read
 * like YouTube / Netflix / Plex: the poster carries the weight, the
 * text is quiet support.
 *
 * Thumbnail:
 *   - Dark neutral backdrop with a subtle radial of the channel's
 *     brand colour at the top; logo centred.
 *   - On hover (opt-in via `previewOnHover`): a muted `<video>` fades
 *     in over the logo with a real HLS preview. Capped at 15 s.
 *   - Overlays: CH number + state pill (Live / Apagado) top-left,
 *     favorite heart top-right.
 *   - Bottom of the thumbnail: a thin progress bar for the EPG
 *     "now on air" remaining time (hidden when no EPG).
 *
 * Body (below thumbnail, on page background):
 *   - Channel name.
 *   - "Ahora {title}" / "Sin guía disponible" / "Apagado".
 *   - Up-next line (time + title) when EPG has it.
 */
export function ChannelCard({
  channel,
  nowPlaying,
  upNext,
  isFavorite = false,
  onClick,
  onToggleFavorite,
  previewOnHover = true,
  dimmed = false,
}: ChannelCardProps) {
  const progress = nowPlaying ? getProgramProgress(nowPlaying) : 0;

  // Thumbnail backdrop — neutral dark base with a whisper of the
  // channel's brand colour at the top so each tile keeps a hint of
  // identity without fighting its neighbours.
  const thumbBg = `radial-gradient(circle at 50% 20%, ${channel.logo_bg}33 0%, transparent 55%), linear-gradient(180deg, var(--tv-bg-2) 0%, var(--tv-bg-1) 100%)`;

  // Track logo-load failures per-URL so a broken upstream falls back to
  // the initials avatar instead of leaving an empty gradient. We key on
  // the URL itself (not a boolean) so a new channel whose logo happens
  // to share the broken URL doesn't auto-hide, and so a fresh URL resets
  // the state at render time — no setState-in-effect plumbing.
  const [failedLogoUrl, setFailedLogoUrl] = useState<string | null>(null);
  const showLogoImg =
    !!channel.logo_url && failedLogoUrl !== channel.logo_url;

  // Hover-preview state. Debounced so a rapid cursor fly-by doesn't
  // spin up HLS; the 250 ms dwell is short enough that a deliberate
  // hover feels instant but a pass-through doesn't trigger.
  const [armed, setArmed] = useState(false);
  const hoverTimer = useRef<number | null>(null);
  const autoStopTimer = useRef<number | null>(null);

  const clearTimers = () => {
    if (hoverTimer.current !== null) {
      window.clearTimeout(hoverTimer.current);
      hoverTimer.current = null;
    }
    if (autoStopTimer.current !== null) {
      window.clearTimeout(autoStopTimer.current);
      autoStopTimer.current = null;
    }
  };

  useEffect(() => () => clearTimers(), []);

  const handleMouseEnter = () => {
    if (!previewOnHover || dimmed) return;
    clearTimers();
    hoverTimer.current = window.setTimeout(() => {
      setArmed(true);
      autoStopTimer.current = window.setTimeout(() => setArmed(false), 15_000);
    }, 250);
  };

  const handleMouseLeave = () => {
    clearTimers();
    setArmed(false);
  };

  return (
    <button
      type="button"
      onClick={onClick}
      onMouseEnter={handleMouseEnter}
      onMouseLeave={handleMouseLeave}
      className={[
        "group flex w-full flex-col gap-2 text-left focus-visible:outline-none",
        dimmed ? "opacity-60 grayscale hover:opacity-90 transition" : "",
      ]
        .filter(Boolean)
        .join(" ")}
      aria-label={
        dimmed
          ? `${channel.name} — apagado`
          : nowPlaying
            ? `${channel.name} — ${nowPlaying.title}`
            : channel.name
      }
    >
      {/* Poster thumbnail — the visual card.
          Border + rounded corners + overflow-hidden live here (not on
          the outer button) so only the poster looks like a tile; the
          text below sits on the page background. */}
      <div className="relative aspect-[16/9] w-full overflow-hidden rounded-tv-md border border-tv-line bg-tv-bg-1 transition group-hover:-translate-y-0.5 group-hover:border-tv-line-strong group-hover:shadow-tv-lg group-focus-visible:ring-2 group-focus-visible:ring-tv-accent">
        <div
          className="pointer-events-none absolute inset-0"
          style={{ background: thumbBg }}
        />

        {/* Centered logo — primary visual when no preview is playing. */}
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center p-6">
          {showLogoImg ? (
            <img
              src={channel.logo_url!}
              alt=""
              className="max-h-full max-w-full object-contain drop-shadow-[0_2px_8px_rgba(0,0,0,0.5)]"
              loading="lazy"
              onError={() => setFailedLogoUrl(channel.logo_url ?? null)}
            />
          ) : (
            <ChannelLogo
              logoUrl={null}
              initials={channel.logo_initials}
              bg={channel.logo_bg}
              fg={channel.logo_fg}
              name={channel.name}
              className="h-14 w-14 rounded-tv-md ring-2 ring-white/10"
              textClassName="text-base font-bold"
            />
          )}
        </div>

        {/* Hover preview (mounts only when armed). pointer-events-none
            on the <video> lets mouse events fall through to the
            button — without it the cursor would "leave" the button
            as soon as the video loads and the preview would instantly
            cancel. */}
        {armed && <StreamPreview streamUrl={channel.stream_url} />}

        {/* Top-left badges. Order: CH number → state pill → country. */}
        <div className="pointer-events-none absolute left-2 top-2 flex max-w-[calc(100%-3rem)] items-center gap-1.5">
          <span className="rounded-tv-xs bg-black/50 px-1.5 py-0.5 font-mono text-[10px] font-semibold tracking-wider text-tv-fg-0 backdrop-blur">
            CH {channel.number}
          </span>
          {dimmed ? (
            <span className="rounded-tv-xs bg-black/70 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wider text-tv-fg-1 backdrop-blur">
              Apagado
            </span>
          ) : channel.health_status === "dead" ? (
            <span className="rounded-tv-xs bg-red-600/85 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wider text-white backdrop-blur">
              Sin señal
            </span>
          ) : channel.health_status === "degraded" ? (
            <span className="rounded-tv-xs bg-amber-500/85 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wider text-black backdrop-blur">
              Inestable
            </span>
          ) : nowPlaying ? (
            <span className="flex items-center gap-1 rounded-tv-xs bg-tv-live/90 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wider text-white">
              <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-white" />
              Live
            </span>
          ) : null}
          {channel.country && (
            <span className="truncate rounded-tv-xs bg-black/50 px-1.5 py-0.5 font-mono text-[10px] font-semibold uppercase tracking-wider text-tv-fg-1 backdrop-blur">
              {channel.country}
            </span>
          )}
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
              "absolute right-2 top-2 flex h-7 w-7 cursor-pointer items-center justify-center rounded-full bg-black/50 backdrop-blur transition hover:bg-black/70",
              isFavorite ? "text-tv-live" : "text-tv-fg-1",
            ].join(" ")}
          >
            <HeartIcon filled={isFavorite} />
          </span>
        )}

        {/* Progress bar, thin, hugging the bottom of the poster. Inside
            the thumbnail (not below) so the text region stays clean
            prose. Only renders when EPG knows how far along we are. */}
        {nowPlaying && !dimmed ? (
          <div
            className="pointer-events-none absolute inset-x-0 bottom-0 h-0.5 bg-black/40"
            aria-hidden="true"
          >
            <div
              className="h-full bg-tv-accent transition-[width] duration-1000"
              style={{ width: `${progress}%` }}
            />
          </div>
        ) : null}
      </div>

      {/* Body — page-background text, no wrapper. */}
      <div className="flex min-w-0 flex-col gap-0.5 px-0.5">
        <div className="truncate text-sm font-semibold text-tv-fg-0">
          {channel.name}
        </div>

        <div className="truncate text-xs text-tv-fg-2">
          {dimmed ? (
            <span className="text-tv-fg-3">
              Canal apagado · reintenta más tarde
            </span>
          ) : nowPlaying ? (
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

        {upNext && !dimmed ? (
          <div className="truncate text-[11px] text-tv-fg-3">
            <span className="mr-1.5 font-mono tabular-nums text-tv-fg-2">
              {formatTime(upNext.start_time)}
            </span>
            {upNext.title}
          </div>
        ) : null}
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
