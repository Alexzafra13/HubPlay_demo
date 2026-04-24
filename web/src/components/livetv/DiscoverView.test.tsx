import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { DiscoverView } from "./DiscoverView";
import type {
  Channel,
  ChannelCategory,
  UnhealthyChannel,
} from "@/api/types";
import type { CategoryFilter } from "./CategoryChips";

// HeroSpotlight has its own test + uses HLS via StreamPreview. Skip its
// internals here — we only want to verify DiscoverView's rail layout and
// category filtering, so a passthrough harness is enough.
vi.mock("./HeroSpotlight", () => ({
  HeroSpotlight: (props: { items: unknown[]; label: string }) => (
    <div data-testid="hero" data-count={props.items.length}>
      hero:{props.label}
    </div>
  ),
}));

// ChannelCard has hover timers / HLS preview that would drag the test into
// its implementation. Render a minimal button that announces the channel
// id and exposes `dimmed` so we can assert the "Apagados" rail styling.
vi.mock("./ChannelCard", () => ({
  ChannelCard: (props: {
    channel: { id: string; name: string };
    dimmed?: boolean;
    onClick: () => void;
    onToggleFavorite: () => void;
  }) => (
    <button
      type="button"
      data-testid="card"
      data-channel={props.channel.id}
      data-dimmed={props.dimmed ? "1" : "0"}
      onClick={props.onClick}
    >
      {props.channel.name}
    </button>
  ),
}));

// CategoryChips — render chips as buttons so we can click them from tests.
vi.mock("./CategoryChips", () => ({
  CategoryChips: (props: {
    counts: Record<string, number>;
    active: string;
    onChange: (c: string) => void;
  }) => (
    <div data-testid="chips">
      {Object.entries(props.counts).map(([cat, n]) => (
        <button
          key={cat}
          type="button"
          data-active={props.active === cat ? "1" : "0"}
          onClick={() => props.onChange(cat)}
        >
          chip-{cat}:{n}
        </button>
      ))}
    </div>
  ),
}));

const NOW = new Date("2026-04-24T12:00:00Z").getTime();

function channel(
  id: string,
  cat: ChannelCategory,
  overrides: Partial<Channel> = {},
): Channel {
  return {
    id,
    library_id: "lib1",
    name: id.toUpperCase(),
    number: 1,
    group: null,
    group_name: null,
    category: cat,
    logo_initials: "X",
    logo_bg: "#111",
    logo_fg: "#fff",
    logo_url: null,
    stream_url: `http://stream/${id}`,
    language: "",
    country: "",
    is_active: true,
    ...overrides,
  };
}

function unhealthy(id: string): UnhealthyChannel {
  return {
    ...channel(id, "general"),
    last_probe_at: null,
    last_probe_status: "error",
    last_probe_error: "timeout",
    consecutive_failures: 3,
  };
}

function makeCounts(): Record<CategoryFilter, number> {
  return {
    all: 0,
    general: 0,
    news: 0,
    sports: 0,
    movies: 0,
    music: 0,
    entertainment: 0,
    kids: 0,
    culture: 0,
    documentaries: 0,
    international: 0,
    travel: 0,
    religion: 0,
    adult: 0,
  };
}

