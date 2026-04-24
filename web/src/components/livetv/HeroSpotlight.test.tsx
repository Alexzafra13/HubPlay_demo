import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { HeroSpotlight, type HeroSpotlightItem } from "./HeroSpotlight";
import type { Channel, EPGProgram } from "@/api/types";

// StreamPreview mounts hls.js, which blows up in jsdom. Replace with a stub
// that simply records the stream URL it received so we can assert the
// `key={channel.id}` remount across slide changes.
vi.mock("./StreamPreview", () => ({
  StreamPreview: (props: { streamUrl: string }) => (
    <div data-testid="preview" data-url={props.streamUrl} />
  ),
}));

const NOW = new Date("2026-04-24T12:00:00Z").getTime();

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

function liveProg(id: string): EPGProgram {
  return {
    id,
    channel_id: "c",
    title: `Live ${id}`,
    description: "",
    category: "",
    icon_url: "",
    start_time: new Date(NOW - 15 * 60_000).toISOString(),
    end_time: new Date(NOW + 45 * 60_000).toISOString(),
  };
}

function item(id: string, withEpg = true): HeroSpotlightItem {
  return {
    channel: channel(id),
    nowPlaying: withEpg ? liveProg(`p-${id}`) : null,
  };
}

describe("HeroSpotlight", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders null when items is empty", () => {
    const { container } = render(
      <HeroSpotlight items={[]} label="Empty" />,
    );
    // The <section> is what HeroSpotlight renders — an empty tree means
    // the component returned null.
    expect(container.querySelector("section")).toBeNull();
  });

  it("with a single item: no dots, no crash, and the label shows", () => {
    render(<HeroSpotlight items={[item("a")]} label="Tu favorito" />);

    // Label pill is rendered.
    expect(screen.getByText("Tu favorito")).toBeInTheDocument();
    // Dots container only appears when items.length > 1.
    expect(screen.queryByRole("tablist")).toBeNull();
  });

  it("with 2+ items: renders dots and marks the first aria-selected on mount", () => {
    render(
      <HeroSpotlight
        items={[item("a"), item("b"), item("c")]}
        label="Live"
      />,
    );
    const dots = screen.getAllByRole("tab");
    expect(dots).toHaveLength(3);
    expect(dots[0]).toHaveAttribute("aria-selected", "true");
    expect(dots[1]).toHaveAttribute("aria-selected", "false");
    expect(dots[2]).toHaveAttribute("aria-selected", "false");
  });

  it("clicking a dot switches the active slide", () => {
    render(
      <HeroSpotlight
        items={[item("a"), item("b"), item("c")]}
        label="Live"
      />,
    );
    fireEvent.click(screen.getAllByRole("tab")[2]);

    const dots = screen.getAllByRole("tab");
    expect(dots[2]).toHaveAttribute("aria-selected", "true");
    expect(dots[0]).toHaveAttribute("aria-selected", "false");
  });

  it("auto-rotates every 12 s and wraps around", () => {
    render(
      <HeroSpotlight items={[item("a"), item("b")]} label="Live" />,
    );

    expect(screen.getAllByRole("tab")[0]).toHaveAttribute(
      "aria-selected",
      "true",
    );

    act(() => {
      vi.advanceTimersByTime(12_000);
    });
    expect(screen.getAllByRole("tab")[1]).toHaveAttribute(
      "aria-selected",
      "true",
    );

    act(() => {
      vi.advanceTimersByTime(12_000);
    });
    expect(screen.getAllByRole("tab")[0]).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });

  it("does NOT auto-rotate with a single item (no timer to advance)", () => {
    render(<HeroSpotlight items={[item("a")]} label="Live" />);

    // After 30 s nothing would change because there are no siblings and
    // no timer was installed. We assert by looking for a dot: still none.
    act(() => {
      vi.advanceTimersByTime(30_000);
    });
    expect(screen.queryAllByRole("tab")).toHaveLength(0);
  });

  it("clicking the hero tile calls onOpen with the currently-displayed channel", () => {
    const onOpen = vi.fn();
    const a = channel("a");
    const b = channel("b");
    render(
      <HeroSpotlight
        items={[
          { channel: a, nowPlaying: null },
          { channel: b, nowPlaying: null },
        ]}
        label="Live"
        onOpen={onOpen}
      />,
    );

    // The hero tile is the first button — click it.
    const hero = screen
      .getAllByRole("button")
      .find((el) => el.getAttribute("aria-label") === "A");
    expect(hero).toBeDefined();
    fireEvent.click(hero!);
    expect(onOpen).toHaveBeenCalledWith(a);

    // Rotate to slide b and click: onOpen reflects the new channel.
    act(() => {
      vi.advanceTimersByTime(12_000);
    });
    const heroB = screen
      .getAllByRole("button")
      .find((el) => el.getAttribute("aria-label") === "B");
    fireEvent.click(heroB!);
    expect(onOpen).toHaveBeenLastCalledWith(b);
  });

  it("StreamPreview key={channel.id} remounts across slides (no stale HLS)", () => {
    render(
      <HeroSpotlight items={[item("a"), item("b")]} label="Live" />,
    );
    expect(screen.getByTestId("preview")).toHaveAttribute(
      "data-url",
      "http://stream/a",
    );

    act(() => {
      vi.advanceTimersByTime(12_000);
    });
    expect(screen.getByTestId("preview")).toHaveAttribute(
      "data-url",
      "http://stream/b",
    );
  });

  it("clamp: survives items shrinking to 1 after landing on a higher rawIdx", () => {
    const { rerender } = render(
      <HeroSpotlight
        items={[item("a"), item("b"), item("c")]}
        label="Live"
      />,
    );
    // Move to idx=2.
    fireEvent.click(screen.getAllByRole("tab")[2]);
    expect(screen.getAllByRole("tab")[2]).toHaveAttribute(
      "aria-selected",
      "true",
    );

    // Items shrink to 1 — the clamp at render time (idx = rawIdx % length)
    // picks the remaining item without crashing.
    rerender(<HeroSpotlight items={[item("z")]} label="Live" />);
    // Only one item → no dots rendered.
    expect(screen.queryAllByRole("tab")).toHaveLength(0);
    // And the preview reflects the surviving channel.
    expect(screen.getByTestId("preview")).toHaveAttribute(
      "data-url",
      "http://stream/z",
    );
  });

  it("shows the LIVE pill only when nowPlaying is present", () => {
    const { rerender } = render(
      <HeroSpotlight
        items={[{ channel: channel("a"), nowPlaying: null }]}
        label="Recién añadidos"
      />,
    );
    // No EPG → no "Live" pill and no program title.
    expect(screen.queryByText("Live")).toBeNull();
    expect(screen.queryByText(/Ahora/)).toBeNull();

    rerender(
      <HeroSpotlight items={[item("a")]} label="Recién añadidos" />,
    );
    // With EPG → the "Live" pill appears.
    expect(screen.getByText("Live")).toBeInTheDocument();
  });
});
