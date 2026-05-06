import { describe, it, expect } from "vitest";
import { buildAuroraStyle } from "./aurora";

// The aurora builder emits a Plex-style 4-corner radial-gradient
// stack on a black base. See aurora.ts for the look reference,
// the per-corner fallback chain, and the rationale for `normalize()`.
// These tests anchor the contract so a future "let me just tweak
// the gradient" doesn't drift the surface back into the muddy
// single-tint look OR the fluorescent raw-RGB look (we've shipped
// both and walked them back).

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
      vibrant: "rgb(143, 59, 69)",
      darkVibrant: "rgb(77, 28, 34)",
      lightVibrant: "rgb(200, 100, 110)",
      muted: "rgb(91, 35, 39)",
      lightMuted: "rgb(120, 80, 90)",
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

    // All four input swatches were already within the
    // NORMALIZE_MAX cap (max channel ≤ 200), so they pass through
    // unchanged. The Plex distribution is exactly this — every
    // swatch in the deep-jewel range, never highlighter-bright.
    expect(bg).toContain("rgb(77, 28, 34)");
    expect(bg).toContain("rgb(143, 59, 69)");
    expect(bg).toContain("rgb(91, 35, 39)");

    // 95% transparent stop matches Plex's exact reference. With
    // normalised colours we don't need to tighten this further.
    expect(bg.match(/rgba\(0, 0, 0, 0\) 95%/g)?.length).toBe(4);

    // detail-tint published for the hero — uses the darkest-family
    // swatch (darkVibrant when present), passed through normalize()
    // for the same hero/body seam consistency.
    expect(detailStyle!["--detail-tint" as never]).toBe("rgb(77, 28, 34)");
  });

  it("normalises bright vibrants by scaling proportionally to NORMALIZE_MAX", () => {
    // node-vibrant happily returns rgb(254, 255, 2) on a poster
    // with neon yellow text (e.g. Den of Thieves). Without the
    // clamp the page reads as a highlighter; with it, the swatch
    // is scaled proportionally so the hue is preserved but the
    // luminance lands in Plex's range.
    const { auroraBackground } = buildAuroraStyle({
      vibrant: "rgb(254, 255, 2)",
    });
    expect(auroraBackground).toBeDefined();
    const bg = auroraBackground!.backgroundImage as string;
    // Raw rgb should NOT appear — the clamp must have rewritten it.
    expect(bg).not.toContain("rgb(254, 255, 2)");
    // Scaled values: max=255 → scale=150/255 ≈ 0.588 →
    // 254*0.588≈149, 255*0.588=150, 2*0.588≈1.
    expect(bg).toContain("rgb(149, 150, 1)");
  });

  it("leaves dark Plex-range swatches untouched", () => {
    // Plex's reference for Daredevil: Born Again. Every swatch
    // already sits below the cap so normalize() returns them
    // unchanged. Anchors the contract that we don't accidentally
    // dim swatches that are already at the right intensity.
    const { auroraBackground } = buildAuroraStyle({
      darkVibrant: "rgb(77, 28, 34)",
      vibrant: "rgb(143, 59, 69)",
      muted: "rgb(91, 35, 39)",
      lightMuted: "rgb(28, 4, 6)",
    });
    expect(auroraBackground).toBeDefined();
    const bg = auroraBackground!.backgroundImage as string;
    expect(bg).toContain("rgb(77, 28, 34)");
    expect(bg).toContain("rgb(143, 59, 69)");
    expect(bg).toContain("rgb(91, 35, 39)");
    // Note: lightMuted is in the BL chain only as a fallback, the
    // primary BL pick here is `muted` (rgb(91, 35, 39)). lightMuted
    // wouldn't appear in the gradient unless muted+darkVibrant were
    // both null.
  });

  it("falls through the corner priority chain when only vibrant is extracted", () => {
    const { auroraBackground } = buildAuroraStyle({
      vibrant: "rgb(143, 59, 69)",
    });
    expect(auroraBackground).toBeDefined();
    const bg = auroraBackground!.backgroundImage as string;
    // Every corner falls back to vibrant — page is still tinted
    // (no holes) just monochromatic. Acceptable on monochrome
    // posters where node-vibrant only finds one strong colour.
    expect(bg.match(/radial-gradient/g)?.length).toBe(4);
    expect(bg.match(/rgb\(143, 59, 69\)/g)?.length).toBe(4);
  });

  it("omits no corners when at least two swatches are present", () => {
    // darkVibrant + muted → all four corner picks resolve through
    // the priority chain. No undefined entries ever land in the
    // gradient string.
    const { auroraBackground } = buildAuroraStyle({
      darkVibrant: "rgb(50, 80, 120)",
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
      vibrant: "rgb(143, 59, 69)",
      darkVibrant: "rgb(60, 10, 10)",
      muted: "rgb(20, 10, 10)",
    });
    expect(detailStyle!["--detail-tint" as never]).toBe("rgb(60, 10, 10)");
  });
});
