import { useEffect, useState } from "react";

/**
 * Color palette extracted from an image. Each entry is a CSS rgb(a) string,
 * or `null` if the algorithm could not find a swatch of that role.
 *
 * `vibrant` is the most visually striking colour — used as the gradient
 * accent. `muted` is a desaturated complement — used as the gradient
 * fade-out colour next to the page background. The two together produce
 * a subtle, cinema-style ambient bleed instead of a single flat tint.
 */
export interface VibrantPalette {
  vibrant: string | null;
  muted: string | null;
  // Plex-style 4-corner backgrounds (see itemDetail/aurora.ts) want
  // up to four distinct swatches to fill the corners; node-vibrant
  // already returns these, so we just expose them. All optional —
  // monochrome posters return null for the variants the algorithm
  // couldn't find, and downstream falls back to the basic vibrant /
  // muted pair.
  darkVibrant: string | null;
  lightVibrant: string | null;
  lightMuted: string | null;
}

const EMPTY: VibrantPalette = {
  vibrant: null,
  muted: null,
  darkVibrant: null,
  lightVibrant: null,
  lightMuted: null,
};

/**
 * In-memory cache so repeated mounts of the same hero don't re-decode the
 * image. Keyed by URL — palette is stable for a given image, and we never
 * have to invalidate. Survives React strict-mode double-mount and route
 * back-navigation, but resets with the page (which is fine — the colours
 * paint within ~50ms anyway).
 */
const cache = new Map<string, VibrantPalette>();

/**
 * useVibrantColors — extract the dominant + muted colours from `imageUrl`
 * via `node-vibrant`, returning `{vibrant, muted}` once decoding finishes.
 * Returns nulls until the image is fetched + decoded; consumers should
 * default to a static fallback colour while loading so the UI doesn't
 * flicker at first paint.
 *
 * Implementation notes:
 *   - The library is loaded via dynamic import so it lands in its own
 *     chunk; the main bundle stays slim and pages without a hero (login,
 *     home, list views) never touch it.
 *   - Cancellation: a stale resolution from the previous URL must not
 *     overwrite the new URL's palette. The `cancelled` flag in the effect
 *     guards against that — necessary because dynamic-import resolution
 *     can land long after the URL prop has changed (slow networks, fast
 *     hero swaps).
 *   - Errors are swallowed: a failed colour extraction degrades to the
 *     static fallback the consumer already renders, which is strictly
 *     better than a blocked page.
 */
export function useVibrantColors(imageUrl: string | null | undefined): VibrantPalette {
  const [palette, setPalette] = useState<VibrantPalette>(() => {
    if (!imageUrl) return EMPTY;
    return cache.get(imageUrl) ?? EMPTY;
  });

  // Synchronous palette swap on URL change — render-time guarded so the
  // previous hero's colours don't bleed into the next one's first paint.
  // The async decode for uncached URLs still lives in the effect below.
  const [lastImageUrl, setLastImageUrl] = useState(imageUrl);
  if (imageUrl !== lastImageUrl) {
    setLastImageUrl(imageUrl);
    setPalette(imageUrl ? cache.get(imageUrl) ?? EMPTY : EMPTY);
  }

  useEffect(() => {
    if (!imageUrl) return;
    if (cache.has(imageUrl)) return;

    let cancelled = false;
    void (async () => {
      try {
        const { Vibrant } = await import("node-vibrant/browser");
        const swatches = await Vibrant.from(imageUrl).getPalette();
        if (cancelled) return;
        const toRgb = (rgb?: number[] | null): string | null =>
          rgb ? `rgb(${rgb.map(Math.round).join(", ")})` : null;
        const next: VibrantPalette = {
          vibrant: toRgb(swatches.Vibrant?.rgb),
          // Kept under the legacy name `muted` for backward-compat
          // with HeroSection / SeriesHero — they read the darker
          // muted swatch as the gradient backstop.
          muted: toRgb(swatches.DarkMuted?.rgb),
          darkVibrant: toRgb(swatches.DarkVibrant?.rgb),
          lightVibrant: toRgb(swatches.LightVibrant?.rgb),
          lightMuted: toRgb(swatches.Muted?.rgb),
        };
        cache.set(imageUrl, next);
        setPalette(next);
      } catch {
        // best-effort — consumer renders its static fallback
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [imageUrl]);

  return palette;
}
