import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { NowNextRow } from "./NowNextRow";
import type { Channel, EPGProgram } from "@/api/types";

const NOW = new Date("2026-04-24T18:30:00Z").getTime();

function channel(overrides: Partial<Channel> = {}): Channel {
  return {
    id: "ch1",
    library_id: "lib1",
    name: "ESPN",
    number: 112,
    group: null,
    group_name: null,
    category: "sports",
    logo_initials: "ES",
    logo_bg: "#111",
    logo_fg: "#fff",
    logo_url: null,
    stream_url: "http://stream/ch1",
    language: "",
    country: "",
    is_active: true,
    ...overrides,
  };
}

function program(
  id: string,
  startOffsetMin: number,
  durMin: number,
  title: string,
): EPGProgram {
  const start = NOW + startOffsetMin * 60_000;
  return {
    id,
    channel_id: "ch1",
    title,
    description: null,
    start_time: new Date(start).toISOString(),
    end_time: new Date(start + durMin * 60_000).toISOString(),
    category: null,
    icon_url: null,
  };
}

describe("NowNextRow", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders the now-playing title and the up-next title", () => {
    render(
      <NowNextRow
        channel={channel()}
        nowPlaying={program("now", -20, 60, "NBA: Lakers @ Celtics")}
        upNext={program("next", 40, 30, "SportsCenter")}
        isFavorite={false}
        onClick={vi.fn()}
        onToggleFavorite={vi.fn()}
      />,
    );
    expect(screen.getByText("NBA: Lakers @ Celtics")).toBeInTheDocument();
    expect(screen.getByText("SportsCenter")).toBeInTheDocument();
    expect(screen.getByText(/CH 112/)).toBeInTheDocument();
  });

  it("falls back to a no-guide label when there is no program", () => {
    render(
      <NowNextRow
        channel={channel()}
        nowPlaying={null}
        upNext={null}
        isFavorite={false}
        onClick={vi.fn()}
        onToggleFavorite={vi.fn()}
      />,
    );
    expect(screen.getByText(/Sin guía disponible/i)).toBeInTheDocument();
  });

  it("clicking the row calls onClick but not onToggleFavorite", () => {
    const onClick = vi.fn();
    const onToggleFavorite = vi.fn();
    render(
      <NowNextRow
        channel={channel()}
        nowPlaying={program("now", -10, 50, "Live show")}
        upNext={null}
        isFavorite={false}
        onClick={onClick}
        onToggleFavorite={onToggleFavorite}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /ESPN/ }));
    expect(onClick).toHaveBeenCalledTimes(1);
    expect(onToggleFavorite).not.toHaveBeenCalled();
  });

  it("clicking the heart toggles favorite without opening the channel", () => {
    const onClick = vi.fn();
    const onToggleFavorite = vi.fn();
    render(
      <NowNextRow
        channel={channel()}
        nowPlaying={program("now", -10, 50, "Live show")}
        upNext={null}
        isFavorite={false}
        onClick={onClick}
        onToggleFavorite={onToggleFavorite}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /favoritos/i }));
    expect(onToggleFavorite).toHaveBeenCalledTimes(1);
    expect(onClick).not.toHaveBeenCalled();
  });
});
