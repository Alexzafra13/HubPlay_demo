import { useEffect, useRef, useState } from "react";
import Hls from "hls.js";
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
  /**
   * When true (default), hovering the card for 500 ms attaches a muted HLS
   * preview of the live stream over the thumbnail. Auto-stops after 15 s
   * even if the cursor stays put, so an idle hover doesn't burn bandwidth
   * forever. Set to false to disable previews entirely (faster, zero cost).
   */
  previewOnHover?: boolean;
}

/**
 * ChannelCard — the unit of the Live TV "Discover" rails.
 *
 * Thumbnail layout:
 *   - Backdrop: a gradient tinted by the channel's deterministic logo color
 *     (cheap, stable visual identity even when the real logo is missing).
 *   - Foreground: the real `logo_url` centered, large. Falls back to
 *     initials-on-color via ChannelLogo when the image is missing or
 *     404s — no layout shift either way.
 *   - On hover (opt-in via `previewOnHover`): a muted `<video>` fades in
 *     over the logo with a real HLS preview of the channel. Capped at 15 s.
 *
 * Overlays (always on top of the thumbnail):
 *   - Top-left:  channel number + LIVE pill
 *   - Top-right: favorite toggle (if `onToggleFavorite` provided)
 *
 * Body (below the thumbnail):
 *   - Name (bold)
 *   - "Ahora {title}" from nowPlaying, or "Sin guía disponible"
 *   - EPG progress bar
 *   - Up-next time + title
 */
export function ChannelCard({
  channel,
  nowPlaying,
  upNext,
  isFavorite = false,
  onClick,
  onToggleFavorite,
  previewOnHover = true,
}: ChannelCardProps) {
  const progress = nowPlaying ? getProgramProgress(nowPlaying) : 0;

  // Backdrop gradient: biased toward the channel's logo hue so cards still
  // read as distinct even before the real logo image resolves.
  const thumbBg = `linear-gradient(135deg, ${channel.logo_bg}aa 0%, var(--tv-bg-2) 120%)`;

  // Hover-preview state -------------------------------------------------
  // `armed` is flipped on after the debounce fires; `<video>` mounts only
  // when `armed` is true, so no HLS traffic starts until the user actually
  // dwells on the card.
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
    if (!previewOnHover) return;
    clearTimers();
    // 500 ms dwell before arming — filters rapid cursor pass-throughs.
    hoverTimer.current = window.setTimeout(() => {
      setArmed(true);
      // Absolute safety net: 15 s max preview per hover session.
      autoStopTimer.current = window.setTimeout(() => setArmed(false), 15_000);
    }, 500);
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
      className="group flex w-full flex-col overflow-hidden rounded-tv-md border border-tv-line bg-tv-bg-1 text-left transition hover:-translate-y-0.5 hover:border-tv-line-strong hover:shadow-tv-lg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-tv-accent"
      aria-label={
        nowPlaying ? `${channel.name} — ${nowPlaying.title}` : channel.name
      }
    >
      {/* Thumbnail ---------------------------------------------------- */}
      <div className="relative aspect-[16/9] w-full overflow-hidden">
        <div className="absolute inset-0" style={{ background: thumbBg }} />

        {/* Centered logo — primary visual when no preview is playing. */}
        <div className="absolute inset-0 flex items-center justify-center p-6">
          {channel.logo_url ? (
            <img
              src={channel.logo_url}
              alt=""
              className="max-h-full max-w-full object-contain drop-shadow-[0_2px_8px_rgba(0,0,0,0.5)]"
              loading="lazy"
              onError={(e) => {
                // Hide the broken image; the initials fallback underneath
                // (rendered below as a sibling) becomes visible instead.
                e.currentTarget.style.display = "none";
              }}
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

        {/* Hover preview (mounts only when armed). */}
        {armed && (
          <HoverPreview
            streamUrl={channel.stream_url}
            onUnmount={() => {
              /* noop */
            }}
          />
        )}

        {/* Top-left: channel number + LIVE pill */}
        <div className="pointer-events-none absolute left-2 top-2 flex items-center gap-1.5">
          <span className="rounded-tv-xs bg-black/50 px-1.5 py-0.5 font-mono text-[10px] font-semibold tracking-wider text-tv-fg-0 backdrop-blur">
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
              "absolute right-2 top-2 flex h-7 w-7 cursor-pointer items-center justify-center rounded-full bg-black/50 backdrop-blur transition hover:bg-black/70",
              isFavorite ? "text-tv-live" : "text-tv-fg-1",
            ].join(" ")}
          >
            <HeartIcon filled={isFavorite} />
          </span>
        )}
      </div>

      {/* Body --------------------------------------------------------- */}
      <div className="flex flex-col gap-1.5 px-3 pb-3 pt-3">
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

// ───────────────────────────────────────────────────────────────────
// HoverPreview
// ───────────────────────────────────────────────────────────────────
//
// Dedicated lightweight HLS attacher for the card hover preview. Distinct
// from `useLiveHls` — no loading chrome, no error UI, silent failure. Uses
// a tiny buffer (≤ 8 s) so a short hover doesn't pull minutes of segments,
// and runs muted/playsInline so iOS Safari accepts the autoplay.

interface HoverPreviewProps {
  streamUrl: string;
  onUnmount: () => void;
}

function HoverPreview({ streamUrl, onUnmount }: HoverPreviewProps) {
  const videoRef = useRef<HTMLVideoElement>(null);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    let hls: Hls | null = null;
    if (Hls.isSupported()) {
      hls = new Hls({
        enableWorker: true,
        lowLatencyMode: false,
        maxBufferLength: 8,
        maxMaxBufferLength: 10,
        backBufferLength: 0,
        // Silent network retry — short budget, no UI feedback.
        manifestLoadingMaxRetry: 1,
        levelLoadingMaxRetry: 1,
        fragLoadingMaxRetry: 1,
        xhrSetup: (xhr) => {
          xhr.withCredentials = true;
        },
      });
      hls.loadSource(streamUrl);
      hls.attachMedia(video);
      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        video.play().catch(() => {});
      });
      hls.on(Hls.Events.ERROR, (_event, data) => {
        if (data.fatal) hls?.destroy();
      });
    } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
      video.src = streamUrl;
      video.play().catch(() => {});
    }

    return () => {
      if (hls) hls.destroy();
      video.removeAttribute("src");
      video.load();
      onUnmount();
    };
  }, [streamUrl, onUnmount]);

  return (
    <video
      ref={videoRef}
      muted
      playsInline
      autoPlay
      className="absolute inset-0 h-full w-full object-cover"
    />
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
