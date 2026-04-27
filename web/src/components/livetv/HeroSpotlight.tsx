import { useEffect, useState, type ReactNode } from "react";
import type { Channel, EPGProgram } from "@/api/types";
import { useNowTick } from "@/hooks/useNowTick";
import { ChannelLogo } from "./ChannelLogo";
import { StreamPreview } from "./StreamPreview";
import { formatTime, getProgramProgress } from "./epgHelpers";

/**
 * Lazy-mount window for the live preview so a fast page load isn't
 * accompanied by an HLS request firing in the same tick. Gives the
 * shell a moment to settle and dampens the network spike on cold
 * landings.
 */
const PREVIEW_DELAY_MS = 800;

/**
 * Reads the current `prefers-reduced-motion` setting. Subscribes to
 * the media query so a user toggling the OS setting at runtime gets
 * a live update without a refresh. The query API matches `useIsMobile`
 * elsewhere in the codebase — useSyncExternalStore would be slightly
 * cleaner but a tiny effect is fine here for one consumer.
 */
function usePrefersReducedMotion(): boolean {
  const [prefers, setPrefers] = useState(() => {
    if (typeof window === "undefined") return false;
    return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  });
  useEffect(() => {
    if (typeof window === "undefined") return;
    const mql = window.matchMedia("(prefers-reduced-motion: reduce)");
    const onChange = () => setPrefers(mql.matches);
    mql.addEventListener("change", onChange);
    return () => mql.removeEventListener("change", onChange);
  }, []);
  return prefers;
}

export interface HeroSpotlightItem {
  channel: Channel;
  nowPlaying?: EPGProgram | null;
}

interface HeroSpotlightProps {
  /**
   * Items to feature. The hero is editorial — it shows the first item
   * and stops there. No carousel rotation, no dots, no live preview
   * loop. A separate "Ahora en directo" rail covers the inmediacy
   * angle, leaving the hero to do one job: sell discovery for the
   * single channel chosen by the parent's strategy.
   */
  items: HeroSpotlightItem[];
  /** Caption above the title, e.g. "Tu favorito" or "En directo ahora". */
  label: string;
  onOpen?: (channel: Channel) => void;
  /**
   * Optional page-level title block rendered as a top-left overlay on
   * the hero card. Lets the page lift its `<h1>` + counts into the
   * hero so the hero itself hugs the top of the layout instead of
   * sitting under a separate page header.
   */
  headerOverlay?: ReactNode;
  /** Drop the top-rounded corners so the hero can sit flush against
   * the global TopBar. The bottom corners stay rounded. */
  flushTop?: boolean;
}

/**
 * HeroSpotlight — the top-of-Discover focal point.
 *
 * Editorial card. The signal that picks the channel and the persisted
 * "hero mode" preference live in the parent (LiveTV.tsx +
 * useHeroSpotlight). Keeping this component dumb means the same shape
 * can be driven from favorites, live-now, or any future signal without
 * touching the layout.
 *
 * Why no auto-rotate, no carousel dots?
 *   1. Two competing motions (carousel rotation + autoplay HLS) were
 *      fighting for the user's eye. We kept the live preview because
 *      seeing the channel actually airing is a real signal — but
 *      removed the rotation so the user is never wrong-footed by a
 *      slide changing while they're moving the cursor.
 *   2. Inmediacy ("what's on right now across all channels") moved to
 *      a dedicated "Ahora" tab so the hero stays editorial.
 *   3. The preview honours `prefers-reduced-motion` — under that
 *      setting we drop the HLS autoplay entirely and lean on the
 *      brand-tinted gradient backdrop alone.
 */