describe("DiscoverView", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("with category='all' renders rails in CHANNEL_CATEGORY_ORDER, skipping empty ones", () => {
    const byCat = new Map<ChannelCategory, Channel[]>([
      ["news", [channel("n1", "news")]],
      ["movies", [channel("m1", "movies")]],
      // "sports" intentionally missing — should not render a rail.
    ]);

    render(
      <DiscoverView
        heroItems={[]}
        heroLabel="Hero"
        counts={makeCounts()}
        category="all"
        onCategoryChange={vi.fn()}
        channelsByCategory={byCat}
        scheduleByChannel={{}}
        onOpen={vi.fn()}
        favoriteSet={new Set()}
        onToggleFavorite={vi.fn()}
        unhealthyChannels={[]}
      />,
    );

    const cards = screen.getAllByTestId("card");
    expect(cards.map((c) => c.getAttribute("data-channel"))).toEqual([
      "n1",
      "m1",
    ]);
  });

  it("with a specific category only renders that rail", () => {
    const byCat = new Map<ChannelCategory, Channel[]>([
      ["news", [channel("n1", "news")]],
      ["movies", [channel("m1", "movies"), channel("m2", "movies")]],
      ["sports", [channel("s1", "sports")]],
    ]);

    render(
      <DiscoverView
        heroItems={[]}
        heroLabel="Hero"
        counts={makeCounts()}
        category="movies"
        onCategoryChange={vi.fn()}
        channelsByCategory={byCat}
        scheduleByChannel={{}}
        onOpen={vi.fn()}
        favoriteSet={new Set()}
        onToggleFavorite={vi.fn()}
        unhealthyChannels={[]}
      />,
    );

    const cards = screen.getAllByTestId("card");
    expect(cards).toHaveLength(2);
    expect(cards.map((c) => c.getAttribute("data-channel")).sort()).toEqual([
      "m1",
      "m2",
    ]);
  });

  it("Apagados rail appears only when there are unhealthy channels AND category='all'", () => {
    const byCat = new Map<ChannelCategory, Channel[]>([
      ["news", [channel("n1", "news")]],
    ]);
    const unhealthyList = [unhealthy("dead1"), unhealthy("dead2")];

    const { rerender } = render(
      <DiscoverView
        heroItems={[]}
        heroLabel="Hero"
        counts={makeCounts()}
        category="all"
        onCategoryChange={vi.fn()}
        channelsByCategory={byCat}
        scheduleByChannel={{}}
        onOpen={vi.fn()}
        favoriteSet={new Set()}
        onToggleFavorite={vi.fn()}
        unhealthyChannels={unhealthyList}
      />,
    );
    const dimmed = screen
      .getAllByTestId("card")
      .filter((el) => el.getAttribute("data-dimmed") === "1");
    expect(dimmed).toHaveLength(2);

    // Switch to a specific category — Apagados rail must disappear even with
    // unhealthy channels still provided.
    rerender(
      <DiscoverView
        heroItems={[]}
        heroLabel="Hero"
        counts={makeCounts()}
        category="sports"
        onCategoryChange={vi.fn()}
        channelsByCategory={byCat}
        scheduleByChannel={{}}
        onOpen={vi.fn()}
        favoriteSet={new Set()}
        onToggleFavorite={vi.fn()}
        unhealthyChannels={unhealthyList}
      />,
    );
    const dimmedAfter = screen
      .queryAllByTestId("card")
      .filter((el) => el.getAttribute("data-dimmed") === "1");
    expect(dimmedAfter).toHaveLength(0);
  });

  it("Apagados rail is absent when there are no unhealthy channels, even in 'all'", () => {
    render(
      <DiscoverView
        heroItems={[]}
        heroLabel="Hero"
        counts={makeCounts()}
        category="all"
        onCategoryChange={vi.fn()}
        channelsByCategory={
          new Map([["news", [channel("n1", "news")]]]) as Map<
            ChannelCategory,
            Channel[]
          >
        }
        scheduleByChannel={{}}
        onOpen={vi.fn()}
        favoriteSet={new Set()}
        onToggleFavorite={vi.fn()}
        unhealthyChannels={[]}
      />,
    );
    const dimmed = screen
      .queryAllByTestId("card")
      .filter((el) => el.getAttribute("data-dimmed") === "1");
    expect(dimmed).toHaveLength(0);
  });

  it("renders the empty state when no category has channels", () => {
    render(
      <DiscoverView
        heroItems={[]}
        heroLabel="Hero"
        counts={makeCounts()}
        category="all"
        onCategoryChange={vi.fn()}
        channelsByCategory={new Map()}
        scheduleByChannel={{}}
        onOpen={vi.fn()}
        favoriteSet={new Set()}
        onToggleFavorite={vi.fn()}
        unhealthyChannels={[]}
      />,
    );
    expect(screen.getByText(/No hay canales en esta categoría/i)).toBeInTheDocument();
    // No cards, no Apagados rail either.
    expect(screen.queryAllByTestId("card")).toHaveLength(0);
  });

  it("clicking a card forwards the channel to onOpen", () => {
    const onOpen = vi.fn();
    const ch = channel("n1", "news");
    render(
      <DiscoverView
        heroItems={[]}
        heroLabel="Hero"
        counts={makeCounts()}
        category="all"
        onCategoryChange={vi.fn()}
        channelsByCategory={new Map([["news", [ch]]])}
        scheduleByChannel={{}}
        onOpen={onOpen}
        favoriteSet={new Set()}
        onToggleFavorite={vi.fn()}
        unhealthyChannels={[]}
      />,
    );
    fireEvent.click(screen.getByTestId("card"));
    expect(onOpen).toHaveBeenCalledWith(ch);
  });

  it("forwards heroItems + heroLabel to the HeroSpotlight", () => {
    render(
      <DiscoverView
        heroItems={[
          { channel: channel("a", "news"), nowPlaying: null },
          { channel: channel("b", "news"), nowPlaying: null },
        ]}
        heroLabel="Tu favorito"
        counts={makeCounts()}
        category="all"
        onCategoryChange={vi.fn()}
        channelsByCategory={new Map()}
        scheduleByChannel={{}}
        onOpen={vi.fn()}
        favoriteSet={new Set()}
        onToggleFavorite={vi.fn()}
        unhealthyChannels={[]}
      />,
    );
    const hero = screen.getByTestId("hero");
    expect(hero).toHaveAttribute("data-count", "2");
    expect(hero).toHaveTextContent("Tu favorito");
  });
});
