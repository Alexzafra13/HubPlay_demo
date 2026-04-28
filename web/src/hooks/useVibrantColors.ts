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
}

const EMPTY: VibrantPalette = { vibrant: null, muted: null };

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

  useEffect(() => {
    if (!imageUrl) {
      setPalette(EMPTY);
      return;
    }
    const cached = cache.get(imageUrl);
    if (cached) {
      setPalette(cached);
      return;
    }

    let cancelled = false;
    void (async () => {
      try {
        const { Vibrant } = await import("node-vibrant/browser");
        const swatches = await Vibrant.from(imageUrl).getPalette();
        if (cancelled) return;
        const next: VibrantPalette = {
          vibrant: swatches.Vibrant?.rgb
            ? `rgb(${swatches.Vibrant.rgb.map(Math.round).join(", ")})`
            : null,
          muted: swatches.DarkMuted?.rgb
            ? `rgb(${swatches.DarkMuted.rgb.map(Math.round).join(", ")})`
            : null,
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
