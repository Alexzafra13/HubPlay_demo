import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Channel, EPGProgram } from "@/api/types";
import { NowPlayingCard } from "./NowPlayingCard";

const NOW = new Date("2026-04-24T12:00:00Z").getTime();

function channel(overrides: Partial<Channel> = {}): Channel {
  return {
    id: "c1",
    library_id: "lib1",
    name: "La 1 HD",
    number: 1,
    group: null,
    group_name: null,
    category: "general",
    logo_initials: "L1",
    logo_bg: "#111",
    logo_fg: "#fff",
    logo_url: null,
    stream_url: "http://stream/c1",
    language: "",
    country: "",
    is_active: true,
    ...overrides,
  };
}

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
    channel_id: "c1",
    title: `Program ${id}`,
    description: "",
    category: "",
    icon_url: "",
    start_time: start.toISOString(),
    end_time: end.toISOString(),
    ...overrides,
  };
}

describe("NowPlayingCard", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("shows the no-EPG fallback when nowPlaying is null", () => {
    render(
      <NowPlayingCard
        channel={channel()}
        nowPlaying={null}
        upNext={null}
        now={NOW}
      />,
    );
    expect(screen.getByText("Ahora en antena")).toBeInTheDocument();
    expect(
      screen.getByText(/Sin guía disponible — La 1 HD/),
    ).toBeInTheDocument();
  });

  it("renders title, description, duration and category", () => {
    const live = program("live", -15, 60, {
      title: "Telediario",
      description: "Las noticias del día.",
      category: "News",
    });
    render(
      <NowPlayingCard
        channel={channel()}
        nowPlaying={live}
        upNext={null}
        now={NOW}
      />,
    );
    expect(
      screen.getByRole("heading", { name: "Telediario" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Las noticias del día.")).toBeInTheDocument();
    expect(screen.getByText(/60 min/)).toBeInTheDocument();
    expect(screen.getByText("News")).toBeInTheDocument();
  });

  it("omits category block when the program has no category", () => {
    const live = program("live", -15, 60, { title: "Cine" });
    render(
      <NowPlayingCard
        channel={channel()}
        nowPlaying={live}
        upNext={null}
        now={NOW}
      />,
    );
    expect(screen.getByRole("heading", { name: "Cine" })).toBeInTheDocument();
    // Category text absent.
    expect(screen.queryByText(/News/)).toBeNull();
  });

  it("renders the up-next hint when provided", () => {
    const live = program("live", -15, 60);
    const next = program("next", 45, 30, { title: "Deportes" });
    render(
      <NowPlayingCard
        channel={channel()}
        nowPlaying={live}
        upNext={next}
        now={NOW}
      />,
    );
    expect(screen.getByText("Después")).toBeInTheDocument();
    expect(screen.getByText("Deportes")).toBeInTheDocument();
  });

  it("omits the up-next hint when null", () => {
    const live = program("live", -15, 60);
    render(
      <NowPlayingCard
        channel={channel()}
        nowPlaying={live}
        upNext={null}
        now={NOW}
      />,
    );
    expect(screen.queryByText("Después")).toBeNull();
  });

  /**
   * Progress is computed from `now` relative to the program window. Halfway
   * through a 60-minute program, the width of the progress bar element
   * should be 50%. We read the inline style since the DOM reflects it.
   */
  it("drives the progress bar from the `now` prop", () => {
    const live = program("live", -30, 60); // started 30 min ago, 60 min long
    const { container } = render(
      <NowPlayingCard
        channel={channel()}
        nowPlaying={live}
        upNext={null}
        now={NOW}
      />,
    );
    // The progress bar fill is the div with the tv-accent class and
    // an inline width style. We assert the width is around 50%.
    const fills = container.querySelectorAll<HTMLDivElement>(
      "[style*='width']",
    );
    const progressFill = Array.from(fills).find((el) =>
      el.style.width.endsWith("%"),
    );
    expect(progressFill).toBeDefined();
    expect(progressFill?.style.width).toBe("50%");
  });

  it("clamps progress to 0 before the window and 100 after", () => {
    // After the window — +2 h past program end.
    const past = program("past", -120, 30);
    const { container, rerender } = render(
      <NowPlayingCard
        channel={channel()}
        nowPlaying={past}
        upNext={null}
        now={NOW}
      />,
    );
    let fill = container.querySelector<HTMLDivElement>("[style*='width']");
    expect(fill?.style.width).toBe("100%");

    // Before the window — program hasn't started yet.
    const future = program("future", 60, 30);
    rerender(
      <NowPlayingCard
        channel={channel()}
        nowPlaying={future}
        upNext={null}
        now={NOW}
      />,
    );
    fill = container.querySelector<HTMLDivElement>("[style*='width']");
    expect(fill?.style.width).toBe("0%");
  });
});
