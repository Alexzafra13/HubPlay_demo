import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { EPGGrid } from "./EPGGrid";
import type { Channel, EPGProgram } from "@/api/types";

// ─── Fixtures ───────────────────────────────────────────────────────────────
//
// Anchor the clock at 12:00 local so the now-line falls in the middle of
// the 24 h window (0..24 h from midnight local). With PX_PER_HOUR = 160
// the now-line sits around 12 * 160 = 1920 px from the timeline origin.

const NOW = new Date("2026-04-24T12:00:00").getTime();

function channel(
  id: string,
  overrides: Partial<Channel> = {},
): Channel {
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

/**
 * startOffsetMin is relative to NOW; durationMin is the programme length
 * in minutes. Returns an EPGProgram positioned accordingly.
 */
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

describe("EPGGrid", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders 24 hour columns in the ruler", () => {
    const { container } = render(
      <EPGGrid
        channels={[channel("c1")]}
        scheduleByChannel={{}}
        onSelectChannel={vi.fn()}
        autoScrollToNow={false}
      />,
    );
    const hourCells = container.querySelectorAll(
      '[role="columnheader"]',
    );
    // 1 corner ("Canal") + 24 hour labels.
    expect(hourCells.length).toBe(25);
  });

  it("renders a row per channel with its name visible", () => {
    const { container } = render(
      <EPGGrid
        channels={[channel("a"), channel("b"), channel("c")]}
        scheduleByChannel={{}}
        onSelectChannel={vi.fn()}
        autoScrollToNow={false}
      />,
    );
    // Channel names appear twice per row (logo aria-label + visible div);
    // a count is the stable assertion.
    expect(screen.getAllByText("A").length).toBeGreaterThan(0);
    expect(screen.getAllByText("B").length).toBeGreaterThan(0);
    expect(screen.getAllByText("C").length).toBeGreaterThan(0);
    // The sticky-cell button is rendered with role="gridcell"; one per
    // channel row (3 rows → 3 gridcells with aria-pressed).
    expect(
      container.querySelectorAll("button[aria-pressed]"),
    ).toHaveLength(3);
  });

  it("shows the empty state when channels is empty", () => {
    render(
      <EPGGrid
        channels={[]}
        scheduleByChannel={{}}
        onSelectChannel={vi.fn()}
        autoScrollToNow={false}
      />,
    );
    expect(
      screen.getByText(/No hay canales disponibles/i),
    ).toBeInTheDocument();
  });

  it("programmes inside the window render, ones outside do not", () => {
    // windowStart = today 00:00 local, windowEnd = tomorrow 00:00 local.
    // At NOW = 12:00, a programme 13 h in the past started at -1:00 yesterday
    // (end at 00:00 today) — end <= windowStart → skipped.
    // A programme 20 h in the future also sits inside the 24h window.
    const visible = program("p-in", 0, 30); // now..now+30min
    const past = program("p-past", -13 * 60, 30); // yesterday 23:00..23:30
    const future = program("p-future", 20 * 60, 30); // tomorrow 08:00..08:30

    render(
      <EPGGrid
        channels={[channel("c1")]}
        scheduleByChannel={{ c1: [visible, past, future] }}
        onSelectChannel={vi.fn()}
        autoScrollToNow={false}
      />,
    );

    expect(screen.getByText("Program p-in")).toBeInTheDocument();
    expect(screen.queryByText("Program p-past")).toBeNull();
    // "p-future" at 08:00 tomorrow falls OUTSIDE windowEnd (24 h from
    // today's midnight = tomorrow 00:00), so it should not render either.
    expect(screen.queryByText("Program p-future")).toBeNull();
  });

  it("renders the 'no guide' placeholder on a row with zero programmes", () => {
    render(
      <EPGGrid
        channels={[channel("c1")]}
        scheduleByChannel={{ c1: [] }}
        onSelectChannel={vi.fn()}
        autoScrollToNow={false}
      />,
    );
    expect(screen.getByText(/Sin guía disponible/i)).toBeInTheDocument();
  });

  it("clicking a programme cell calls onSelectChannel with its channel", () => {
    const onSelect = vi.fn();
    const ch = channel("c1");
    const live = program("p1", -5, 60, { title: "Telediario" });

    render(
      <EPGGrid
        channels={[ch]}
        scheduleByChannel={{ c1: [live] }}
        onSelectChannel={onSelect}
        autoScrollToNow={false}
      />,
    );
    fireEvent.click(
      screen.getByRole("button", {
        name: /Telediario en C1/,
      }),
    );
    expect(onSelect).toHaveBeenCalledWith(ch);
  });

  it("clicking the sticky channel cell also calls onSelectChannel", () => {
    const onSelect = vi.fn();
    const ch = channel("c1");
    const { container } = render(
      <EPGGrid
        channels={[ch]}
        scheduleByChannel={{}}
        onSelectChannel={onSelect}
        autoScrollToNow={false}
      />,
    );
    // The sticky channel cell is a <button> (with role=gridcell overriding
    // the implicit button role). It is the one that carries aria-pressed.
    const chButton = container.querySelector<HTMLButtonElement>(
      "button[aria-pressed]",
    );
    expect(chButton).not.toBeNull();
    fireEvent.click(chButton!);
    expect(onSelect).toHaveBeenCalledWith(ch);
  });

  it("activeChannelId sets aria-pressed=true on that row's sticky cell", () => {
    const { container } = render(
      <EPGGrid
        channels={[channel("a"), channel("b")]}
        scheduleByChannel={{}}
        onSelectChannel={vi.fn()}
        activeChannelId="b"
        autoScrollToNow={false}
      />,
    );
    const pressed = container.querySelectorAll(
      'button[aria-pressed="true"]',
    );
    expect(pressed).toHaveLength(1);
    expect(pressed[0]).toHaveTextContent("B");
  });

  it("renders the 'Ahora · HH:MM' button with the current local time", () => {
    render(
      <EPGGrid
        channels={[]}
        scheduleByChannel={{}}
        onSelectChannel={vi.fn()}
        autoScrollToNow={false}
      />,
    );
    // NOW is 12:00 local by construction.
    expect(screen.getByRole("button", { name: /Ahora · 12:00/ })).toBeInTheDocument();
  });

  it("auto-scrolls to ~now on first render and does NOT re-scroll on re-tick", () => {
    const scrollToSpy = vi.fn();
    // Stub HTMLDivElement.scrollTo before render so the ref picks it up.
    const original = HTMLElement.prototype.scrollTo;
    HTMLElement.prototype.scrollTo = scrollToSpy as unknown as typeof original;

    try {
      render(
        <EPGGrid
          channels={[channel("c1")]}
          scheduleByChannel={{}}
          onSelectChannel={vi.fn()}
          autoScrollToNow
        />,
      );

      expect(scrollToSpy).toHaveBeenCalledTimes(1);
      const [firstCall] = scrollToSpy.mock.calls;
      expect(firstCall[0]).toMatchObject({ behavior: "auto" });
      expect(firstCall[0].left).toBeGreaterThan(0);

      // Advance 30 s — useNowTick fires, now changes, the auto-scroll effect
      // re-runs but hasScrolledRef gates it. scrollTo must not be called again.
      vi.advanceTimersByTime(30_000);
      expect(scrollToSpy).toHaveBeenCalledTimes(1);
    } finally {
      HTMLElement.prototype.scrollTo = original;
    }
  });

  it("clicking 'Ahora' button smooth-scrolls to now", () => {
    const scrollToSpy = vi.fn();
    const original = HTMLElement.prototype.scrollTo;
    HTMLElement.prototype.scrollTo = scrollToSpy as unknown as typeof original;
    try {
      render(
        <EPGGrid
          channels={[channel("c1")]}
          scheduleByChannel={{}}
          onSelectChannel={vi.fn()}
          autoScrollToNow={false}
        />,
      );
      fireEvent.click(screen.getByRole("button", { name: /Ahora · 12:00/ }));
      expect(scrollToSpy).toHaveBeenCalledTimes(1);
      expect(scrollToSpy.mock.calls[0][0]).toMatchObject({
        behavior: "smooth",
      });
      expect(scrollToSpy.mock.calls[0][0].left).toBeGreaterThan(0);
    } finally {
      HTMLElement.prototype.scrollTo = original;
    }
  });

  it("autoScrollToNow=false: does not invoke scrollTo on first render", () => {
    const scrollToSpy = vi.fn();
    const original = HTMLElement.prototype.scrollTo;
    HTMLElement.prototype.scrollTo = scrollToSpy as unknown as typeof original;
    try {
      render(
        <EPGGrid
          channels={[channel("c1")]}
          scheduleByChannel={{}}
          onSelectChannel={vi.fn()}
          autoScrollToNow={false}
        />,
      );
      expect(scrollToSpy).not.toHaveBeenCalled();
    } finally {
      HTMLElement.prototype.scrollTo = original;
    }
  });
});
