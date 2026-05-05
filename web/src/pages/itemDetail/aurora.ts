import type { CSSProperties } from "react";

// Pure builder for the per-route Plex-style background. Lives here
// (not inside ItemDetail.tsx) so the gradient math is testable on
// its own and the page component stays presentation-glue.
//
// The look is borrowed directly from watch.plex.tv/movie/<slug>:
// four corner radial-gradients on a black base, each fading to
// transparent so they overlap softly in the centre. What Plex's
// own CSS shows when inspected:
//
//   background-color: rgb(0 0 0);
//   background-image:
//     radial-gradient(at 0% 0%,    rgb(89, 10, 13),   transparent 95%),
//     radial-gradient(at 100% 0%,  rgb(171, 31, 35),  transparent 95%),
//     radial-gradient(at 100% 100%,rgb(46, 25, 67),   transparent 95%),
//     radial-gradient(at 0% 100%,  rgb(87, 62, 139),  transparent 95%);
//
// Note that EVERY one of Plex's swatches there is < rgb(200, *, *) —
// their palette extractor returns swatches that are already dark
// enough to read as a page background. node-vibrant (the extractor
// we use) is more aggressive and will happily return rgb(254, 255, 2)
// on a poster with bright yellow text. Pasting that raw into a 95%
// radial gradient produces a fluorescent surface, not Plex's mood.
//
// To stay faithful to the LOOK without depending on the extractor's
// idiosyncrasies, every swatch is dimmed via color-mix with black
// before being inlined. The mix is mild (60% colour + 40% black)
// so already-dark swatches barely shift, but bright ones get cut
// down into the same "deep saturated jewel" range Plex uses.
// Combined with a 75% transparent stop (instead of Plex's 95%),
// the centre of the canvas keeps a healthy amount of black showing
// through — the gradients land as accents, not floods.

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

// dim — mix colour with black so brilliant vibrants don't burn the
// canvas. 60% keeps the hue identifiable; 40% black knocks the
// luminance down enough that even rgb(254, 255, 2) lands as a
// readable mustard rather than a highlighter pen.
function dim(color: string): string {
  return `color-mix(in srgb, ${color} 60%, rgb(0, 0, 0))`;
}

export function buildAuroraStyle(palette: Palette): AuroraStyle {
  // Pick the four corner swatches with sensible fallbacks. The
  // priorities mean a poster with only one swatch extracted still
  // gets a working (if monochromatic) 4-corner composition — every
  // corner falls back to whatever's available.
  const tlSeed = palette?.darkVibrant ?? palette?.vibrant ?? palette?.muted;
  const trSeed = palette?.vibrant ?? palette?.lightVibrant ?? palette?.darkVibrant;
  const brSeed = palette?.muted ?? palette?.darkVibrant ?? palette?.vibrant;
  const blSeed =
    palette?.lightMuted ??
    palette?.lightVibrant ??
    palette?.vibrant ??
    palette?.muted;

  // No swatch at all → no aurora. Page falls through to bg-base
  // and looks like every other route. Only happens on a cold-start
  // before node-vibrant resolves, or when the URL chain produced
  // no decodable image (no backdrop, no series backdrop, no poster).
  if (!tlSeed && !trSeed && !brSeed && !blSeed) {
    return { detailStyle: undefined, auroraBackground: undefined };
  }

  // Tint base used by hero gradients (`--detail-tint`). Picks the
  // darkest available swatch and dims it the same way the corners
  // are dimmed, so the hero's bottom-fade and the page's lower
  // half land on visually-identical colour — no seam at the hero/
  // body boundary.
  const tintSeed =
    palette?.darkVibrant ?? palette?.muted ?? palette?.vibrant;
  const detailStyle: CSSProperties | undefined = tintSeed
    ? { ["--detail-tint" as string]: dim(tintSeed) }
    : undefined;

  // Compose the 4-corner background. Corners that don't have a
  // swatch are simply omitted — the remaining corners still anchor
  // the page identity, with black showing through where they fade
  // out. Same trick Plex uses on partially-extracted palettes.
  const layers: string[] = [];
  if (tlSeed)
    layers.push(`radial-gradient(at 0% 0%, ${dim(tlSeed)}, rgba(0, 0, 0, 0) 75%)`);
  if (trSeed)
    layers.push(`radial-gradient(at 100% 0%, ${dim(trSeed)}, rgba(0, 0, 0, 0) 75%)`);
  if (brSeed)
    layers.push(`radial-gradient(at 100% 100%, ${dim(brSeed)}, rgba(0, 0, 0, 0) 75%)`);
  if (blSeed)
    layers.push(`radial-gradient(at 0% 100%, ${dim(blSeed)}, rgba(0, 0, 0, 0) 75%)`);

  const auroraBackground: CSSProperties = {
    backgroundColor: "rgb(0, 0, 0)",
    backgroundImage: layers.join(", "),
  };
  return { detailStyle, auroraBackground };
}
