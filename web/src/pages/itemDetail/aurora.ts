import type { CSSProperties } from "react";

// Pure builder for the per-route Plex-style background. Lives here
// (not inside ItemDetail.tsx) so the gradient math is testable on
// its own and the page component stays presentation-glue.
//
// The look matches watch.plex.tv/movie/<slug> exactly: four corner
// radial-gradients on a black base, each fading to transparent at
// 95% so they overlap softly in the centre. What Plex's own CSS
// emits when inspected (Daredevil: Born Again, 2026-05-06):
//
//   background-color: rgb(0 0 0);
//   background-image:
//     radial-gradient(at 0% 0%,    rgb(77, 28, 34),  transparent 95%),
//     radial-gradient(at 100% 0%,  rgb(143, 59, 69), transparent 95%),
//     radial-gradient(at 100% 100%,rgb(91, 35, 39),  transparent 95%),
//     radial-gradient(at 0% 100%,  rgb(28, 4, 6),    transparent 95%);
//
// Notice that EVERY one of Plex's swatches has max(R,G,B) ≤ ~170 —
// their palette extractor (or some pre-processing pass) caps the
// brightness so even the "vibrant" swatch lands in the deep-jewel
// range. node-vibrant (the extractor we use) is more aggressive
// and will happily return rgb(254, 255, 2) on a poster with neon
// yellow text. Pasting that raw into a 95% radial gradient produces
// a fluorescent surface, not Plex's mood.
//
// To stay faithful to the LOOK without depending on the extractor's
// idiosyncrasies, every swatch is run through `normalize()` which
// caps the maximum channel at NORMALIZE_MAX. The transformation is
// proportional — the hue is preserved, only the luminance/intensity
// is scaled down. Already-dark swatches (Plex's own range) pass
// through unchanged.

export type Palette =
  | {
      vibrant?: string;
      muted?: string;
      darkVibrant?: string;
      lightVibrant?: string;
      lightMuted?: string;
    }
  | undefined;

export type AuroraStyle = {
  detailStyle: CSSProperties | undefined;
  auroraBackground: CSSProperties | undefined;
};

// Maximum allowed channel value after normalisation. Tuned against
// Plex's reference distribution where the brightest swatch lands
// around rgb(143, *, *) on Daredevil: Born Again and rgb(171, *, *)
// on Clerks II. 150 lets Plex-range vibrants pass through and only
// kicks in for the genuinely-too-bright cases (node-vibrant's
// rgb(254, 255, 2) on neon-text posters). Lowering this further
// produces washed-out muddy pages; raising it brings back the
// highlighter look on neon posters.
const NORMALIZE_MAX = 150;

function parseRgb(s: string): [number, number, number] | null {
  const m = s.match(/rgb\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)\s*\)/);
  if (!m) return null;
  return [parseInt(m[1], 10), parseInt(m[2], 10), parseInt(m[3], 10)];
}

// normalize — cap the maximum channel at NORMALIZE_MAX while
// preserving the hue (proportional scaling). Returns the original
// string if parsing fails OR if the colour is already within range
// (avoids unnecessary transformation of already-good swatches).
function normalize(color: string): string {
  const rgb = parseRgb(color);
  if (!rgb) return color;
  const max = Math.max(rgb[0], rgb[1], rgb[2]);
  if (max <= NORMALIZE_MAX) return color;
  const scale = NORMALIZE_MAX / max;
  const [r, g, b] = rgb.map((v) => Math.round(v * scale));
  return `rgb(${r}, ${g}, ${b})`;
}

export function buildAuroraStyle(palette: Palette): AuroraStyle {
  // Pick the four corner swatches with sensible fallbacks. Plex's
  // distribution puts the brightest swatch in the top-right (with
  // the backdrop image overlay), darker variants in the other
  // three corners — the priority chains below mirror that. On
  // posters where some swatches don't extract, the chain falls
  // through so every corner still gets filled.
  const tlSeed = palette?.darkVibrant ?? palette?.vibrant ?? palette?.muted;
  const trSeed = palette?.vibrant ?? palette?.lightVibrant ?? palette?.darkVibrant;
  const brSeed = palette?.muted ?? palette?.darkVibrant ?? palette?.vibrant;
  // BL prefers the muted family over lightMuted — earlier revs put
  // lightMuted first and got bright pastel corners that fought the
  // text contrast in the cast section that sits above it.
  const blSeed =
    palette?.muted ??
    palette?.darkVibrant ??
    palette?.lightMuted ??
    palette?.vibrant;

  // No swatch at all → no aurora. Page falls through to bg-base
  // and looks like every other route. Only happens on a cold-start
  // before node-vibrant resolves, or when the URL chain produced
  // no decodable image (no backdrop, no series backdrop, no poster).
  if (!tlSeed && !trSeed && !brSeed && !blSeed) {
    return { detailStyle: undefined, auroraBackground: undefined };
  }

  // Tint base used by hero gradients (`--detail-tint`). Picks the
  // darkest-family swatch so the hero's bottom-fade and the page's
  // lower half land on visually-identical colour — no seam at the
  // hero/body boundary.
  const tintSeed =
    palette?.darkVibrant ?? palette?.muted ?? palette?.vibrant;
  const detailStyle: CSSProperties | undefined = tintSeed
    ? { ["--detail-tint" as string]: normalize(tintSeed) }
    : undefined;

  // Compose the 4-corner background. Corners that don't have a
  // swatch are simply omitted — the remaining corners still anchor
  // the page identity, with black showing through where they fade
  // out. Same trick Plex uses on partially-extracted palettes.
  const layers: string[] = [];
  if (tlSeed)
    layers.push(`radial-gradient(at 0% 0%, ${normalize(tlSeed)}, rgba(0, 0, 0, 0) 95%)`);
  if (trSeed)
    layers.push(`radial-gradient(at 100% 0%, ${normalize(trSeed)}, rgba(0, 0, 0, 0) 95%)`);
  if (brSeed)
    layers.push(`radial-gradient(at 100% 100%, ${normalize(brSeed)}, rgba(0, 0, 0, 0) 95%)`);
  if (blSeed)
    layers.push(`radial-gradient(at 0% 100%, ${normalize(blSeed)}, rgba(0, 0, 0, 0) 95%)`);

  const auroraBackground: CSSProperties = {
    backgroundColor: "rgb(0, 0, 0)",
    backgroundImage: layers.join(", "),
  };
  return { detailStyle, auroraBackground };
}
