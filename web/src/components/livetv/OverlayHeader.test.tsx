import { describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import type { Channel } from "@/api/types";
import { OverlayHeader } from "./OverlayHeader";

function channel(overrides: Partial<Channel> = {}): Channel {
  return {
    id: "c1",
    library_id: "lib1",
    name: "La 1 HD",
    number: 1,
    group: null,
    group_name: null,
    category: "news",
    logo_initials: "L1",
    logo_bg: "#111",
    logo_fg: "#fff",
    logo_url: null,
    stream_url: "http://stream/c1",
    language: "",
    country: "es",
    is_active: true,
    ...overrides,
  };
}

describe("OverlayHeader", () => {
  it("renders channel identity (number, name, category, country)", () => {
    render(<OverlayHeader channel={channel()} onClose={vi.fn()} />);
    expect(screen.getByText("CH 1")).toBeInTheDocument();
    expect(screen.getByText("La 1 HD")).toBeInTheDocument();
    // Category is capitalised for display.
    expect(screen.getByText(/News/)).toBeInTheDocument();
    // Country is upper-cased.
    expect(screen.getByText(/ES/)).toBeInTheDocument();
  });

  it("fires onClose when the back button is clicked", () => {
    const onClose = vi.fn();
    render(<OverlayHeader channel={channel()} onClose={onClose} />);
    fireEvent.click(screen.getByRole("button", { name: "Cerrar" }));
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("omits the favorite button when no onToggleFavorite is provided", () => {
    render(<OverlayHeader channel={channel()} onClose={vi.fn()} />);
    expect(
      screen.queryByRole("button", { name: /favoritos/ }),
    ).toBeNull();
  });

  it("renders 'Añadir a favoritos' when isFavorite is false", () => {
    render(
      <OverlayHeader
        channel={channel()}
        onClose={vi.fn()}
        onToggleFavorite={vi.fn()}
      />,
    );
    const btn = screen.getByRole("button", { name: "Añadir a favoritos" });
    expect(btn).toHaveAttribute("aria-pressed", "false");
  });

  it("renders 'Quitar de favoritos' when isFavorite is true", () => {
    render(
      <OverlayHeader
        channel={channel()}
        isFavorite
        onClose={vi.fn()}
        onToggleFavorite={vi.fn()}
      />,
    );
    const btn = screen.getByRole("button", { name: "Quitar de favoritos" });
    expect(btn).toHaveAttribute("aria-pressed", "true");
  });

  it("fires onToggleFavorite when the heart is clicked", () => {
    const onToggle = vi.fn();
    render(
      <OverlayHeader
        channel={channel()}
        onClose={vi.fn()}
        onToggleFavorite={onToggle}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /favoritos/ }));
    expect(onToggle).toHaveBeenCalledOnce();
  });

  it("leaves country segment off when channel has no country", () => {
    render(
      <OverlayHeader
        channel={channel({ country: "" })}
        onClose={vi.fn()}
      />,
    );
    // The capitalised category is still shown; the separator + country
    // suffix is not.
    expect(screen.queryByText(/· ES/i)).toBeNull();
  });
});
