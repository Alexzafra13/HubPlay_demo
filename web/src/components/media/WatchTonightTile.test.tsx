import { describe, it, expect } from "vitest";
import type { MediaItem } from "@/api/types";
import { pickWatchTonight } from "./WatchTonightTile";

function mk(over: Partial<MediaItem> & Record<string, unknown>): MediaItem {
  return {
    id: "x",
    type: "movie",
    title: "T",
    original_title: null,
    year: null,
    sort_title: "t",
    overview: null,
    tagline: null,
    genres: [],
    community_rating: null,
    content_rating: null,
    duration_ticks: null,
    premiere_date: null,
    poster_url: null,
    backdrop_url: null,
    logo_url: null,
    parent_id: null,
    series_id: null,
    season_number: null,
    episode_number: null,
    path: null,
    ...over,
  } as MediaItem;
}

const NOW = new Date("2026-04-27T20:00:00Z").getTime();
const day = 24 * 60 * 60 * 1000;

describe("pickWatchTonight", () => {
  it("returns null when both inputs are empty", () => {
    expect(pickWatchTonight([], [], NOW)).toBeNull();
  });

  it("prefers a recent continue-watching item with resume position", () => {
    const recent = mk({
      id: "r",
      title: "Recent show",
      backdrop_url: "/b.jpg",
      // Synthetic fields the backend adds for /me/continue-watching.
      last_played_at: new Date(NOW - 2 * day).toISOString(),
      position_ticks: 18_000_000_000, // 1800s = 30 min
    } as never);
    const got = pickWatchTonight([recent], [], NOW);
    expect(got?.reason).toBe("resume");
    expect(got?.item.id).toBe("r");
    expect(got?.resumeSeconds).toBe(1800);
  });

  it("skips a continue-watching item older than 14 days", () => {
    const old = mk({
      id: "o",
      title: "Old",
      backdrop_url: "/b.jpg",
      last_played_at: new Date(NOW - 30 * day).toISOString(),
      position_ticks: 1,
    } as never);
    const fresh = mk({
      id: "f",
      title: "Fresh latest",
      backdrop_url: "/b.jpg",
      community_rating: 8.5,
    });
    const got = pickWatchTonight([old], [fresh], NOW);
    // Old continue-watching skipped → falls through to recommendation.
    expect(got?.reason).toBe("recommended");
    expect(got?.item.id).toBe("f");
  });

  it("recommendation picks the highest-rated item with a backdrop", () => {
    const items = [
      mk({ id: "a", community_rating: 6.0, backdrop_url: "/a.jpg" }),
      mk({ id: "b", community_rating: 9.0, backdrop_url: "/b.jpg" }),
      mk({ id: "c", community_rating: 10.0 /* no backdrop, must skip */ }),
      mk({ id: "d", community_rating: 7.0, backdrop_url: "/d.jpg" }),
    ];
    const got = pickWatchTonight([], items, NOW);
    expect(got?.reason).toBe("recommended");
    expect(got?.item.id).toBe("b");
  });

  it("returns null when no latest item has a backdrop", () => {
    // No fallback to poster — the tile is billboard-sized and a
    // poster fallback looks broken at this scale, so we'd rather
    // hide the slot.
    const items = [mk({ id: "x", community_rating: 8.0 })];
    expect(pickWatchTonight([], items, NOW)).toBeNull();
  });

  it("ignores continue-watching entries without a parseable timestamp", () => {
    const broken = mk({
      id: "b",
      backdrop_url: "/b.jpg",
      last_played_at: null,
      position_ticks: 100,
    } as never);
    const fresh = mk({ id: "f", community_rating: 7.0, backdrop_url: "/f.jpg" });
    const got = pickWatchTonight([broken], [fresh], NOW);
    expect(got?.reason).toBe("recommended");
    expect(got?.item.id).toBe("f");
  });
});
