import type { CSSProperties } from "react";

// Pure builder for the per-route ambient-aurora canvas style. Lives
// here (not inside ItemDetail.tsx) so the gradient math is testable
// on its own and the page component stays presentation-glue.
//
// Inputs are the two CSS rgb() strings the backend extracts from the
// primary backdrop (see internal/imaging/colors.go::ExtractDominantColors)
// — vibrant for "page identity" and muted as a backstop. Both are
// optional because older rows scanned before extraction shipped will
// be missing one or both.
//
// Returns:
//   - detailStyle: the wrapper inline style that publishes
//     `--detail-tint` so the hero's bottom-fade gradient targets the
//     exact base colour the canvas paints in. null when no palette is
//     present (page falls through to bg-base).
//   - auroraBackground: the full-viewport-canvas inline style; only
//     defined when at least one swatch is present.
//
// The intensity tuning (60% / 50% / 28%) and blob positioning come
// from the iterative pass on PRs #117-#120; do NOT lower without
// re-eyeballing on rich-coloured posters where the original fade was
// invisible.

export type Palette = { vibrant?: string; muted?: string } | undefined;

export type AuroraStyle = {
  detailStyle: CSSProperties | undefined;
  auroraBackground: CSSProperties | undefined;
};

export function buildAuroraStyle(palette: Palette): AuroraStyle {
  const tintSeed = palette?.muted ?? palette?.vibrant;
  if (!tintSeed) {
    return { detailStyle: undefined, auroraBackground: undefined };
  }

  const tintBase = `color-mix(in srgb, ${tintSeed} 14%, rgb(8 12 16))`;
  const detailStyle: CSSProperties = {
    ["--detail-tint" as string]: tintBase,
  };

  // Both blobs prefer the VIBRANT swatch — by definition it's the
  // most saturated colour the palette extracted, so it carries the
  // page identity. Muted falls in only as a backstop for items whose
  // vibrant slot couldn't be filled (rare, but happens on monochrome
  // posters). Earlier revisions used muted for the lower-right blob,
  // which read as "soso" because muted IS by definition desaturated;
  // the lower half of the page is exactly where the user spends the
  // most time scrolling, so it's the wrong place to dial colour back.
  const primary = palette?.vibrant ?? palette?.muted;
  const secondary = palette?.muted ?? palette?.vibrant;
  const layers: string[] = [];
  if (primary) {
    // Upper-left vibrant blob — covers the hero left side and bleeds
    // into the seasons-grid headline area. Big radius so the bleed
    // reads as "the whole top of the page is tinted", not "there's a
    // circle of red here".
    layers.push(
      `radial-gradient(ellipse 100% 80% at 10% 0%, color-mix(in srgb, ${primary} 60%, transparent) 0%, transparent 65%)`,
    );
    // Lower-right vibrant blob — the seasons grid + cast strip sit
    // here. Same vibrant swatch but slightly muted via the mix
    // percentage so foreground text stays readable.
    layers.push(
      `radial-gradient(ellipse 90% 90% at 90% 100%, color-mix(in srgb, ${primary} 50%, transparent) 0%, transparent 70%)`,
    );
  }
  if (secondary) {
    // Cooler counter-blob: balances the warm primary with a softer
    // accent so the whole canvas isn't a single hue.
    layers.push(
      `radial-gradient(circle 55% at 50% 55%, color-mix(in srgb, ${secondary} 28%, transparent) 0%, transparent 75%)`,
    );
  }

  const auroraBackground: CSSProperties = {
    backgroundColor: tintBase,
    backgroundImage: layers.join(", "),
  };
  return { detailStyle, auroraBackground };
}
