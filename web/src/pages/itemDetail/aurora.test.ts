import { describe, it, expect } from "vitest";
import { buildAuroraStyle } from "./aurora";

// The aurora builder used to live as a 50-line IIFE inside ItemDetail's
// render path; now it's a pure function so the gradient math has its
// own tests. Anchor the contract — no palette ⇒ no styles, palette ⇒
// CSS-var publishes + layered gradients seeded by the swatches —
// against future "I'll just tweak this in place" drift.

describe("buildAuroraStyle", () => {
  it("returns undefined for both styles when palette is missing", () => {
    expect(buildAuroraStyle(undefined)).toEqual({
      detailStyle: undefined,
      auroraBackground: undefined,
    });
  });

  it("returns undefined when palette has neither swatch", () => {
    expect(buildAuroraStyle({})).toEqual({
      detailStyle: undefined,
      auroraBackground: undefined,
    });
  });

  it("publishes --detail-tint and seeds two vibrant blobs from vibrant swatch", () => {
    const { detailStyle, auroraBackground } = buildAuroraStyle({
      vibrant: "rgb(220, 30, 30)",
      muted: "rgb(40, 20, 20)",
    });
    expect(detailStyle).toBeDefined();
    expect(detailStyle!["--detail-tint" as never]).toContain("color-mix");
    expect(detailStyle!["--detail-tint" as never]).toContain("rgb(40, 20, 20)");

    expect(auroraBackground).toBeDefined();
    const bg = auroraBackground!.backgroundImage as string;
    // Two vibrant blobs (upper-left + lower-right) both seeded from
    // the vibrant swatch — the project explicitly demoted the muted
    // swatch from the lower-right after PR #120 because muted reads
    // as "soso" in the part of the page the user scrolls most.
    expect(bg.match(/rgb\(220, 30, 30\)/g)?.length).toBe(2);
    // Plus the muted counter-blob anchored in the centre.
    expect(bg).toContain("rgb(40, 20, 20)");
    // Three radial-gradients composed into the layer stack.
    expect(bg.match(/radial-gradient/g)?.length).toBe(3);
  });

  it("falls back to muted as primary when vibrant is missing", () => {
    const { auroraBackground } = buildAuroraStyle({
      muted: "rgb(40, 20, 20)",
    });
    expect(auroraBackground).toBeDefined();
    const bg = auroraBackground!.backgroundImage as string;
    // Without vibrant the muted swatch carries every blob — the page
    // still gets a tinted canvas instead of falling through to
    // bg-base, but it's monochromatic. Acceptable on monochrome
    // posters where the extractor genuinely couldn't find vibrant.
    expect(bg.match(/rgb\(40, 20, 20\)/g)?.length).toBeGreaterThanOrEqual(2);
  });

  it("seeds tint base from the muted swatch when both are present", () => {
    // The tint base prefers muted because it's by definition darker
    // and reads better as a page background — using vibrant there
    // would over-saturate the surface even at 14% mix.
    const { detailStyle } = buildAuroraStyle({
      vibrant: "rgb(255, 100, 100)",
      muted: "rgb(20, 10, 10)",
    });
    expect(detailStyle!["--detail-tint" as never]).toContain("rgb(20, 10, 10)");
  });
});
