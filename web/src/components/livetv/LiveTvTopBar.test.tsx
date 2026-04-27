import { describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { LiveTvTopBar } from "./LiveTvTopBar";
import type { HeroModeOption } from "./HeroSettings";

// HeroSettings has its own internal state (dropdown menu, click-outside, icons)
// that would drag the topbar test into its concerns. A minimal harness lets
// us assert its visibility per tab and the props it receives without any of
// that noise.
vi.mock("./HeroSettings", () => ({
  HeroSettings: (props: {
    mode: string;
    modeOptions: { mode: string; label: string }[];
    onModeChange: (m: string) => void;
  }) => (
    <div data-testid="hero-settings" data-mode={props.mode}>
      {props.modeOptions.map((o) => (
        <button
          key={o.mode}
          type="button"
          onClick={() => props.onModeChange(o.mode)}
        >
          mode-{o.mode}
        </button>
      ))}
    </div>
  ),
}));

const MODE_OPTIONS: HeroModeOption[] = [
  { mode: "favorites", label: "Tu favorito" },
  { mode: "live-now", label: "En directo" },
  { mode: "off", label: "Ocultar" },
];

function renderBar(
  overrides: Partial<React.ComponentProps<typeof LiveTvTopBar>> = {},
) {
  const props: React.ComponentProps<typeof LiveTvTopBar> = {
    tab: "discover",
    onTab: vi.fn(),
    search: "",
    onSearch: vi.fn(),
    totalChannels: 42,
    liveNow: 7,
    heroMode: "favorites",
    heroModeOptions: MODE_OPTIONS,
    onHeroModeChange: vi.fn(),
    ...overrides,
  };
  return { props, ...render(<LiveTvTopBar {...props} />) };
}

describe("LiveTvTopBar", () => {
  it("renders the total and live-now counts from props", () => {
    renderBar({ totalChannels: 268, liveNow: 19 });
    // Counts live in <b> tags; assert both are present somewhere in the header.
    expect(screen.getByText("268")).toBeInTheDocument();
    expect(screen.getByText("19")).toBeInTheDocument();
  });

  it("renders four tabs with role=tab and marks the active one aria-selected", () => {
    renderBar({ tab: "guide" });
    const tabs = screen.getAllByRole("tab");
    // Now / Descubrir / Guía / Favoritos. "Ahora" was promoted to a
    // first-class tab so the default landing answers "what to put on?"
    // directly instead of routing the user through editorial Discover.
    expect(tabs).toHaveLength(4);

    const selected = tabs.filter((t) => t.getAttribute("aria-selected") === "true");
    expect(selected).toHaveLength(1);
    expect(selected[0]).toHaveTextContent(/Guía/);
  });

  it("clicking a tab fires onTab with its id", () => {
    const onTab = vi.fn();
    renderBar({ tab: "discover", onTab });

    fireEvent.click(screen.getByRole("tab", { name: /Favoritos/ }));
    expect(onTab).toHaveBeenCalledWith("favorites");
  });

  it("search input is controlled and fires onSearch on every keystroke", () => {
    const onSearch = vi.fn();
    renderBar({ search: "ini", onSearch });

    const input = screen.getByRole("searchbox");
    expect(input).toHaveValue("ini");

    fireEvent.change(input, { target: { value: "news" } });
    expect(onSearch).toHaveBeenCalledWith("news");
  });

  it("HeroSettings is only rendered on the discover tab", () => {
    const { rerender, props } = renderBar({ tab: "discover" });
    expect(screen.getByTestId("hero-settings")).toBeInTheDocument();

    rerender(<LiveTvTopBar {...props} tab="guide" />);
    expect(screen.queryByTestId("hero-settings")).toBeNull();

    rerender(<LiveTvTopBar {...props} tab="favorites" />);
    expect(screen.queryByTestId("hero-settings")).toBeNull();
  });

  it("forwards the heroMode prop and wires onHeroModeChange when a mode is picked", () => {
    const onHeroModeChange = vi.fn();
    renderBar({
      tab: "discover",
      heroMode: "live-now",
      onHeroModeChange,
    });

    expect(screen.getByTestId("hero-settings")).toHaveAttribute(
      "data-mode",
      "live-now",
    );

    fireEvent.click(screen.getByText("mode-off"));
    expect(onHeroModeChange).toHaveBeenCalledWith("off");
  });
});
