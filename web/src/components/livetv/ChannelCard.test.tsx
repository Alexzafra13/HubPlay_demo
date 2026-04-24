import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import type { Channel, EPGProgram } from "@/api/types";
import { ChannelCard } from "./ChannelCard";

// NOTE: we use `fireEvent` instead of `user-event`. The component debounces
// hover via window timers; with vi.useFakeTimers, user-event's own queue
// never drains and every interaction times out. fireEvent is synchronous
// and exercises the same DOM event handlers.
//
// NOTE: the <img> inside the card is decorative (alt=""), so it's excluded
// from the accessibility tree and `getByRole("img")` cannot find it. We
// use `container.querySelector("img")` for those queries.

const NOW = new Date("2026-04-24T12:00:00Z").getTime();

function channel(overrides: Partial<Channel> = {}): Channel {
  return {
    id: "c1",
    library_id: "lib1",
    name: "La 1 HD",
    number: 1,
    group: "General",
    group_name: "General",
    category: "general",
    logo_initials: "L1",
    logo_bg: "#111111",
    logo_fg: "#ffffff",
    logo_url: null,
    stream_url: "http://stream/c1",
    language: "",
    country: "ES",
    is_active: true,
    added_at: new Date(NOW).toISOString(),
    ...overrides,
  };
}

function liveNow(): EPGProgram {
  return {
    id: "p1",
    channel_id: "c1",
    title: "Telediario",
    description: "",
    category: "",
    icon_url: "",
    start_time: new Date(NOW - 15 * 60_000).toISOString(),
    end_time: new Date(NOW + 45 * 60_000).toISOString(),
  };
}

describe("ChannelCard", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders channel name and number", () => {
    render(<ChannelCard channel={channel()} />);
    expect(screen.getByText("La 1 HD")).toBeInTheDocument();
    expect(screen.getByText("CH 1")).toBeInTheDocument();
  });

  it("shows 'Ahora' + program title when EPG is available", () => {
    render(<ChannelCard channel={channel()} nowPlaying={liveNow()} />);
    expect(screen.getByText("Ahora")).toBeInTheDocument();
    expect(screen.getByText("Telediario")).toBeInTheDocument();
  });

  it("shows 'Sin guía disponible' when no EPG", () => {
    render(<ChannelCard channel={channel()} />);
    expect(screen.getByText("Sin guía disponible")).toBeInTheDocument();
  });

  it("renders initials avatar when channel.logo_url is missing", () => {
    const { container } = render(
      <ChannelCard channel={channel({ logo_url: null })} />,
    );
    // The decorative <img alt=""> is excluded from the a11y tree, so we
    // query the DOM directly. With no URL, no img should exist at all.
    expect(container.querySelector("img")).toBeNull();
    expect(screen.getByText("L1")).toBeInTheDocument();
  });

  it("renders <img> when a logo_url is provided", () => {
    const { container } = render(
      <ChannelCard
        channel={channel({ logo_url: "https://cdn/logo.png" })}
      />,
    );
    const img = container.querySelector("img");
    expect(img).not.toBeNull();
    expect(img).toHaveAttribute("src", "https://cdn/logo.png");
    // Initials are hidden while the img is healthy.
    expect(screen.queryByText("L1")).toBeNull();
  });

  /**
   * Regression test for the pre-fix behaviour where onError only hid the
   * <img> and the <ChannelLogo> fallback lived in the opposite branch of
   * a ternary — broken URLs left an empty gradient. The fix: on error
   * the component should swap in the initials avatar.
   */
  it("falls back to initials avatar when the logo URL fails to load", () => {
    const { container } = render(
      <ChannelCard channel={channel({ logo_url: "https://cdn/broken.png" })} />,
    );
    const img = container.querySelector("img")!;
    expect(img).toBeInTheDocument();

    // Simulate the <img> onError event.
    fireEvent.error(img);

    // After error, the img is gone and the initials avatar renders.
    expect(container.querySelector("img")).toBeNull();
    expect(screen.getByText("L1")).toBeInTheDocument();
  });

  it("fires onClick when the card is clicked", () => {
    const onClick = vi.fn();
    render(<ChannelCard channel={channel()} onClick={onClick} />);
    fireEvent.click(screen.getByRole("button", { name: /La 1 HD/ }));
    expect(onClick).toHaveBeenCalledOnce();
  });

  it("renders the favorite toggle when onToggleFavorite is provided", () => {
    const onToggle = vi.fn();
    render(
      <ChannelCard channel={channel()} isFavorite onToggleFavorite={onToggle} />,
    );
    const fav = screen.getByRole("button", { name: /Quitar de favoritos/ });
    expect(fav).toHaveAttribute("aria-pressed", "true");
  });

  it("does not render the favorite toggle when onToggleFavorite is absent", () => {
    render(<ChannelCard channel={channel()} />);
    expect(
      screen.queryByRole("button", { name: /favoritos/ }),
    ).toBeNull();
  });

  it("fires onToggleFavorite independently of the card click", () => {
    const onClick = vi.fn();
    const onToggle = vi.fn();
    render(
      <ChannelCard
        channel={channel()}
        onClick={onClick}
        onToggleFavorite={onToggle}
      />,
    );
    fireEvent.click(
      screen.getByRole("button", { name: /Añadir a favoritos/ }),
    );
    expect(onToggle).toHaveBeenCalledOnce();
    // Favorite click should not bubble up to the card click.
    expect(onClick).not.toHaveBeenCalled();
  });

  it("renders the 'Apagado' treatment when dimmed", () => {
    render(<ChannelCard channel={channel()} dimmed />);
    expect(screen.getByText("Apagado")).toBeInTheDocument();
    // Dimmed cards advertise themselves via aria-label so screen readers
    // announce the off-air state.
    expect(
      screen.getByRole("button", { name: /La 1 HD — apagado/ }),
    ).toBeInTheDocument();
  });

  it("shows 'Live' pill when EPG says the channel is on air", () => {
    render(<ChannelCard channel={channel()} nowPlaying={liveNow()} />);
    expect(screen.getByText("Live")).toBeInTheDocument();
  });
});
