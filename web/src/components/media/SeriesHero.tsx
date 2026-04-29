import { useEffect, useRef, useState, type FC, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { MediaItem } from "@/api/types";
import { Badge } from "@/components/common/Badge";
import { useVibrantColors } from "@/hooks/useVibrantColors";
import type { HeroMenuItem } from "./HeroSection";
import type { SeriesResumeMode } from "@/hooks/useSeriesResumeTarget";

interface SeriesHeroProps {
  item: MediaItem;
  resumeMode: SeriesResumeMode;
  resumeLabel: ReactNode; // "Reproducir" or "Seguir viendo S01E03"
  resumeProgressPercent: number | null;
  onPlay: () => void;
  onToggleFavorite: () => void;
  isFavorite: boolean;
  menuItems: HeroMenuItem[];
}

function formatRating(rating: number): string {
  return rating.toFixed(1);
}

/**
 * SeriesHero — full-bleed hero for the series detail page.
 *
 * Layout, in order of stacking from bottom up:
 *
 *   1. Backdrop image, full-width, right-aligned crop. Uses absolute
 *      inset-0 so it spans from the sidebar edge to the viewport's
 *      right edge. The wrapper bleeds out of AppLayout's px-4/px-6
 *      via negative margins so we don't see the sidebar gutter on
 *      the left side.
 *
 *   2. Color-fade overlay on the LEFT 50%. Uses two CSS variables
 *      driven by node-vibrant's runtime palette extraction (vibrant
 *      + dark-muted) — we set them on the root and the gradient picks
 *      them up. Falls back to `bg-base` when the palette is still
 *      loading or extraction failed; that fallback is also why the
 *      gradient sits BELOW the content layer, never above (otherwise
 *      a slow extraction would make the title flash).
 *
 *   3. Content column, anchored to the LEFT 30-40% of the hero:
 *        • poster (vertically centered against the hero)
 *        • title block immediately below: title, meta row, overview
 *        • action buttons: smart Play, favorite, kebab
 *
 * Why a separate component from HeroSection:
 *   The movie / season / episode pages still want the original
 *   right-aligned layout (poster left, info right). This new layout
 *   is series-only and inverts that arrangement deliberately — the
 *   poster + info form a vertical column on the left side, leaving
 *   the right two-thirds for the backdrop image to dominate. Trying
 *   to gate this in HeroSection via a "mode" prop would couple two
 *   layouts that share basically nothing visually.
 */
const SeriesHero: FC<SeriesHeroProps> = ({
  item,
  resumeMode,
  resumeLabel,
  resumeProgressPercent,
  onPlay,
  onToggleFavorite,
  isFavorite,
  menuItems,
}) => {
  const { t } = useTranslation();
  // Backdrop resolution priority:
  //   1. The item's own backdrop (full series, when present).
  //   2. The series's backdrop folded in by attachSeriesContext —
  //      relevant for season pages, which have a poster but no
  //      backdrop of their own. Falling back to the season's vertical
  //      poster looked terrible (stretched + portrait orientation in
  //      a 16:7 hero), so the series backdrop wins this slot before
  //      the poster ever does.
  //   3. The item's own poster (last resort, blurred underpainting).
  const heroBackdropUrl =
    item.backdrop_url ?? item.series_backdrop_url ?? item.poster_url ?? null;

  // Prefer the backend-precomputed palette (no decode delay, no extra
  // image fetch) and fall back to runtime extraction only when the
  // primary image was ingested before the extraction code shipped.
  // The hook is a no-op when imageUrl is null, so we skip the fetch
  // entirely on rows whose colours already arrived with the response.
  const hasServerPalette =
    !!(item.backdrop_colors?.vibrant || item.backdrop_colors?.muted);
  const runtimePalette = useVibrantColors(
    hasServerPalette ? null : heroBackdropUrl,
  );

  // CSS-var driven gradient. The two stops correspond to the dark-
  // muted and vibrant swatches; when extraction is still pending or
  // failed, fall back to plain bg-base so the page looks intentional
  // either way.
  const gradientStart =
    item.backdrop_colors?.muted ?? runtimePalette.muted ?? "rgb(8, 12, 16)";
  const gradientMid =
    item.backdrop_colors?.vibrant ?? runtimePalette.vibrant ?? "rgb(8, 12, 16)";

  return (
    <section
      className="relative -mx-4 md:-mx-6 overflow-hidden"
      style={{
        ['--hero-c1' as string]: gradientStart,
        ['--hero-c2' as string]: gradientMid,
        // Bleed behind the sticky TopBar so the backdrop reaches the
        // very top of the viewport. The TopBar paints over it at z-30
        // and goes glass-blurred on scroll (see TopBar.tsx). Same
        // pattern as the Home page hero — keeps the visual language
        // consistent across surfaces.
        marginTop: "calc(var(--topbar-height) * -1)",
      }}
    >
      {/* Backdrop layer — fixed-height band so the season grid below
          stays visible above the fold on a typical 1080p viewport
          (~960px usable after browser chrome).

          On wide desktop windows `object-cover` scales the natively
          16:9 TMDb backdrop to fit the viewport width, which means
          a band shorter than ~560px on lg ends up showing only the
          centre slice of the image and reads as "very zoomed in".
          Two levers fight that: a generous min-height so we reveal
          more of the scaled image, and `object-position: right top`
          so what we DO reveal includes the upper third where actor
          faces typically sit (instead of cropping heads off, which
          the previous `object-right` centre-anchor did). */}
      <div className="relative min-h-[460px] sm:min-h-[540px] lg:min-h-[600px] max-h-[720px]">
        {heroBackdropUrl ? (
          <img
            src={heroBackdropUrl}
            alt=""
            loading="eager"
            className="absolute inset-0 h-full w-full object-cover"
            style={{ objectPosition: "right top" }}
          />
        ) : (
          <div className="absolute inset-0 bg-gradient-to-br from-bg-elevated to-bg-card" />
        )}

        {/* Netflix-style preview: a couple of seconds after the page
            settles, fade in a muted trailer over the backdrop. The
            HeroTrailer component handles the timer, the embed URL
            (YouTube / Vimeo) and graceful dismissal — when there's
            no trailer it renders nothing. */}
        {item.trailer && (
          <HeroTrailer
            siteKey={item.trailer.site}
            videoKey={item.trailer.key}
          />
        )}

        {/* Vibrant-color fade on the left 50%. The first stop is solid
            (hides whatever the backdrop has under the poster), the
            second is at 70% opacity to bleed colour into the image, the
            third is fully transparent to let the backdrop breathe on
            the right side. The vertical fade-to-bg-base at the bottom
            (8% from the bottom) hides the seam where the hero meets
            the rest of the page. */}
        <div
          className="absolute inset-0"
          style={{
            background:
              "linear-gradient(to right, var(--hero-c1) 0%, color-mix(in srgb, var(--hero-c2) 60%, transparent) 30%, transparent 55%)",
          }}
        />
        {/* Bottom-fade: image -> page tint. The target colour is
            published by the ItemDetail wrapper as `--detail-tint` and
            falls back to plain bg-base when no palette is available.
            Targeting the same colour the page below uses means the
            seam between hero and seasons grid is invisible — no
            "container-on-container" look. Taller fade band (h-32)
            than before so the transition is gradual rather than
            cliff-edge. */}
        <div
          className="absolute inset-x-0 bottom-0 h-32"
          style={{
            background:
              "linear-gradient(to bottom, transparent, var(--detail-tint, rgb(8 12 16)))",
          }}
        />

        {/* Content column. `max-w` keeps it on the left third on wide
            screens; on mobile it stretches and the gradient extends
            further across. Vertical centering matches Plex / Jellyfin
            where the poster sits in the optical centre of the hero.
            `pt-[topbar]` keeps the poster from sliding behind the
            translucent TopBar — the backdrop bleeds up there but
            interactive content stays clear. */}
        <div
          className="relative z-10 flex h-full items-center px-6 pb-12 sm:px-10 lg:px-16"
          style={{ paddingTop: "calc(var(--topbar-height) + 1.5rem)" }}
        >
          <div className="flex w-full max-w-md flex-col items-start gap-5 lg:max-w-lg">
            {item.poster_url && (
              <img
                src={item.poster_url}
                alt={item.title}
                className="h-[240px] w-auto rounded-[--radius-lg] shadow-2xl shadow-black/60 object-cover sm:h-[280px] lg:h-[340px]"
              />
            )}

            <div className="flex flex-col gap-3">
              {item.logo_url ? (
                <img
                  src={item.logo_url}
                  alt={item.title}
                  className="max-h-[60px] w-auto max-w-full object-contain object-left drop-shadow-lg"
                />
              ) : (
                <h1 className="text-3xl font-bold text-text-primary drop-shadow-lg sm:text-4xl">
                  {item.title}
                </h1>
              )}

              {/* Tagline sits between the title and the meta badges.
                  Plex and TMDb both surface this string ("With great
                  power comes great responsibility") right under the
                  title — italic, single line, dimmer than the
                  overview. Hidden when empty so series without a
                  tagline don't get a phantom row. */}
              {item.tagline && (
                <p className="-mt-1 max-w-xl text-sm italic text-text-secondary/90 drop-shadow-md">
                  {item.tagline}
                </p>
              )}

              <div className="flex flex-wrap items-center gap-2 text-sm text-text-secondary">
                {item.year != null && <span className="font-medium">{item.year}</span>}
                {item.community_rating != null && (
                  <Badge variant="warning">
                    <svg className="h-3 w-3" viewBox="0 0 24 24" fill="currentColor">
                      <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z" />
                    </svg>
                    {formatRating(item.community_rating)}
                  </Badge>
                )}
                {item.content_rating != null && <Badge>{item.content_rating}</Badge>}
                {item.genres?.slice(0, 3).map((g) => (
                  <Badge key={g}>{g}</Badge>
                ))}
                {/* Studio / network — last in the row so it reads as
                    a soft attribution rather than a primary tag.
                    Rendered as plain text (not a Badge) to keep the
                    badge cluster visually about taxonomy. */}
                {item.studio && (
                  <span className="text-xs text-text-muted">· {item.studio}</span>
                )}
              </div>

              {/* Watched-count aggregate — only present on the series
                  scope and only when the user is authenticated. Shown
                  as a thin progress bar with the count alongside, so
                  a glance at the hero answers "how far am I in this
                  show?" without scrolling to the seasons grid. */}
              {item.episode_progress && item.episode_progress.total > 0 && (
                <div className="flex items-center gap-3 text-xs text-text-secondary">
                  <div
                    className="h-1.5 w-32 overflow-hidden rounded-full bg-bg-elevated/60"
                    role="progressbar"
                    aria-valuemin={0}
                    aria-valuemax={item.episode_progress.total}
                    aria-valuenow={item.episode_progress.watched}
                  >
                    <div
                      className="h-full bg-accent transition-all duration-500"
                      style={{
                        width: `${Math.min(
                          100,
                          (item.episode_progress.watched /
                            item.episode_progress.total) *
                            100,
                        )}%`,
                      }}
                    />
                  </div>
                  <span>
                    {item.episode_progress.watched >= item.episode_progress.total
                      ? t("itemDetail.episodesAllWatched", {
                          total: item.episode_progress.total,
                        })
                      : t("itemDetail.episodesWatched", {
                          watched: item.episode_progress.watched,
                          total: item.episode_progress.total,
                        })}
                  </span>
                </div>
              )}

              {item.overview && (
                <p className="line-clamp-3 max-w-2xl text-sm leading-relaxed text-text-secondary sm:text-[15px]">
                  {item.overview}
                </p>
              )}

              <div className="flex items-center gap-3 pt-1">
                <button
                  type="button"
                  onClick={onPlay}
                  disabled={resumeMode === "none"}
                  className="group relative flex items-center gap-2 overflow-hidden rounded-[--radius-md] bg-accent px-5 py-2.5 text-sm font-semibold text-white shadow-lg shadow-accent/30 transition-all hover:bg-accent-hover hover:shadow-accent/40 disabled:cursor-not-allowed disabled:opacity-50 disabled:shadow-none cursor-pointer"
                >
                  <svg className="h-5 w-5 shrink-0" viewBox="0 0 24 24" fill="currentColor">
                    <path d="M8 5v14l11-7z" />
                  </svg>
                  <span>{resumeLabel}</span>
                  {resumeProgressPercent != null && resumeProgressPercent > 0 && (
                    <span
                      className="absolute bottom-0 left-0 h-0.5 bg-white/70"
                      style={{ width: `${Math.min(100, Math.max(0, resumeProgressPercent))}%` }}
                      aria-hidden="true"
                    />
                  )}
                </button>

                <button
                  type="button"
                  onClick={onToggleFavorite}
                  className="flex h-10 w-10 items-center justify-center rounded-full border border-border bg-bg-card/60 backdrop-blur-sm transition-colors hover:bg-bg-elevated cursor-pointer"
                  aria-label={
                    isFavorite
                      ? t("itemDetail.removeFromFavorites")
                      : t("itemDetail.addToFavorites")
                  }
                >
                  <svg
                    className={`h-5 w-5 transition-colors ${isFavorite ? "text-error fill-error" : "text-text-secondary"}`}
                    viewBox="0 0 24 24"
                    fill={isFavorite ? "currentColor" : "none"}
                    stroke="currentColor"
                    strokeWidth={2}
                  >
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      d="M20.84 4.61a5.5 5.5 0 00-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 00-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 000-7.78z"
                    />
                  </svg>
                </button>

                {menuItems.length > 0 && <SeriesHeroKebab items={menuItems} />}
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
};

// Lightweight kebab variant of HeroSection's menu, kept inline here so
// SeriesHero stays self-contained. Mirrors the original API + visual.
const SeriesHeroKebab: FC<{ items: HeroMenuItem[] }> = ({ items }) => {
  const { t } = useTranslation();
  // Local open state — no portal needed, the hero overflow is hidden but
  // the menu pops upward on top of it.
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onClickOutside = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    const onEsc = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onClickOutside);
    document.addEventListener("keydown", onEsc);
    return () => {
      document.removeEventListener("mousedown", onClickOutside);
      document.removeEventListener("keydown", onEsc);
    };
  }, [open]);

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex h-10 w-10 items-center justify-center rounded-full border border-border bg-bg-card/60 backdrop-blur-sm transition-colors hover:bg-bg-elevated cursor-pointer"
        aria-label={t("common.moreOptions")}
        aria-expanded={open}
      >
        <svg className="h-5 w-5 text-text-secondary" viewBox="0 0 24 24" fill="currentColor">
          <circle cx="12" cy="5" r="1.5" />
          <circle cx="12" cy="12" r="1.5" />
          <circle cx="12" cy="19" r="1.5" />
        </svg>
      </button>
      {open && (
        <div className="absolute bottom-full mb-2 left-0 z-50 min-w-[200px] rounded-[--radius-lg] border border-border bg-bg-card shadow-xl shadow-black/40 backdrop-blur-xl overflow-hidden">
          {items.map((mi, i) => (
            <button
              key={i}
              type="button"
              onClick={() => {
                setOpen(false);
                mi.onClick();
              }}
              className={[
                "flex w-full items-center gap-3 px-4 py-2.5 text-sm transition-colors cursor-pointer",
                mi.variant === "danger"
                  ? "text-error hover:bg-error/10"
                  : "text-text-secondary hover:text-text-primary hover:bg-bg-elevated",
              ].join(" ")}
            >
              <span className="flex h-5 w-5 shrink-0 items-center justify-center">{mi.icon}</span>
              {mi.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
};

// sessionStorage key used to remember a session-wide dismissal of the
// hero trailer. Once the user clicks "Skip" on any trailer, every other
// trailer stays suppressed for the rest of the tab's lifetime — they
// already told us they don't want it; making them dismiss again on the
// next page would be hostile.
const TRAILER_DISMISSED_KEY = "hubplay:trailers-dismissed";

// Whether the user opted out of trailers in this session. Reads on each
// call (cheap) so the post-dismissal check stays accurate after the
// flag is set.
function trailersDismissedThisSession(): boolean {
  try {
    return sessionStorage.getItem(TRAILER_DISMISSED_KEY) === "1";
  } catch {
    // Safari Private Mode and similar throw on storage access; treat
    // an inaccessible store the same as an empty one — don't suppress.
    return false;
  }
}

// shouldSkipTrailer collapses every "don't load video" condition into a
// single decision. We check at mount and never re-evaluate; if the user
// changes their reduced-motion preference mid-session a refresh picks
// it up.
function shouldSkipTrailer(): boolean {
  if (typeof window === "undefined") return true;
  if (trailersDismissedThisSession()) return true;

  // Respect prefers-reduced-motion. Autoplaying video for users who
  // explicitly asked the OS to dial back animation is exactly the kind
  // of thing that motion preference exists to prevent.
  if (window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) {
    return true;
  }

  // Save-Data and slow connections: don't burn the user's data plan on
  // a decorative preview. effectiveType comes from the Network
  // Information API, present in Chromium and stable enough for this
  // kind of soft heuristic. Absence of the API = we assume a normal
  // connection, which matches Safari/Firefox behaviour.
  const conn = (navigator as Navigator & {
    connection?: { saveData?: boolean; effectiveType?: string };
  }).connection;
  if (conn?.saveData) return true;
  if (conn?.effectiveType === "slow-2g" || conn?.effectiveType === "2g") {
    return true;
  }

  return false;
}

/**
 * HeroTrailer — Netflix-style autoplay-muted preview that fades in
 * over the backdrop a couple of seconds after the hero enters view.
 *
 * Cost-savings over a naive `<iframe src=embedUrl>`:
 *
 *   1. We never mount the iframe at all if the user opted out of
 *      animation, is on Save-Data/2G, or already dismissed a trailer
 *      earlier in this session.
 *   2. IntersectionObserver gates the load on the hero actually being
 *      visible — opening a series page and immediately scrolling away
 *      never triggers a YouTube round-trip.
 *   3. A `<link rel="preconnect">` is dropped on the document head
 *      while we wait, so by the time the iframe src flips, the TLS
 *      handshake to youtube-nocookie.com is already done.
 *   4. The two-stage reveal (load at +2.5s, fade at +3.7s) hides
 *      YouTube's pre-roll click-to-play overlay; the user never sees
 *      static placeholder UI.
 *
 * Embed URLs are platform-specific; YouTube and Vimeo only (the picker
 * on the Go side filters anything else). The iframe stays
 * `pointer-events: none` so a click anywhere in the hero hits the Play
 * button, never the embedded player.
 */
function HeroTrailer({ siteKey, videoKey }: { siteKey: string; videoKey: string }) {
  const { t } = useTranslation();

  // Decide once at mount whether we should even start the dance. The
  // initialiser only runs on the first render; subsequent renders
  // observe the cached value, so the suppression decision is stable
  // for the life of the component.
  const [skipped] = useState(() => shouldSkipTrailer());

  // Two-stage reveal solves the "click-to-play overlay leaks through"
  // problem with naive autoplay embeds:
  //
  //   1. `loaded` flips ~2.5s after we decide to load → the iframe
  //      gets its real src and YouTube starts initialising. The
  //      wrapper is still opacity-0 here, so the user never sees
  //      YouTube's pre-play poster-frame + centred play button that
  //      briefly flashes while the player buffers.
  //   2. `revealed` flips another ~1.2s later → wrapper fades in.
  //      By then the trailer is actually playing, so what surfaces
  //      is the moving image, not the static placeholder UI.
  const [inViewport, setInViewport] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [revealed, setRevealed] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const wrapperRef = useRef<HTMLDivElement>(null);

  // IntersectionObserver: only kick off the load timer when the hero
  // is at least 25% visible. Once we've seen it, we stop observing —
  // re-entering the viewport later doesn't restart the show, which
  // keeps the "Skip trailer" decision sticky within the same mount.
  useEffect(() => {
    if (skipped || dismissed) return;
    const node = wrapperRef.current;
    if (!node || typeof IntersectionObserver === "undefined") {
      // Fallback for jsdom + ancient browsers: just load immediately.
      setInViewport(true);
      return;
    }
    const observer = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (e.isIntersecting) {
            setInViewport(true);
            observer.disconnect();
            return;
          }
        }
      },
      { threshold: 0.25 },
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [skipped, dismissed]);

  // Preconnect hint while the timers tick. Dropping it on mount of
  // the in-viewport phase saves ~150ms of TLS handshake when the
  // iframe finally requests the embed page.
  useEffect(() => {
    if (!inViewport || skipped || dismissed) return;
    const origins = siteKey === "Vimeo"
      ? ["https://player.vimeo.com"]
      : ["https://www.youtube-nocookie.com", "https://i.ytimg.com"];
    const links: HTMLLinkElement[] = origins.map((href) => {
      const link = document.createElement("link");
      link.rel = "preconnect";
      link.href = href;
      link.crossOrigin = "";
      document.head.appendChild(link);
      return link;
    });
    return () => {
      for (const l of links) l.remove();
    };
  }, [inViewport, skipped, dismissed, siteKey]);

  // Load + reveal timers, gated on actually being in view.
  useEffect(() => {
    if (!inViewport || skipped || dismissed) return;
    const loadTimer = setTimeout(() => setLoaded(true), 2500);
    const revealTimer = setTimeout(() => setRevealed(true), 3700);
    return () => {
      clearTimeout(loadTimer);
      clearTimeout(revealTimer);
    };
  }, [inViewport, skipped, dismissed]);

  const handleDismiss = () => {
    setDismissed(true);
    try {
      sessionStorage.setItem(TRAILER_DISMISSED_KEY, "1");
    } catch {
      // No storage = no persistence; the dismissal still holds for
      // this mount via the dismissed state.
    }
  };

  const embedUrl = trailerEmbedURL(siteKey, videoKey);
  if (!embedUrl || dismissed || skipped) {
    // Still render the ref target when skipped so a future enable
    // path could re-observe — but in practice we render null because
    // there's nothing to show. The wrapperRef stays null and that's
    // fine; the IO effect bails early on skipped/dismissed.
    return null;
  }

  // Mask the trailer so its left edge fades into the gradient instead
  // of cutting hard against the poster column. Black at the right is
  // "fully opaque", transparent at ~50% means "blend into whatever is
  // beneath" — and what's beneath is the static backdrop image plus
  // the colour gradient, which is exactly the look we want. A solid
  // sharp boundary felt like a TV-on-a-page; this fade matches the
  // Netflix / Disney+ pre-play overlay.
  const fadeMask = "linear-gradient(to right, transparent 0%, transparent 25%, black 55%, black 100%)";

  return (
    <div
      ref={wrapperRef}
      className={[
        "absolute inset-0 transition-opacity duration-700",
        revealed ? "opacity-100" : "opacity-0 pointer-events-none",
      ].join(" ")}
    >
      <div
        className="absolute inset-0 overflow-hidden"
        style={{
          maskImage: fadeMask,
          WebkitMaskImage: fadeMask,
        }}
      >
        {/* Object-cover behaviour for an iframe: size the iframe at
            the hero's full width but lock its aspect to 16:9 (the
            shape YouTube renders inside). The hero is closer to 16:7
            on wide monitors, so 16:9 height overflows top + bottom —
            cropped by `overflow-hidden`. The previous `scale-1.35`
            approach scaled the wrapper but YouTube's letterbox bars
            scaled with it, leaving black gutters on the right edge
            on extra-wide screens. Aspect-ratio sizing kills them.

            Render the iframe *only* once `loaded` flips. Keeping it
            mounted with src='about:blank' would still cost layout +
            an internal frame in the engine, and would pre-emptively
            wire up the iframe's parent-frame messaging hooks. Mounting
            on demand is the cheapest path. */}
        {loaded && (
          <iframe
            src={embedUrl}
            title={t("itemDetail.trailer")}
            allow="autoplay; encrypted-media; picture-in-picture"
            referrerPolicy="strict-origin-when-cross-origin"
            loading="lazy"
            className="absolute left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 border-0 pointer-events-none"
            style={{
              width: "100%",
              aspectRatio: "16 / 9",
              // Belt-and-suspenders for unusually tall hero bands
              // (e.g. very narrow viewports): force the iframe to
              // at least cover the parent height, accepting horizontal
              // letterbox in that edge case rather than vertical bars.
              minHeight: "100%",
              minWidth: "calc(100% * 16 / 9)",
            }}
          />
        )}
      </div>
      {/* Subtle right-side dim — the gradient on the left already
          handles its own blend with the masked trailer; this just
          takes a bit of saturation off the bare right side so the
          content + button stay readable. */}
      <div className="absolute inset-0 bg-bg-base/20" />

      {revealed && (
        <button
          type="button"
          onClick={handleDismiss}
          aria-label={t("itemDetail.dismissTrailer")}
          className="absolute bottom-4 right-4 z-20 flex h-9 items-center gap-1.5 rounded-full bg-black/60 px-3 text-xs font-medium text-white backdrop-blur-sm transition-colors hover:bg-black/80 cursor-pointer"
        >
          <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
            <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
          </svg>
          {t("itemDetail.dismissTrailer")}
        </button>
      )}
    </div>
  );
}

// trailerEmbedURL maps a (site, key) pair to the right embed URL. The
// site list mirrors the picker in `internal/provider/tmdb.go::pickTrailer`
// — adding a third platform means extending both. Returns null for
// unknown sites so the hero falls back to the static backdrop.
function trailerEmbedURL(site: string, key: string): string | null {
  switch (site) {
    case "YouTube":
      // mute=1 + playsinline=1 are required for autoplay on every
      // major browser as of 2024. modestbranding/rel/iv_load_policy
      // strip the YouTube chrome we don't want bleeding into the
      // hero — same flags Plex / Jellyfin pass to their embeds.
      return `https://www.youtube-nocookie.com/embed/${encodeURIComponent(key)}?autoplay=1&mute=1&controls=0&loop=1&playlist=${encodeURIComponent(key)}&modestbranding=1&playsinline=1&rel=0&iv_load_policy=3&disablekb=1`;
    case "Vimeo":
      return `https://player.vimeo.com/video/${encodeURIComponent(key)}?autoplay=1&muted=1&loop=1&controls=0&background=1`;
    default:
      return null;
  }
}

export { SeriesHero };
export type { SeriesHeroProps };
