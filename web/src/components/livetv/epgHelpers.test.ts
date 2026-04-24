import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import type { EPGProgram } from "@/api/types";
import {
  capitalize,
  formatTime,
  getNowPlaying,
  getProgramProgress,
  getUpNext,
} from "./epgHelpers";

// Fixed clock — every test anchors to 2026-04-24T12:00:00Z so assertions
// don't drift with real time. `vi.setSystemTime` makes Date.now() return
// the mocked instant; helpers read `Date.now()` internally.
const NOW = new Date("2026-04-24T12:00:00Z").getTime();

function program(
  id: string,
  startOffsetMin: number,
  durationMin: number,
  overrides: Partial<EPGProgram> = {},
): EPGProgram {
  const start = new Date(NOW + startOffsetMin * 60_000);
  const end = new Date(start.getTime() + durationMin * 60_000);
  return {
    id,
    channel_id: "ch1",
    title: `Program ${id}`,
    description: "",
    category: "",
    icon_url: "",
    start_time: start.toISOString(),
    end_time: end.toISOString(),
    ...overrides,
  };
}

describe("epgHelpers", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  describe("getNowPlaying", () => {
    it("returns null on undefined or empty input", () => {
      expect(getNowPlaying(undefined)).toBeNull();
      expect(getNowPlaying([])).toBeNull();
    });

    it("returns the program whose window contains now", () => {
      const past = program("past", -60, 30); // -60..-30 (ended)
      const live = program("live", -15, 60); // -15..+45 (current)
      const future = program("future", 60, 30); // +60..+90
      expect(getNowPlaying([past, live, future])?.id).toBe("live");
    });

    it("returns null when now falls between programs", () => {
      const a = program("a", -60, 30); // ended at -30
      const b = program("b", 30, 30); // starts at +30
      expect(getNowPlaying([a, b])).toBeNull();
    });

    it("treats start as inclusive and end as exclusive", () => {
      // Program starting exactly at "now" is live.
      const startsNow = program("starts", 0, 30);
      expect(getNowPlaying([startsNow])?.id).toBe("starts");
      // Program ending exactly at "now" is NOT live anymore.
      const endsNow = program("ends", -30, 30);
      expect(getNowPlaying([endsNow])).toBeNull();
    });
  });

  describe("getUpNext", () => {
    it("returns null on undefined or empty input", () => {
      expect(getUpNext(undefined)).toBeNull();
      expect(getUpNext([])).toBeNull();
    });

    it("returns the first program whose start is in the future", () => {
      const past = program("past", -60, 30);
      const live = program("live", -15, 60);
      const next = program("next", 60, 30);
      const later = program("later", 120, 30);
      expect(getUpNext([past, live, next, later])?.id).toBe("next");
    });

    it("returns null when nothing is upcoming", () => {
      const past = program("past", -60, 30);
      const live = program("live", -15, 60);
      expect(getUpNext([past, live])).toBeNull();
    });

    it("respects input order (no client-side sort)", () => {
      // The helper trusts the backend's ORDER BY start_time; if callers
      // hand us unsorted data we just return the first future entry we
      // encounter, not the earliest one. This pins the documented contract.
      const late = program("late", 120, 30);
      const early = program("early", 60, 30);
      expect(getUpNext([late, early])?.id).toBe("late");
    });
  });

  describe("getProgramProgress", () => {
    it("returns 0 before the program starts", () => {
      expect(getProgramProgress(program("x", 30, 60))).toBe(0);
    });

    it("returns 100 after the program ends", () => {
      expect(getProgramProgress(program("x", -120, 60))).toBe(100);
    });

    it("returns halfway progress at program midpoint", () => {
      expect(getProgramProgress(program("x", -30, 60))).toBeCloseTo(50, 1);
    });

    it("returns 0 for zero-duration programs (defensive)", () => {
      const zero: EPGProgram = {
        ...program("z", 0, 0),
        end_time: new Date(NOW).toISOString(),
      };
      expect(getProgramProgress(zero)).toBe(0);
    });
  });

  describe("formatTime", () => {
    it("renders HH:MM in the host locale", () => {
      // We don't pin the exact string because toLocaleTimeString respects
      // the JS runtime's locale, but the result should always contain a
      // colon and two-digit hour/minute segments.
      const out = formatTime(new Date(NOW).toISOString());
      expect(out).toMatch(/\d{2}:\d{2}/);
    });
  });

  describe("capitalize", () => {
    it("uppercases the first letter", () => {
      expect(capitalize("hello")).toBe("Hello");
    });

    it("leaves an empty string alone", () => {
      expect(capitalize("")).toBe("");
    });

    it("leaves an already-capitalised string unchanged", () => {
      expect(capitalize("News")).toBe("News");
    });

    it("only touches the first character", () => {
      expect(capitalize("iPTV")).toBe("IPTV");
    });
  });
});
