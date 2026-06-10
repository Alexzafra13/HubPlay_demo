import { describe, it, expect, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import "@/i18n";
import { TimeDisplay } from "./TimeDisplay";

beforeEach(() => {
  window.localStorage.clear();
});

describe("TimeDisplay — toggle total ↔ restante", () => {
  it("muestra la duración total por defecto", () => {
    render(<TimeDisplay currentTime={60} duration={600} />);
    const btn = screen.getByRole("button", { name: /alternar|toggle/i });
    expect(btn).toHaveTextContent("1:00");
    expect(btn).toHaveTextContent("10:00");
  });

  it("al tocar el contador cambia a tiempo restante con signo −", () => {
    render(<TimeDisplay currentTime={60} duration={600} />);
    const btn = screen.getByRole("button", { name: /alternar|toggle/i });
    fireEvent.click(btn);
    expect(btn).toHaveTextContent("−9:00");
  });

  it("persiste la preferencia en localStorage", () => {
    const { unmount } = render(<TimeDisplay currentTime={60} duration={600} />);
    fireEvent.click(screen.getByRole("button", { name: /alternar|toggle/i }));
    unmount();

    // Un montaje nuevo (otro vídeo, otra sesión) arranca en restante.
    render(<TimeDisplay currentTime={120} duration={600} />);
    expect(screen.getByRole("button", { name: /alternar|toggle/i })).toHaveTextContent("−8:00");
  });
});
