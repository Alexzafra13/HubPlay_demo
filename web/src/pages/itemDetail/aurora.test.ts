import { describe, it, expect } from "vitest";
import { buildAuroraStyle } from "./aurora";

// The aurora builder emits a Plex-style 4-corner radial-gradient
// stack on a black base. See aurora.ts for the look reference,
// the per-corner fallback chain, and the rationale for the
// `dim()` color-mix wrapper. These tests anchor the contract so a
// future "let me just tweak the gradient" doesn't drift the surface
// back into the muddy single-tint look OR the fluorescent raw-RGB
// look (both of which we've shipped and walked back from).

describe("buildAuroraStyle", () => {
  it("returns undefined for both styles when palette is missing", () => {
    expect(buildAuroraStyle(undefined)).toEqual({
      detailStyle: undefined,
      auroraBackground: undefined,
    });
  });

  it("returns undefined when palette has no swatches at all", () => {
    expect(buildAuroraStyle({})).toEqual({
      detailStyle: undefined,
      auroraBackground: undefined,
    });
  });

  it("emits four corner gradients on a black base when all swatches are present", () => {
    const { detailStyle, auroraBackground } = buildAuroraStyle({
      vibrant: "rgb(220, 30, 30)",
      darkVibrant: "rgb(89, 10, 13)",
      lightVibrant: "rgb(255, 100, 100)",
      muted: "rgb(46, 25, 67)",
      lightMuted: "rgb(87, 62, 139)",
    });

    expect(auroraBackground).toBeDefined();
    expect(auroraBackground!.backgroundColor).toBe("rgb(0, 0, 0)");

    const bg = auroraBackground!.backgroundImage as string;
    // Four radial-gradient layers — one per corner. Anchored to
    // the corners (0%/100%) so the gradients overlap in the centre.
    expect(bg.match(/radial-gradient/g)?.length).toBe(4);
    expect(bg).toContain("at 0% 0%");
    expect(bg).toContain("at 100% 0%");
    expect(bg).toContain("at 100% 100%");
    expect(bg).toContain("at 0% 100%");

    // Top-left = darkVibrant, top-right = vibrant — that pairing
    // gives a "warm gradient across the top" effect identical to
    // Plex's reference. Bottom corners pull from the muted family
    // so the lower half sits a touch cooler / less saturated.
    expect(bg).toContain("rgb(89, 10, 13)");
    expect(bg).toContain("rgb(220, 30, 30)");
    expect(bg).toContain("rgb(46, 25, 67)");
    expect(bg).toContain("rgb(87, 62, 139)");

    // Every swatch is wrapped in color-mix(...60% colour, 40% black)
    // to tame node-vibrant's tendency to return brilliant raw
    // vibrants on posters with neon text. Without this wrap, a
    // poster like Den of Thieves (vibrant=rgb(254,255,2)) paints
    // the page fluorescent yellow.
    expect(bg.match(/color-mix\(in srgb,/g)?.length).toBe(4);

    // 75% transparent stop (Plex uses 95%, we tightened it because
    // dim()'d colours need the centre to stay mostly black to
    // avoid mud where the four corners overlap).
    expect(bg).toContain("rgba(0, 0, 0, 0) 75%");

    // detail-tint published for the hero gradient — uses the
    // darkest available swatch (darkVibrant when present), wrapped
    // in the same dim() so the hero/body seam lands on identical
    // colour.
    expect(detailStyle!["--detail-tint" as never]).toContain("color-mix");
    expect(detailStyle!["--detail-tint" as never]).toContain("rgb(89, 10, 13)");
  });

  it("falls through the corner priority chain when only vibrant is extracted", () => {
    const { auroraBackground } = buildAuroraStyle({
      vibrant: "rgb(254, 255, 2)",
    });
    expect(auroraBackground).toBeDefined();
    const bg = auroraBackground!.backgroundImage as string;
    // Every corner falls back to vibrant — page is still tinted
    // (no holes) just monochromatic. The dim() wrap is what saves
    // this case from being unbearable: rgb(254, 255, 2) becomes
    // a deep mustard ~rgb(152, 153, 1) when mixed 60/40 with black.
    expect(bg.match(/radial-gradient/g)?.length).toBe(4);
    expect(bg.match(/rgb\(254, 255, 2\)/g)?.length).toBe(4);
    expect(bg.match(/color-mix/g)?.length).toBe(4);
  });

  it("omits corners that have no swatch and no fallback", () => {
    // Only darkVibrant + muted — the corner picks resolve through
    // the priority chain so all four corners still get filled, just
    // by repeating the available swatches. No undefined entries
    // ever land in the gradient string.
    const { auroraBackground } = buildAuroraStyle({
      darkVibrant: "rgb(50, 80, 200)",
      muted: "rgb(20, 30, 60)",
    });
    expect(auroraBackground).toBeDefined();
    const bg = auroraBackground!.backgroundImage as string;
    expect(bg.match(/radial-gradient/g)?.length).toBe(4);
    expect(bg).not.toContain("undefined");
  });

  it("seeds --detail-tint from the darkest available swatch", () => {
    // darkVibrant present → wins; even though vibrant is brighter
    // we want the page tint dark so cards/text sitting over it
    // stay readable. Hero left-fade lands on this same colour.
    const { detailStyle } = buildAuroraStyle({
      vibrant: "rgb(255, 100, 100)",
      darkVibrant: "rgb(60, 10, 10)",
      muted: "rgb(20, 10, 10)",
    });
    expect(detailStyle!["--detail-tint" as never]).toContain("rgb(60, 10, 10)");
  });
});
