import { describe, it, expect } from "vitest";
import type { MediaItem } from "@/api/types";
import {
  applyFilters,
  activeFilterCount,
  emptyFilters,
  type BrowseFiltersState,
} from "./MediaBrowseFilters";

function mkItem(over: Partial<MediaItem>): MediaItem {
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
    runtime_ticks: null,
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
  };
}

describe("activeFilterCount", () => {
  it("counts genres / year / rating as separate categories", () => {
    expect(activeFilterCount(emptyFilters)).toBe(0);
    expect(activeFilterCount({ ...emptyFilters, genres: new Set(["Drama"]) })).toBe(1);
    expect(activeFilterCount({ ...emptyFilters, yearFrom: 2000 })).toBe(1);
    expect(activeFilterCount({ ...emptyFilters, yearFrom: 2000, yearTo: 2020 })).toBe(1);
    expect(activeFilterCount({ ...emptyFilters, minRating: 7 })).toBe(1);
    expect(
      activeFilterCount({ genres: new Set(["A"]), yearFrom: 1990, yearTo: null, minRating: 5 }),
    ).toBe(3);
  });
});

describe("applyFilters", () => {
  const items: MediaItem[] = [
    mkItem({ id: "a", title: "Action 99", year: 1999, community_rating: 8.0, genres: ["Action", "Drama"] }),
    mkItem({ id: "b", title: "Drama 05", year: 2005, community_rating: 7.0, genres: ["Drama"] }),
    mkItem({ id: "c", title: "Comedy 20", year: 2020, community_rating: 5.0, genres: ["Comedy"] }),
    mkItem({ id: "d", title: "Unrated 88", year: 1988, community_rating: null, genres: ["Action"] }),
    mkItem({ id: "e", title: "No year", year: null, community_rating: 9.0, genres: ["Drama"] }),
  ];

  it("returns identity when no filter is active", () => {
    expect(applyFilters(items, emptyFilters)).toBe(items);
  });

  it("genre filter is OR (any match) and case-insensitive", () => {
    const f: BrowseFiltersState = {
      ...emptyFilters,
      genres: new Set(["drama"]),
    };
    expect(applyFilters(items, f).map((i) => i.id)).toEqual(["a", "b", "e"]);
  });

  it("year range bounds are inclusive; items with null year bypass", () => {
    const f: BrowseFiltersState = {
      ...emptyFilters,
      yearFrom: 2000,
      yearTo: 2010,
    };
    // b (2005) inside; e (null year) bypasses; rest excluded.
    expect(applyFilters(items, f).map((i) => i.id)).toEqual(["b", "e"]);
  });

  it("min rating excludes items below threshold but keeps unrated items", () => {
    const f: BrowseFiltersState = { ...emptyFilters, minRating: 7.5 };
    // a (8.0) ok; b (7.0) below; c (5.0) below; d (null) bypasses; e (9.0) ok.
    expect(applyFilters(items, f).map((i) => i.id)).toEqual(["a", "d", "e"]);
  });

  it("combines filters AND-style across categories", () => {
    const f: BrowseFiltersState = {
      genres: new Set(["Drama"]),
      yearFrom: 2000,
      yearTo: null,
      minRating: 6.5,
    };
    // Of {a,b,e} (drama), only b (2005, 7.0) and e (null year, 9.0)
    // pass the year + rating combo. a is 1999 → out.
    expect(applyFilters(items, f).map((i) => i.id)).toEqual(["b", "e"]);
  });
});
