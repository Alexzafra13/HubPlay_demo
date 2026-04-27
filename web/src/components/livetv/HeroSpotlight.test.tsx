import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { HeroSpotlight, type HeroSpotlightItem } from "./HeroSpotlight";
import type { Channel, EPGProgram } from "@/api/types";

// StreamPreview mounts hls.js, which jsdom can't run. Stub it to a
// recognisable div so we can assert the lazy-mount behaviour.
vi.mock("./StreamPreview", () => ({
  StreamPreview: (props: { streamUrl: string }) => (
    <div data-testid="preview" data-url={props.streamUrl} />
  ),
}));

const NOW = new Date("2026-04-24T12:00:00Z").getTime();

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
  // The hero used to be a carousel with auto-rotate + autoplay HLS
  // preview. We collapsed it to a single editorial card (one channel,
  // no rotation, no video loop) — inmediacy moved to a dedicated
  // "Ahora en directo" rail instead. These tests pin the new contract.
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
    expect(container.querySelector("section")).toBeNull();
  });

  it("renders the first item only — no carousel dots, no rotation timer", () => {
    render(
      <HeroSpotlight
        items={[item("a"), item("b"), item("c")]}
        label="Tu favorito"
      />,
    );

    // Label pill renders.
    expect(screen.getByText("Tu favorito")).toBeInTheDocument();
    // The hero tile carries the first channel's name as its aria-label
    // suffix (or full label when no EPG). The presence of "A" confirms
    // we rendered items[0], not [1] or [2].
    expect(
      screen.getByRole("button", { name: /A — Live p-a/ }),
    ).toBeInTheDocument();
    // No carousel UI.
    expect(screen.queryByRole("tablist")).toBeNull();
    expect(screen.queryAllByRole("tab")).toHaveLength(0);
  });

  it("does not auto-advance to a different channel after time passes", () => {
    render(
      <HeroSpotlight items={[item("a"), item("b")]} label="Tu favorito" />,
    );
    expect(
      screen.getByRole("button", { name: /A — Live p-a/ }),
    ).toBeInTheDocument();

    // Far past the previous 12 s rotate threshold — still the same
    // channel. The hero is editorial, not a carousel.
    vi.advanceTimersByTime(60_000);

    expect(
      screen.getByRole("button", { name: /A — Live p-a/ }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /B/ })).toBeNull();
  });

  it("clicking the hero tile calls onOpen with the rendered channel", () => {
    const onOpen = vi.fn();
    const a = channel("a");
    render(
      <HeroSpotlight
        items={[{ channel: a, nowPlaying: null }]}
        label="Live"
        onOpen={onOpen}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "A" }));
    expect(onOpen).toHaveBeenCalledWith(a);
  });

  it("lazy-mounts the live preview after the mount delay", () => {
    render(<HeroSpotlight items={[item("a")]} label="Live" />);
    // Right after mount the preview is intentionally absent — we
    // stagger it so a fresh page load doesn't fire an HLS request in
    // the same tick as the shell paint.
    expect(screen.queryByTestId("preview")).toBeNull();
    act(() => {
      vi.advanceTimersByTime(1_000);
    });
    expect(screen.getByTestId("preview")).toHaveAttribute(
      "data-url",
      "http://stream/a",
    );
  });

  it("does NOT mount the live preview when prefers-reduced-motion is set", () => {
    // Override matchMedia to report reduced-motion = true. The hero
    // should respect the OS preference and skip the autoplay HLS,
    // leaning on the gradient backdrop alone.
    const originalMM = window.matchMedia;
    window.matchMedia = vi.fn().mockImplementation((query: string) => ({
      matches: query.includes("prefers-reduced-motion"),
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })) as unknown as typeof window.matchMedia;

    try {
      render(<HeroSpotlight items={[item("a")]} label="Live" />);
      act(() => {
        vi.advanceTimersByTime(2_000);
      });
      expect(screen.queryByTestId("preview")).toBeNull();
    } finally {
      window.matchMedia = originalMM;
    }
  });

  it("renders the programme title (without an 'Ahora' prefix) only when nowPlaying is present", () => {
    const { rerender } = render(
      <HeroSpotlight
        items={[{ channel: channel("a"), nowPlaying: null }]}
        label="Recién añadidos"
      />,
    );
    expect(screen.queryByText("Live")).toBeNull();
    expect(screen.queryByText(/Ahora/)).toBeNull();
    expect(screen.queryByText(/Live p-a/)).toBeNull();

    rerender(<HeroSpotlight items={[item("a")]} label="Recién añadidos" />);
    expect(screen.getByText("Live p-a")).toBeInTheDocument();
    // No leftover "Live" pill or "Ahora" prefix.
    expect(screen.queryByText("Live")).toBeNull();
    expect(screen.queryByText(/Ahora/)).toBeNull();
  });

  it("renders the headerOverlay node when supplied", () => {
    render(
      <HeroSpotlight
        items={[item("a")]}
        label="Live"
        headerOverlay={<h1 data-testid="overlay">TV en directo</h1>}
      />,
    );
    expect(screen.getByTestId("overlay")).toBeInTheDocument();
  });
});
