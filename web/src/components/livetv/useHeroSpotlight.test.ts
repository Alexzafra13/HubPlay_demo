import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook } from "@testing-library/react";
import type { Channel, EPGProgram } from "@/api/types";

// Mock useUserPreference so we can drive the hook without mounting a
// QueryClientProvider + mocking the /api/me/preferences endpoints.
// The hook under test cares about the {mode, setMode} tuple shape, not
// how the preference is persisted.
const setModeSpy = vi.fn();
let currentMode = "favorites";
vi.mock("@/api/hooks", () => ({
  useUserPreference: () => [currentMode, setModeSpy],
}));

// Import AFTER the mock so the hook picks up the mocked module.
import { useHeroSpotlight } from "./useHeroSpotlight";

// ── Fixtures ────────────────────────────────────────────────────────

const NOW = new Date("2026-04-24T12:00:00Z").getTime();

function channel(
  id: string,
  overrides: Partial<Channel> = {},
): Channel {
  return {
    id,
    library_id: "lib1",
    name: `Channel ${id}`,
    number: 1,
    group: null,
    group_name: null,
    logo_url: null,
    stream_url: `http://stream/${id}`,
    language: "",
    country: "",
    category: "general",
    is_active: true,
    added_at: new Date(NOW).toISOString(),
    logo_initials: id.toUpperCase().slice(0, 2),
    logo_bg: "#000000",
    logo_fg: "#ffffff",
    ...overrides,
  };
}

function liveProgram(channelId: string): EPGProgram {
  return {
    id: `prog-${channelId}`,
    channel_id: channelId,
    title: `Live on ${channelId}`,
    description: "",
    category: "",
    icon_url: "",
    start_time: new Date(NOW - 15 * 60_000).toISOString(),
    end_time: new Date(NOW + 45 * 60_000).toISOString(),
  };
}

describe("useHeroSpotlight", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
    setModeSpy.mockClear();
    currentMode = "favorites";
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("surfaces favorites when the user has them", () => {
    const channels = [channel("a"), channel("b"), channel("c")];
    const favorites = new Set(["b"]);
    const { result } = renderHook(() =>
      useHeroSpotlight({
        channels,
        scheduleByChannel: {},
        favoriteSet: favorites,
      }),
    );

    expect(result.current.mode).toBe("favorites");
    expect(result.current.items).toHaveLength(1);
    expect(result.current.items[0].channel.id).toBe("b");
    expect(result.current.label).toBe("Tu favorito");
  });

  it("silently falls back to live-now when favorites is empty", () => {
    currentMode = "favorites";
    const channels = [channel("a"), channel("b"), channel("c")];
    const { result } = renderHook(() =>
      useHeroSpotlight({
        channels,
        scheduleByChannel: {
          b: [liveProgram("b")],
        },
        favoriteSet: new Set(), // no favorites at all
      }),
    );

    // The user's stored preference is still "favorites" (mode),
    // but what actually renders is the next signal in the fallback
    // chain. The label reflects what the viewer is really seeing.
    expect(result.current.mode).toBe("favorites");
    expect(result.current.label).toBe("En directo ahora");
    expect(result.current.items).toHaveLength(1);
    expect(result.current.items[0].channel.id).toBe("b");
  });

  it("falls all the way through to newest when everything else is empty", () => {
    currentMode = "favorites";
    const older = channel("old", {
      added_at: "2026-01-01T00:00:00Z",
    });
    const newer = channel("new", {
      added_at: "2026-04-23T00:00:00Z",
    });
    const { result } = renderHook(() =>
      useHeroSpotlight({
        channels: [older, newer],
        scheduleByChannel: {}, // no live-now candidates
        favoriteSet: new Set(),
      }),
    );

    expect(result.current.label).toBe("Recién añadidos");
    expect(result.current.items[0].channel.id).toBe("new");
  });

  it("honours the 'off' preference with no fallback", () => {
    currentMode = "off";
    const channels = [channel("a")];
    const { result } = renderHook(() =>
      useHeroSpotlight({
        channels,
        scheduleByChannel: {},
        favoriteSet: new Set(["a"]),
      }),
    );

    expect(result.current.items).toEqual([]);
    expect(result.current.label).toBe("");
    expect(result.current.mode).toBe("off");
  });

  it("caps items to 6 regardless of pool size", () => {
    currentMode = "newest";
    const channels = Array.from({ length: 20 }, (_, i) =>
      channel(`c${i}`, {
        added_at: new Date(NOW - i * 1000).toISOString(),
      }),
    );
    const { result } = renderHook(() =>
      useHeroSpotlight({
        channels,
        scheduleByChannel: {},
        favoriteSet: new Set(),
      }),
    );
    expect(result.current.items).toHaveLength(6);
  });

  it("sorts live-now items by channel number ascending", () => {
    currentMode = "live-now";
    const channels = [
      channel("a", { number: 30 }),
      channel("b", { number: 10 }),
      channel("c", { number: 20 }),
    ];
    const { result } = renderHook(() =>
      useHeroSpotlight({
        channels,
        scheduleByChannel: {
          a: [liveProgram("a")],
          b: [liveProgram("b")],
          c: [liveProgram("c")],
        },
        favoriteSet: new Set(),
      }),
    );
    const order = result.current.items.map((it) => it.channel.id);
    expect(order).toEqual(["b", "c", "a"]);
  });

  it("attaches nowPlaying on items whose channel has EPG", () => {
    currentMode = "favorites";
    const channels = [channel("a")];
    const { result } = renderHook(() =>
      useHeroSpotlight({
        channels,
        scheduleByChannel: { a: [liveProgram("a")] },
        favoriteSet: new Set(["a"]),
      }),
    );
    expect(result.current.items[0].nowPlaying?.title).toBe("Live on a");
  });

  it("exposes the 4 standard mode options with translatable labels", () => {
    const { result } = renderHook(() =>
      useHeroSpotlight({
        channels: [channel("a")],
        scheduleByChannel: {},
        favoriteSet: new Set(),
      }),
    );
    const modes = result.current.modeOptions.map((o) => o.mode);
    expect(modes).toEqual(["favorites", "live-now", "newest", "off"]);
    // Every option carries a label and a hint (i18n defaultValue fallback).
    for (const opt of result.current.modeOptions) {
      expect(opt.label).toBeTruthy();
      expect(opt.hint).toBeTruthy();
    }
  });

  it("setMode forwards to the underlying preference setter", () => {
    const { result } = renderHook(() =>
      useHeroSpotlight({
        channels: [channel("a")],
        scheduleByChannel: {},
        favoriteSet: new Set(),
      }),
    );
    result.current.setMode("live-now");
    expect(setModeSpy).toHaveBeenCalledWith("live-now");
  });
});
