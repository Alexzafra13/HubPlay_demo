import { useEffect, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { Channel, EPGProgram } from "@/api/types";
import { useNowTick } from "@/hooks/useNowTick";
import { StreamPreview } from "./StreamPreview";
import { getProgramProgress } from "./epgHelpers";

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
  /** Fired when the user explicitly asks to play (player area click
   * or the Reproducir CTA). Caller decides whether to escalate to
   * fullscreen. */
  onOpen?: (channel: Channel) => void;
  /** Optional favourites toggle wiring. When provided, a heart CTA
   * appears next to "Reproducir". */
  isFavorite?: boolean;
  onToggleFavorite?: (channelId: string) => void;
  /**
   * Optional page-level title block rendered as a top-left overlay on
   * the info column. Lets the page lift its `<h1>` + counts into the
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
  isFavorite,
  onToggleFavorite,
  headerOverlay,
  flushTop,
}: HeroSpotlightProps) {
  const { t } = useTranslation();
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

  // Live countdown to the end of the current programme. We can't
  // call `Date.now()` inline during render — that's an impure
  // function (linter rightly flags it) and the value would freeze
  // at last-render time, so the "X min left" label never actually
  // counts down without an unrelated re-render. Tick once a minute
  // via state + effect so the value is always fresh and React-aware.
  // Hoisted ABOVE the early return below so React's rules-of-hooks
  // sees the hook call on every render.
  const firstNowPlaying = items[0]?.nowPlaying ?? null;
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!firstNowPlaying) return;
    const id = setInterval(() => setNow(Date.now()), 60_000);
    return () => clearInterval(id);
  }, [firstNowPlaying]);

  if (items.length === 0) return null;

  const { channel, nowPlaying } = items[0];
  const progress = nowPlaying ? getProgramProgress(nowPlaying) : 0;
  const showPreview = previewArmed && !reducedMotion;

  // Editorial backdrop — a soft radial in the channel's brand colour
  // anchored on the neutral base. No video, no shimmer; the brand
  // hue does the work of "this channel".
  const bg = `radial-gradient(circle at 18% 12%, ${channel.logo_bg}99 0%, transparent 55%), radial-gradient(circle at 82% 88%, ${channel.logo_bg}55 0%, transparent 60%), linear-gradient(180deg, var(--tv-bg-2) 0%, var(--tv-bg-0) 100%)`;

  const minutesLeft = nowPlaying
    ? Math.max(
        0,
        Math.round((new Date(nowPlaying.end_time).getTime() - now) / 60_000),
      )
    : 0;

  // Info-column backdrop — same brand-tinted gradient as the player
  // box but rotated horizontally so the seam between the two columns
  // reads as one continuous surface.
  const infoBg = `linear-gradient(105deg, ${channel.logo_bg}33 0%, transparent 55%), linear-gradient(180deg, var(--tv-bg-2) 0%, var(--tv-bg-0) 100%)`;

  const playLabel = t("liveTV.play", { defaultValue: "Reproducir" });
  const playAria = nowPlaying
    ? `${playLabel} ${channel.name} — ${nowPlaying.title}`
    : `${playLabel} ${channel.name}`;

  return (
    <section
      aria-label={label}
      className={[
        "relative grid grid-cols-1 overflow-hidden border border-tv-line bg-tv-bg-1 lg:grid-cols-[minmax(360px,460px)_1fr]",
        flushTop ? "rounded-b-tv-lg border-t-0" : "rounded-tv-lg",
      ].join(" ")}
    >
      {/* Player area — clickable, escalates to fullscreen via onOpen.
          aria-label keeps the same `${name} — ${program}` shape so
          existing tests / screen readers find it the same way. */}
      <button
        type="button"
        onClick={() => onOpen?.(channel)}
        aria-label={
          nowPlaying ? `${channel.name} — ${nowPlaying.title}` : channel.name
        }
        className="group relative block aspect-video w-full overflow-hidden text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-tv-accent"
      >
        <div className="pointer-events-none absolute inset-0" style={{ background: bg }} />

        {/* Live preview — muted HLS of the actual stream. Keyed on
            channel.id so changes in the upstream selection remount a
            fresh HLS instance. Only rendered after the mount delay
            AND when reduced-motion is off; otherwise the gradient
            backdrop carries the surface alone. */}
        {showPreview ? (
          <StreamPreview
            key={channel.id}
            streamUrl={channel.stream_url}
            className="absolute inset-0 size-full object-cover"
          />
        ) : null}

        {/* Channel + program overlay top-left, pill style. */}
        <div className="pointer-events-none absolute inset-x-3 top-3 z-10 flex">
          <span className="max-w-full truncate rounded-tv-xs bg-black/65 px-2.5 py-1 text-[11.5px] font-medium text-white backdrop-blur">
            {channel.name}
            {nowPlaying ? ` — ${nowPlaying.title}` : ""}
          </span>
        </div>

        {/* Soft vignette so the play affordance reads. */}
        <div
          className="pointer-events-none absolute inset-0 bg-gradient-to-t from-black/40 via-transparent to-black/10"
          aria-hidden="true"
        />

        {/* Center play affordance — visible on hover/focus, lifts on
            interaction so the click target feels alive. The button
            itself is the parent <button>, this is just the visual. */}
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
          <span className="flex size-16 items-center justify-center rounded-full bg-black/45 ring-1 ring-white/30 backdrop-blur transition group-hover:scale-110 group-hover:bg-tv-accent/85 group-focus-visible:scale-110 group-focus-visible:bg-tv-accent/85">
            <PlayGlyph />
          </span>
        </div>
      </button>

      {/* Info column — informational, NOT a single button. CTAs are
          their own buttons so a click on the description text doesn't
          fire the play action. */}
      <div
        className="relative flex flex-col gap-2.5 p-5 md:gap-3 md:p-7"
        style={{ background: infoBg }}
      >
        {headerOverlay ? <div>{headerOverlay}</div> : null}

        <div className="flex flex-wrap items-center gap-2">
          <span className="rounded-tv-xs bg-accent/95 px-2.5 py-0.5 text-[10.5px] font-bold uppercase tracking-wider text-white shadow-sm">
            {label}
          </span>
          {channel.country ? (
            <span className="rounded-tv-xs bg-black/40 px-2 py-0.5 font-mono text-[10.5px] font-semibold uppercase tracking-wider text-tv-fg-1 backdrop-blur">
              {channel.country}
            </span>
          ) : null}
          <span className="font-mono text-[10.5px] uppercase tracking-widest text-tv-fg-2">
            CH {channel.number}
          </span>
        </div>

        <h2 className="text-2xl font-bold leading-tight text-tv-fg-0 md:text-3xl">
          {channel.name}
        </h2>

        {nowPlaying ? (
          <>
            <p className="text-base font-medium text-tv-fg-0 md:text-lg">
              {nowPlaying.title}
            </p>
            <div className="flex items-center gap-3">
              <span className="text-[12px] tabular-nums text-tv-fg-2">
                {t("liveTV.timeLeftMin", {
                  defaultValue: "Queda {{min}} min",
                  min: minutesLeft,
                })}
              </span>
              <div className="h-1 flex-1 max-w-[280px] overflow-hidden rounded-full bg-white/10">
                <div
                  className="h-full rounded-full bg-accent transition-[width] duration-1000 motion-reduce:transition-none"
                  style={{ width: `${progress}%` }}
                />
              </div>
            </div>
            {nowPlaying.description ? (
              <p className="line-clamp-3 max-w-2xl text-[13px] text-tv-fg-1 md:text-[13.5px]">
                {nowPlaying.description}
              </p>
            ) : null}
          </>
        ) : null}

        {/* CTAs */}
        <div className="mt-2 flex flex-wrap items-center gap-2">
          <button
            type="button"
            onClick={() => onOpen?.(channel)}
            aria-label={playAria}
            className="inline-flex items-center gap-2 rounded-full bg-accent px-4 py-2 text-[13px] font-semibold text-white shadow-md transition hover:opacity-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-tv-accent/60"
          >
            <PlayGlyph small />
            {playLabel}
          </button>
          {onToggleFavorite ? (
            <button
              type="button"
              aria-pressed={!!isFavorite}
              onClick={() => onToggleFavorite(channel.id)}
              className={[
                "inline-flex items-center gap-1.5 rounded-full border px-3 py-2 text-[13px] font-medium transition",
                isFavorite
                  ? "border-tv-accent/60 bg-tv-accent/15 text-tv-accent"
                  : "border-tv-line bg-tv-bg-1 text-tv-fg-1 hover:text-tv-fg-0",
              ].join(" ")}
            >
              <HeartGlyph filled={!!isFavorite} />
              {isFavorite
                ? t("liveTV.removeFromFavorites", {
                    defaultValue: "Quitar de favoritos",
                  })
                : t("liveTV.addToFavorites", {
                    defaultValue: "Añadir a favoritos",
                  })}
            </button>
          ) : null}
        </div>
      </div>
    </section>
  );
}

function PlayGlyph({ small }: { small?: boolean } = {}) {
  const size = small ? 14 : 22;
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="currentColor"
      aria-hidden="true"
    >
      <path d="M8 5v14l11-7z" />
    </svg>
  );
}

function HeartGlyph({ filled }: { filled: boolean }) {
  return (
    <svg
      width="13"
      height="13"
      viewBox="0 0 24 24"
      fill={filled ? "currentColor" : "none"}
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z" />
    </svg>
  );
}
