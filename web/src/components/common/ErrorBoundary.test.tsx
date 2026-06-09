import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { ErrorBoundary } from "./ErrorBoundary";

function Boom(): React.ReactElement {
  throw new Error("kaboom");
}

describe("ErrorBoundary", () => {
  beforeEach(() => {
    // El boundary loguea el error con console.error; lo silenciamos para
    // no ensuciar la salida del test.
    vi.spyOn(console, "error").mockImplementation(() => {});
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renderiza los hijos cuando no hay error", () => {
    render(
      <ErrorBoundary>
        <p>contenido ok</p>
      </ErrorBoundary>,
    );
    expect(screen.getByText("contenido ok")).toBeInTheDocument();
  });

  it("captura el crash de un hijo y muestra el fallback por defecto", () => {
    render(
      <ErrorBoundary>
        <Boom />
      </ErrorBoundary>,
    );
    expect(screen.getByText("Something went wrong")).toBeInTheDocument();
    expect(screen.getByText("kaboom")).toBeInTheDocument();
  });

  it("usa el fallback custom si se pasa", () => {
    render(
      <ErrorBoundary fallback={<div>fallback a medida</div>}>
        <Boom />
      </ErrorBoundary>,
    );
    expect(screen.getByText("fallback a medida")).toBeInTheDocument();
  });

  // Esta es la garantía clave de A11: AppLayout monta el boundary con
  // key={pathname}, así que navegar a otra ruta remonta el boundary y
  // limpia el error sin intervención del usuario.
  it("se resetea al cambiar el key (simula navegar a otra ruta)", () => {
    const { rerender } = render(
      <ErrorBoundary key="/movies">
        <Boom />
      </ErrorBoundary>,
    );
    expect(screen.getByText("Something went wrong")).toBeInTheDocument();

    rerender(
      <ErrorBoundary key="/series">
        <p>otra página</p>
      </ErrorBoundary>,
    );
    expect(screen.getByText("otra página")).toBeInTheDocument();
    expect(screen.queryByText("Something went wrong")).not.toBeInTheDocument();
  });
});