export function HeroSpotlight({
  items,
  label,
  onOpen,
  headerOverlay,
  flushTop,
}: HeroSpotlightProps) {
  // Keep the progress bar advancing without the parent re-rendering
  // every second — the tick is hoisted here (rather than in a child)
  // so the explicit-deps useMemo's downstream stay stable.
  useNowTick(30_000);
  const reducedMotion = usePrefersReducedMotion();

  // Lazy-mount preview after a short delay so a fresh page load
  // doesn't fire an HLS request in the same tick the shell paints.
  // Set to true immediately when reduced-motion is on (we won't
  // render the preview anyway, but the gate stays simple).
  const [previewArmed, setPreviewArmed] = useState(false);
  useEffect(() => {
    if (reducedMotion) return;
    const id = window.setTimeout(() => setPreviewArmed(true), PREVIEW_DELAY_MS);
    return () => window.clearTimeout(id);
  }, [reducedMotion]);

  if (items.length === 0) return null;

  const { channel, nowPlaying } = items[0];
  const progress = nowPlaying ? getProgramProgress(nowPlaying) : 0;
  const showPreview = previewArmed && !reducedMotion;

  // Editorial backdrop — a soft radial in the channel's brand colour
  // anchored on the neutral base. No video, no shimmer; the brand
  // hue does the work of "this channel".
  const bg = `radial-gradient(circle at 18% 12%, ${channel.logo_bg}99 0%, transparent 55%), radial-gradient(circle at 82% 88%, ${channel.logo_bg}55 0%, transparent 60%), linear-gradient(180deg, var(--tv-bg-2) 0%, var(--tv-bg-0) 100%)`;

  return (
    <section aria-label={label} className="relative">
      <button
        type="button"
        onClick={() => onOpen?.(channel)}
        className={[
          "group relative block aspect-[21/9] w-full max-h-[420px] overflow-hidden border border-tv-line text-left transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-tv-accent md:aspect-[32/9]",
          flushTop ? "rounded-b-tv-lg border-t-0" : "rounded-tv-lg",
        ].join(" ")}
        aria-label={
          nowPlaying ? `${channel.name} — ${nowPlaying.title}` : channel.name
        }
      >
        <div
          className="pointer-events-none absolute inset-0"
          style={{ background: bg }}
        />

        {/* Live preview — muted HLS of the actual stream. Keyed on
            channel.id so changes in the upstream selection remount a
            fresh HLS instance (no state leak). Only rendered after the
            mount delay AND when reduced-motion is off; otherwise the
            gradient backdrop carries the surface alone. */}
        {showPreview ? (
          <StreamPreview
            key={channel.id}
            streamUrl={channel.stream_url}
            className="absolute inset-0 h-full w-full object-cover opacity-90"
          />
        ) : null}

        {/* Vignette over the (preview or gradient) backdrop so the
            caption stays readable on bright frames. Always on. */}
        <div
          className="pointer-events-none absolute inset-0 bg-gradient-to-t from-black/80 via-black/30 to-black/10"
          aria-hidden="true"
        />

        {/* Top overlay — page title (when supplied). The mode label +
            country pills sit below it. Whole stack is
            pointer-events-none so the underlying button still receives
            the click. */}
        {headerOverlay ? (
          <div className="pointer-events-none absolute inset-x-5 top-4 md:top-5">
            {headerOverlay}
          </div>
        ) : null}
        <div
          className={[
            "pointer-events-none absolute inset-x-5 flex items-center gap-2",
            headerOverlay ? "top-[5.5rem] md:top-24" : "top-5",
          ].join(" ")}
        >
          {/* Mode label — single explanation of *why* this channel is
              featured. Live status lives on the page-level h1's
              pulsating dot, so no separate LIVE pill here. */}
          <span className="rounded-tv-xs bg-accent/95 px-2.5 py-0.5 text-[11px] font-bold uppercase tracking-wider text-white shadow-md">
            {label}
          </span>
          {channel.country && (
            <span className="rounded-tv-xs bg-black/40 px-2 py-0.5 font-mono text-[11px] font-semibold uppercase tracking-wider text-tv-fg-1 backdrop-blur">
              {channel.country}
            </span>
          )}
        </div>

        {/* Bottom caption. */}
        <div className="absolute inset-x-5 bottom-5 flex flex-col gap-3">
          <div className="flex items-end gap-3">
            <ChannelLogo
              logoUrl={channel.logo_url}
              initials={channel.logo_initials}
              bg={channel.logo_bg}
              fg={channel.logo_fg}
              name={channel.name}
              className="h-14 w-14 rounded-tv-md ring-2 ring-white/10 shadow-lg"
              textClassName="text-base font-bold"
            />
            <div className="min-w-0 flex-1">
              <div className="font-mono text-[11px] uppercase tracking-widest text-tv-fg-2">
                CH {channel.number}
              </div>
              <div className="truncate text-xl font-semibold text-tv-fg-0 md:text-2xl">
                {channel.name}
              </div>
            </div>
          </div>

          {nowPlaying ? (
            <>
              <div className="line-clamp-2 max-w-3xl text-sm text-tv-fg-1 md:text-base">
                {nowPlaying.title}
              </div>
              <div className="flex max-w-xl items-center gap-2">
                <div className="h-1 flex-1 overflow-hidden rounded-full bg-white/10">
                  <div
                    className="h-full rounded-full bg-accent transition-[width] duration-1000 motion-reduce:transition-none"
                    style={{ width: `${progress}%` }}
                  />
                </div>
                <span className="font-mono text-[11px] tabular-nums text-tv-fg-2">
                  {formatTime(nowPlaying.end_time)}
                </span>
              </div>
            </>
          ) : null}
        </div>
      </button>
    </section>
  );
}
