import { describe, it, expect } from "vitest";
import { federationItemToMediaItem } from "./federationAdapter";
import type { FederationRemoteItem } from "./types";

// federationItemToMediaItem is the only path that converts a
// peer-returned item into the canonical MediaItem shape the local
// UI consumes. The tests here pin the contract the page-wide aurora
// depends on: when the wire carries backdrop_colors, the adapter
// must forward them — otherwise PeerItemDetail's hasServerPalette
// check fails and node-vibrant kicks in for no reason.

const baseItem: FederationRemoteItem = {
  id: "rem-1",
  type: "movie",
  title: "Aurora",
  year: 2024,
  overview: "test",
  poster_url: "/api/v1/me/peers/p/items/rem-1/poster",
};

describe("federationItemToMediaItem", () => {
  it("forwards backdrop_colors when present", () => {
    const out = federationItemToMediaItem({
      ...baseItem,
      backdrop_colors: { vibrant: "rgb(10, 20, 30)", muted: "rgb(1, 2, 3)" },
    });
    expect(out.backdrop_colors).toEqual({
      vibrant: "rgb(10, 20, 30)",
      muted: "rgb(1, 2, 3)",
    });
  });

  it("forwards a half-extracted palette (vibrant only) unchanged", () => {
    // Extraction can land one swatch but not the other on monochrome
    // posters. The adapter must NOT synthesise the missing field — it
    // would corrupt the aurora.ts fallback chain.
    const out = federationItemToMediaItem({
      ...baseItem,
      backdrop_colors: { vibrant: "rgb(99, 11, 22)" },
    });
    expect(out.backdrop_colors).toEqual({ vibrant: "rgb(99, 11, 22)" });
    expect(out.backdrop_colors?.muted).toBeUndefined();
  });

  it("omits backdrop_colors when the wire didn't carry them", () => {
    // Older peers + items pre-migration 014. The frontend's
    // hasServerPalette check must read undefined here so the
    // node-vibrant fallback engages.
    const out = federationItemToMediaItem(baseItem);
    expect(out.backdrop_colors).toBeUndefined();
  });

  it("preserves backdrop_colors alongside the existing field mapping", () => {
    // Quick regression guard: the previous fields the adapter was
    // already filling (title, type, poster_url) must not regress as
    // the colour plumbing lands.
    const out = federationItemToMediaItem({
      ...baseItem,
      backdrop_colors: { vibrant: "rgb(5, 5, 5)", muted: "rgb(7, 7, 7)" },
    });
    expect(out.id).toBe("rem-1");
    expect(out.type).toBe("movie");
    expect(out.title).toBe("Aurora");
    expect(out.poster_url).toBe("/api/v1/me/peers/p/items/rem-1/poster");
    expect(out.backdrop_colors?.vibrant).toBe("rgb(5, 5, 5)");
  });
});
