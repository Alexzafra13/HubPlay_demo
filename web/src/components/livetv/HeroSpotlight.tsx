import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Channel, EPGProgram } from "@/api/types";
import { ChannelLogo } from "./ChannelLogo";
import { StreamPreview } from "./StreamPreview";
import { formatTime, getProgramProgress } from "./epgHelpers";

export interface HeroSpotlightItem {
  channel: Channel;
  nowPlaying?: EPGProgram | null;
}

interface HeroSpotlightProps {
  /**
   * Items to feature. The first is shown on mount; when there are
   * more, the hero auto-rotates and exposes carousel dots to scrub.
   *
   * Returns nothing (renders null) when the list is empty — the
   * caller is responsible for deciding whether the hero should be
   * shown at all, based on the user's preference + whichever signal
   * actually yielded items.
   */
  items: HeroSpotlightItem[];
  /** Caption above the title, e.g. "Tu favorito" or "En directo ahora". */
  label: string;
  onOpen?: (channel: Channel) => void;
}

/** How long each hero slide stays visible before auto-advancing. */
const ROTATE_MS = 12_000;

/**
 * HeroSpotlight — the top-of-Discover focal point.
 *
 * Presentational only. The signal that populates `items` and the
 * persisted "hero mode" preference live in the parent (LiveTV.tsx +
 * HeroSettings in the topbar). Keeping this component dumb means
 * the same shape can be driven from favorites, live-now, or any
 * future signal without touching the layout.
 *
 * Renders:
 *   - One large tile at the current index, with a muted HLS
 *     auto-preview so the page feels alive on landing.
 *   - Carousel dots when items.length > 1.
 *   - Nothing (null) when items.length === 0.
 */
export function HeroSpotlight({ items, label, onOpen }: HeroSpotlightProps) {
  const { t } = useTranslation();
  const [rawIdx, setIdx] = useState(0);

  // Clamp the index at render time (rather than setState-in-effect,
  // which the lint rightly objects to). Handles the "items shrank"
  // case (user unfavorited the currently-displayed slide).
  const idx = items.length === 0 ? 0 : rawIdx % items.length;

  // Auto-rotation — pauses on a single slide to avoid pointless
  // re-renders.
  useEffect(() => {
    if (items.length < 2) return;
    const timer = window.setInterval(() => {
      setIdx((i) => (i + 1) % items.length);
    }, ROTATE_MS);
    return () => window.clearInterval(timer);
  }, [items.length]);

  if (items.length === 0) return null;
  const { channel, nowPlaying } = items[idx];
  const progress = nowPlaying ? getProgramProgress(nowPlaying) : 0;

  // Backdrop tuned brighter than a ChannelCard — hero deserves a
  // stronger presence — but still anchored on the neutral base so
  // the spotlight doesn't clash with the rails below.
  const bg = `radial-gradient(circle at 20% 10%, ${channel.logo_bg}66 0%, transparent 60%), linear-gradient(180deg, var(--tv-bg-2) 0%, var(--tv-bg-0) 100%)`;

  return (
    <section aria-label={label} className="relative">
      <button
        type="button"
        onClick={() => onOpen?.(channel)}
        className="group relative block aspect-[21/9] w-full overflow-hidden rounded-tv-lg border border-tv-line text-left transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-tv-accent md:aspect-[24/9]"
        aria-label={
          nowPlaying
            ? `${channel.name} — ${nowPlaying.title}`
            : channel.name
        }
      >
        <div
          className="pointer-events-none absolute inset-0"
          style={{ background: bg }}
        />

        {/* Auto-preview. Keyed on channel.id so switching slides
            dismounts the old HLS and mounts a fresh instance — no
            state leakage, no stale stream. */}
        <StreamPreview
          key={channel.id}
          streamUrl={channel.stream_url}
          className="absolute inset-0 h-full w-full object-cover opacity-80"
        />

        {/* Dark vignette so the caption stays readable over any video
            content. Always on, even when preview fails. */}
        <div
          className="pointer-events-none absolute inset-0 bg-gradient-to-t from-black/80 via-black/30 to-black/20"
          aria-hidden="true"
        />

        {/* Top meta row: label + LIVE pill + country. */}
        <div className="pointer-events-none absolute inset-x-5 top-5 flex items-center gap-2">
          <span className="rounded-tv-xs bg-tv-accent/90 px-2 py-0.5 text-[11px] font-bold uppercase tracking-wider text-tv-accent-ink">
            {label}
          </span>
          {nowPlaying && (
            <span className="flex items-center gap-1 rounded-tv-xs bg-tv-live/90 px-2 py-0.5 text-[11px] font-bold uppercase tracking-wider text-white">
              <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-white" />
              Live
            </span>
          )}
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
                <span className="mr-1.5 uppercase tracking-wider text-tv-fg-3">
                  Ahora
                </span>
                {nowPlaying.title}
              </div>
              <div className="flex max-w-xl items-center gap-2">
                <div className="h-1 flex-1 overflow-hidden rounded-full bg-white/10">
                  <div
                    className="h-full rounded-full bg-tv-accent transition-[width] duration-1000"
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

      {/* Carousel dots — only render when there's more than one slide. */}
      {items.length > 1 ? (
        <div
          className="mt-3 flex items-center justify-center gap-2"
          role="tablist"
          aria-label={t("liveTV.hero.dots", {
            defaultValue: "Elegir destacado",
          })}
        >
          {items.map((item, i) => (
            <button
              key={item.channel.id}
              type="button"
              role="tab"
              aria-selected={i === idx}
              aria-label={t("liveTV.hero.goTo", {
                defaultValue: "Ir a {{name}}",
                name: item.channel.name,
              })}
              onClick={() => setIdx(i)}
              className={[
                "h-1.5 rounded-full transition-all",
                i === idx
                  ? "w-8 bg-tv-accent"
                  : "w-2 bg-tv-line hover:bg-tv-line-strong",
              ].join(" ")}
            />
          ))}
        </div>
      ) : null}
    </section>
  );
}
