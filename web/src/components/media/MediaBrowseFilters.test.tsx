import { describe, it, expect } from "vitest";
import { activeFilterCount, emptyFilters } from "./MediaBrowseFilters";

describe("activeFilterCount", () => {
  it("counts genre / year / rating as separate categories", () => {
    expect(activeFilterCount(emptyFilters)).toBe(0);
    expect(activeFilterCount({ ...emptyFilters, genre: "Drama" })).toBe(1);
    expect(activeFilterCount({ ...emptyFilters, yearFrom: 2000 })).toBe(1);
    expect(activeFilterCount({ ...emptyFilters, yearFrom: 2000, yearTo: 2020 })).toBe(1);
    expect(activeFilterCount({ ...emptyFilters, minRating: 7 })).toBe(1);
    expect(
      activeFilterCount({ genre: "Drama", yearFrom: 1990, yearTo: null, minRating: 5 }),
    ).toBe(3);
  });

  it("treats empty genre / null year / 0 rating as inactive", () => {
    expect(activeFilterCount({ genre: "", yearFrom: null, yearTo: null, minRating: 0 })).toBe(0);
  });
});
