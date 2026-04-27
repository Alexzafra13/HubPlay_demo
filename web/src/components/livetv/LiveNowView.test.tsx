import { describe, it, expect, vi, beforeAll, afterAll } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { LiveNowView } from "./LiveNowView";
import type { Channel, EPGProgram } from "@/api/types";
import type { CategoryFilter } from "./CategoryChips";

const NOW = new Date("2026-04-26T20:00:00Z").getTime();

beforeAll(() => {
  vi.useFakeTimers();
  vi.setSystemTime(NOW);
});
afterAll(() => {
  vi.useRealTimers();
});

function channel(id: string, overrides: Partial<Channel> = {}): Channel {
  return {
    id,
    library_id: "lib1",
    name: id.toUpperCase(),
    number: 1,
    group: null,
    group_name: null,
    category: "general",
    logo_initials: id.slice(0, 2).toUpperCase(),
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

function liveProg(id: string, title: string): EPGProgram {
  return {
    id,
    channel_id: "c",
    title,
    description: "",
    category: "",
    icon_url: "",
    start_time: new Date(NOW - 15 * 60_000).toISOString(),
    end_time: new Date(NOW + 45 * 60_000).toISOString(),
  };
}

const baseCounts: Record<CategoryFilter, number> = {
  all: 3,
  "no-signal": 0,
  general: 1,
  news: 1,
  sports: 1,
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

function renderView(
  overrides: Partial<Parameters<typeof LiveNowView>[0]> = {},
) {
  const news = channel("news1", {
    category: "news",
    name: "Telediario Channel",
  });
  const sports = channel("sport1", {
    category: "sports",
    name: "Sports Today",
  });
  const general = channel("gen1", { category: "general", name: "Generalist" });

  const props = {
    channels: [news, sports, general],
    scheduleByChannel: {
      [news.id]: [liveProg("p1", "Telediario noche")],
      [sports.id]: [liveProg("p2", "Champions live")],
      [general.id]: [liveProg("p3", "Concurso variedades")],
    },
    category: "all" as CategoryFilter,
    onCategoryChange: vi.fn(),
    counts: baseCounts,
    search: "",
    sort: "favorites" as const,
    onSortChange: vi.fn(),
    onOpen: vi.fn(),
    favoriteSet: new Set<string>(),
    onToggleFavorite: vi.fn(),
    ...overrides,
  };
  return { props, ...render(<LiveNowView {...props} />) };
}

describe("LiveNowView", () => {
  it("shows every supplied channel when category is 'all' and search is empty", () => {
    renderView();
    expect(screen.getByText("Telediario Channel")).toBeInTheDocument();
    expect(screen.getByText("Sports Today")).toBeInTheDocument();
    expect(screen.getByText("Generalist")).toBeInTheDocument();
  });

  it("filters by category", () => {
    renderView({ category: "news" });
    expect(screen.getByText("Telediario Channel")).toBeInTheDocument();
    expect(screen.queryByText("Sports Today")).toBeNull();
    expect(screen.queryByText("Generalist")).toBeNull();
  });

  it("filters by search across channel name and current programme title", () => {
    // 'champions' lives in the sports channel's programme title; the
    // channel name doesn't contain it. The search must reach into EPG
    // for this case to work — that's the whole point of the surface.
    renderView({ search: "champions" });
    expect(screen.getByText("Sports Today")).toBeInTheDocument();
    expect(screen.queryByText("Telediario Channel")).toBeNull();
  });

  it("renders an empty state when no channels survive the search filter", () => {
    renderView({ category: "news", search: "xyz-no-match" });
    expect(
      screen.getByText(/Ningún canal en directo coincide con la búsqueda/),
    ).toBeInTheDocument();
  });

  it("renders an EPG-aware empty state when there are no live channels at all", () => {
    // The empty pool case is the most informative one — likely cause
    // is "EPG hasn't loaded yet". The hint points the user at where
    // to verify the source.
    renderView({ channels: [] });
    expect(
      screen.getByText(/No hay canales emitiendo ahora mismo/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/su guía EPG aún se está descargando/),
    ).toBeInTheDocument();
  });

  it("sort='ending' orders by current programme end_time ascending", () => {
    // Three channels whose current programme ends at different times.
    // 'ending' should put the soonest-to-end first.
    const earlyEnd = channel("e1", { name: "Ends First" });
    const midEnd = channel("e2", { name: "Ends Mid" });
    const lateEnd = channel("e3", { name: "Ends Last" });

    const prog = (id: string, endOffsetMin: number): EPGProgram => ({
      id,
      channel_id: "c",
      title: id,
      description: "",
      category: "",
      icon_url: "",
      start_time: new Date(NOW - 30 * 60_000).toISOString(),
      end_time: new Date(NOW + endOffsetMin * 60_000).toISOString(),
    });

    renderView({
      channels: [lateEnd, midEnd, earlyEnd],
      scheduleByChannel: {
        [earlyEnd.id]: [prog("p1", 5)],
        [midEnd.id]: [prog("p2", 30)],
        [lateEnd.id]: [prog("p3", 90)],
      },
      sort: "ending",
    });

    const titles = screen
      .getAllByText(/Ends (First|Mid|Last)/)
      .map((el) => el.textContent);
    expect(titles).toEqual(["Ends First", "Ends Mid", "Ends Last"]);
  });

  it("sort='name' orders alphabetically (case-insensitive)", () => {
    renderView({ sort: "name" });
    const names = screen
      .getAllByText(/^(Telediario Channel|Sports Today|Generalist)$/)
      .map((el) => el.textContent);
    expect(names).toEqual(["Generalist", "Sports Today", "Telediario Channel"]);
  });

  it("clicking a card calls onOpen with the channel", () => {
    const onOpen = vi.fn();
    renderView({ onOpen });
    fireEvent.click(screen.getByText("Sports Today"));
    expect(onOpen).toHaveBeenCalled();
    const arg = onOpen.mock.calls[0][0];
    expect(arg.name).toBe("Sports Today");
  });
});
