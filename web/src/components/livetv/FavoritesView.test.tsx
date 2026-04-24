import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import type { Channel, EPGProgram } from "@/api/types";
import { FavoritesView } from "./FavoritesView";

const NOW = new Date("2026-04-24T12:00:00Z").getTime();

function channel(
  id: string,
  name: string,
  overrides: Partial<Channel> = {},
): Channel {
  return {
    id,
    library_id: "lib1",
    name,
    number: 1,
    group: null,
    group_name: null,
    category: "general",
    logo_initials: id.toUpperCase().slice(0, 2),
    logo_bg: "#111",
    logo_fg: "#fff",
    logo_url: null,
    stream_url: `http://stream/${id}`,
    language: "",
    country: "",
    is_active: true,
    added_at: new Date(NOW).toISOString(),
    ...overrides,
  };
}

describe("FavoritesView", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders an empty state when the user has no favorites", () => {
    render(
      <FavoritesView
        channels={[channel("a", "Alpha")]}
        favoriteSet={new Set()}
        scheduleByChannel={{}}
        onOpen={vi.fn()}
        onToggleFavorite={vi.fn()}
      />,
    );
    // Heart glyph and help text are both present.
    expect(
      screen.getByText(/Aún no tienes favoritos/),
    ).toBeInTheDocument();
  });

  it("renders only the channels present in favoriteSet", () => {
    const channels = [
      channel("a", "Alpha"),
      channel("b", "Bravo"),
      channel("c", "Charlie"),
    ];
    render(
      <FavoritesView
        channels={channels}
        favoriteSet={new Set(["a", "c"])}
        scheduleByChannel={{}}
        onOpen={vi.fn()}
        onToggleFavorite={vi.fn()}
      />,
    );
    expect(screen.getByText("Alpha")).toBeInTheDocument();
    expect(screen.queryByText("Bravo")).toBeNull();
    expect(screen.getByText("Charlie")).toBeInTheDocument();
  });

  /**
   * Channels removed by an M3U refresh disappear from `channels` but may
   * still be in the stored favoriteSet. The view must not render them as
   * ghost cards — the intersection of the two inputs is what shows.
   */
  it("silently drops stale favorites that are not in channels anymore", () => {
    render(
      <FavoritesView
        channels={[channel("a", "Alpha")]}
        favoriteSet={new Set(["a", "deleted-id"])}
        scheduleByChannel={{}}
        onOpen={vi.fn()}
        onToggleFavorite={vi.fn()}
      />,
    );
    expect(screen.getByText("Alpha")).toBeInTheDocument();
    // No "deleted-id" ghost anywhere in the DOM.
    expect(screen.queryByText(/deleted-id/i)).toBeNull();
  });

  it("fires onOpen with the favorited channel when its card is clicked", () => {
    const onOpen = vi.fn();
    const alpha = channel("a", "Alpha");
    render(
      <FavoritesView
        channels={[alpha]}
        favoriteSet={new Set(["a"])}
        scheduleByChannel={{}}
        onOpen={onOpen}
        onToggleFavorite={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /Alpha/ }));
    expect(onOpen).toHaveBeenCalledWith(alpha);
  });

  it("fires onToggleFavorite with the channel id on heart click", () => {
    const onToggle = vi.fn();
    render(
      <FavoritesView
        channels={[channel("a", "Alpha")]}
        favoriteSet={new Set(["a"])}
        scheduleByChannel={{}}
        onOpen={vi.fn()}
        onToggleFavorite={onToggle}
      />,
    );
    fireEvent.click(
      screen.getByRole("button", { name: /Quitar de favoritos/ }),
    );
    expect(onToggle).toHaveBeenCalledWith("a");
  });

  it("passes now-playing / up-next data to the card when EPG is present", () => {
    const nowProg: EPGProgram = {
      id: "p-now",
      channel_id: "a",
      title: "On right now",
      description: "",
      category: "",
      icon_url: "",
      start_time: new Date(NOW - 15 * 60_000).toISOString(),
      end_time: new Date(NOW + 45 * 60_000).toISOString(),
    };
    render(
      <FavoritesView
        channels={[channel("a", "Alpha")]}
        favoriteSet={new Set(["a"])}
        scheduleByChannel={{ a: [nowProg] }}
        onOpen={vi.fn()}
        onToggleFavorite={vi.fn()}
      />,
    );
    expect(screen.getByText("On right now")).toBeInTheDocument();
    expect(screen.getByText("Live")).toBeInTheDocument();
  });
});
