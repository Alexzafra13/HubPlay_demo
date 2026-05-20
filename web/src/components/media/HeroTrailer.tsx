import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

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
//
// The cross-session opt-out (`playback.trailers_enabled` in user
// preferences) lives on the parent — we receive it as `userOptedOut`
// rather than reading it here so the decision tree stays declarative
// and the hook test surface is honest.
//
// Module-private: not exported because Fast Refresh wants this file to
// only emit components, and the function is internal to HeroTrailer.
function shouldSkipTrailer(userOptedOut: boolean): boolean {
  if (typeof window === "undefined") return true;
  if (userOptedOut) return true;
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

export interface HeroTrailerProps {
  siteKey: string;
  videoKey: string;
  /**
   * Cross-session user preference (`playback.trailers_enabled`). When
   * the user disabled trailers in Settings the parent passes `false`
   * and the component renders nothing. The session-only "Skip" button
   * is layered on top of this, in sessionStorage, so a single dismissal
   * suppresses every subsequent hero in the same tab without
   * persisting to the server.
   */
  userOptedOut?: boolean;
  /** Fired the first time the trailer becomes visible (post-reveal
   *  timer). Lets the parent fade the static backdrop out so the
   *  two layers don't fight for attention. */
  onReveal?: () => void;
  /** Fired when the user clicks "Skip trailer" or the component
   *  decides to bail. Parent should fade the backdrop back in. */
  onDismiss?: () => void;
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
export function HeroTrailer({
  siteKey,
  videoKey,
  userOptedOut = false,
  onReveal,
  onDismiss,
}: HeroTrailerProps) {
  const { t } = useTranslation();

  // Decide once at mount whether we should even start the dance. The
  // initialiser only runs on the first render; subsequent renders
  // observe the cached value, so the suppression decision is stable
  // for the life of the component.
  const [skipped] = useState(() => shouldSkipTrailer(userOptedOut));

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
  // iframeLoaded flips true on the iframe's onLoad event (the YouTube /
  // Vimeo embed page actually fetched). Used to gate the reveal: we
  // wait for proof of life before fading out the backdrop, so a slow
  // CDN or a blocked-frame error doesn't leave the user staring at a
  // half-loaded player while the static hero is gone. A watchdog
  // dismisses if this never flips.
  const [iframeLoaded, setIframeLoaded] = useState(false);
  const [revealed, setRevealed] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const wrapperRef = useRef<HTMLDivElement>(null);

  // Embeddability pre-flight. YouTube returns 401 when a video is
  // region-restricted or its owner disabled embedding, and 404 when
  // the video was removed; without checking we'd render the iframe
  // anyway and the user would see YouTube's "Este vídeo no está
  // disponible" error inside our hero. The oEmbed endpoint is CORS-
  // enabled and tiny (a few hundred bytes), so we treat it as a
  // gating fetch before mounting the player. `null` = still
  // checking, `true` = mount the iframe, `false` = silently bail.
  const [embeddable, setEmbeddable] = useState<boolean | null>(null);

  // IntersectionObserver: only kick off the load timer when the hero
  // is at least 25% visible. Once we've seen it, we stop observing —
  // re-entering the viewport later doesn't restart the show, which
  // keeps the "Skip trailer" decision sticky within the same mount.
  useEffect(() => {
    if (skipped || dismissed) return;
    const node = wrapperRef.current;
    if (!node || typeof IntersectionObserver === "undefined") {
      // Fallback for jsdom + ancient browsers: just load immediately.
      // The synchronous setState inside the effect is intentional —
      // without IO we have no event source to subscribe to, so this
      // effect's only job IS to flip the gating state once. The lint
      // rule's "cascading renders" concern doesn't apply: the next
      // render of this same component is exactly what we want.
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

  // Embeddability check, gated on viewport so we don't pay the round
  // trip for trailers the user may never see (scrolling away from a
  // hero before it enters view). Aborts on unmount + on dismiss so a
  // fast nav doesn't leak in-flight requests. Failures (network,
  // 401, 404) all collapse to "not embeddable" — the trailer is a
  // decorative affordance, falling back to the static backdrop is
  // strictly better than showing a YouTube error frame.
  useEffect(() => {
    if (!inViewport || skipped || dismissed) return;
    if (embeddable !== null) return;
    const ctl = new AbortController();
    checkEmbeddable(siteKey, videoKey, ctl.signal)
      .then((ok) => setEmbeddable(ok))
      .catch(() => setEmbeddable(false));
    return () => ctl.abort();
  }, [inViewport, skipped, dismissed, siteKey, videoKey, embeddable]);

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

  // Mount the iframe ~2.5s after the hero enters view, gated on the
  // oEmbed pre-flight passing. Without the embeddability gate we'd
  // fire this timer immediately, and a failed oEmbed coming back
  // later would have to tear it down — simpler to wait for the
  // green light.
  useEffect(() => {
    if (!inViewport || skipped || dismissed) return;
    if (embeddable !== true) return;
    const loadTimer = setTimeout(() => setLoaded(true), 2500);
    return () => clearTimeout(loadTimer);
  }, [inViewport, skipped, dismissed, embeddable]);

  // Reveal is gated on the iframe actually loading, not on a fixed
  // wall-clock timer. Once iframeLoaded flips true we add a 1.2s
  // settle buffer to hide YouTube's pre-play overlay before the
  // wrapper fades in, then notify the parent so the static backdrop
  // can fade out in step. If the iframe never loads (CSP block,
  // network failure, frame-ancestors mismatch from upstream, browser
  // extension breaking the embed) the watchdog effect below
  // dismisses and the backdrop stays — strictly better than fading
  // the hero into a half-loaded player or a blocked-frame error.
  useEffect(() => {
    if (!loaded || !iframeLoaded || revealed || dismissed || skipped) return;
    const revealTimer = setTimeout(() => {
      setRevealed(true);
      onReveal?.();
    }, 1200);
    return () => clearTimeout(revealTimer);
  }, [loaded, iframeLoaded, revealed, dismissed, skipped, onReveal]);

  const handleDismiss = useCallback(() => {
    setDismissed(true);
    // Restore the static backdrop in the parent so the page doesn't
    // go blank on the right when the trailer disappears.
    onDismiss?.();
    try {
      sessionStorage.setItem(TRAILER_DISMISSED_KEY, "1");
    } catch {
      // No storage = no persistence; the dismissal still holds for
      // this mount via the dismissed state.
    }
  }, [onDismiss]);

  // Watchdog: if the iframe doesn't fire onLoad within 6s of mount
  // we treat it as a hard fail and dismiss. Cross-origin iframes
  // don't always fire onError on CSP / X-Frame-Options blocks
  // (browsers vary), so this timer is the reliable signal — onError
  // is a belt-and-suspenders nice-to-have, not the primary gate.
  // 6s comfortably exceeds a slow-network embed page fetch (~3s p99
  // on YouTube nocookie) without making a real failure hang the
  // hero indefinitely.
  useEffect(() => {
    if (!loaded || iframeLoaded || dismissed || skipped) return;
    const watchdog = setTimeout(() => {
      handleDismiss();
    }, 6000);
    return () => clearTimeout(watchdog);
  }, [loaded, iframeLoaded, dismissed, skipped, handleDismiss]);

  const embedUrl = trailerEmbedURL(siteKey, videoKey);
  if (!embedUrl || dismissed || skipped || embeddable === false) {
    return null;
  }

  // 2D mask applied DIRECTLY to the iframe (not the wrapper). The
  // wrapper spans the full hero, but the iframe only occupies the
  // right ~60% — if the mask is on the wrapper, "0% transparent"
  // lives at the wrapper's left edge (far outside the iframe), so
  // the iframe's own left edge lands at ~30% mask opacity and the
  // rectangle outline stays visible.
  //
  // Putting the mask on the iframe makes the gradient stops relative
  // to the IFRAME'S bounds: 0% IS the iframe's left edge, fully
  // transparent there, fading to opaque past ~50%. Top stays opaque,
  // bottom fades. Composited via mask-composite: intersect so the
  // image is only solid where both gradients agree.
  const fadeMask =
    "linear-gradient(to right, transparent 0%, rgba(0,0,0,0.25) 20%, rgba(0,0,0,0.85) 50%, black 75%), linear-gradient(to bottom, black 0%, black 70%, rgba(0,0,0,0.25) 92%, transparent 100%)";

  return (
    <div
      ref={wrapperRef}
      className={[
        "absolute inset-0 transition-opacity duration-700",
        revealed ? "opacity-100" : "opacity-0 pointer-events-none",
      ].join(" ")}
    >
      <div className="absolute inset-0 overflow-hidden">
        {loaded && (
          <iframe
            src={embedUrl}
            title={t("itemDetail.trailer")}
            allow="autoplay; encrypted-media; picture-in-picture"
            referrerPolicy="strict-origin-when-cross-origin"
            loading="lazy"
            onLoad={() => setIframeLoaded(true)}
            onError={handleDismiss}
            className="absolute right-0 top-0 border-0 pointer-events-none"
            style={{
              height: "100%",
              aspectRatio: "16 / 9",
              width: "auto",
              maskImage: fadeMask,
              WebkitMaskImage: fadeMask,
              maskComposite: "intersect",
              WebkitMaskComposite: "source-in",
            }}
          />
        )}
      </div>

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

// checkEmbeddable returns true when the platform's oEmbed endpoint
// confirms the video is publicly embeddable, false when it 401s
// (region-restricted / embed-disabled), 404s (removed), or any other
// error. The caller treats false the same as "no trailer at all" —
// silently fall back to the static backdrop instead of rendering an
// iframe that will surface YouTube's "Este vídeo no está disponible"
// error inside the hero.
//
// oEmbed is CORS-enabled on both YouTube and Vimeo so we don't need a
// server-side proxy. The response payload (a few hundred bytes) is
// thrown away — we only care about the HTTP status.
async function checkEmbeddable(
  site: string,
  key: string,
  signal: AbortSignal,
): Promise<boolean> {
  let url: string;
  switch (site) {
    case "YouTube":
      url = `https://www.youtube.com/oembed?url=${encodeURIComponent(
        `https://youtu.be/${key}`,
      )}&format=json`;
      break;
    case "Vimeo":
      url = `https://vimeo.com/api/oembed.json?url=${encodeURIComponent(
        `https://vimeo.com/${key}`,
      )}`;
      break;
    default:
      // Unknown site — trailerEmbedURL will reject it anyway, but
      // returning false here short-circuits the gating effect so we
      // don't even start timers.
      return false;
  }
  try {
    const res = await fetch(url, { signal, method: "GET" });
    return res.ok;
  } catch {
    return false;
  }
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
