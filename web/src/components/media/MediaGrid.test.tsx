import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "@/i18n";
import type { MediaItem } from "@/api/types";
import { MediaGrid } from "./MediaGrid";

// ─── Item factory ────────────────────────────────────────────────────────────

function makeItems(n: number, prefix = "it"): MediaItem[] {
  return Array.from({ length: n }, (_, i) => ({
    id: `${prefix}-${i}`,
    type: "movie",
    title: `Item ${i}`,
    original_title: null,
    year: 2020,
    sort_title: `item ${i}`,
    overview: null,
    tagline: null,
    genres: [],
    community_rating: null,
    content_rating: null,
    duration_ticks: null,
    premiere_date: null,
    poster_url: null,
    backdrop_url: null,
    logo_url: null,
    parent_id: null,
    series_id: null,
    season_number: null,
    episode_number: null,
    path: null,
  }));
}

function makeClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
}

function renderGrid(items: MediaItem[], loading = false, emptyMessage?: string) {
  return render(
    <QueryClientProvider client={makeClient()}>
      <MemoryRouter>
        <MediaGrid items={items} loading={loading} emptyMessage={emptyMessage} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

// ─── Tests ───────────────────────────────────────────────────────────────────

describe("MediaGrid", () => {
  it("renders 8 skeleton placeholders when loading", () => {
    const { container } = renderGrid([], true);
    const skeletons = container.querySelectorAll(".aspect-\\[2\\/3\\]");
    expect(skeletons).toHaveLength(8);
  });

  it("renders the empty state when not loading and no items", () => {
    renderGrid([], false, "No movies found");
    expect(screen.getByText("No movies found")).toBeInTheDocument();
  });

  it("renders every card directly for a small catalogue (below threshold)", () => {
    renderGrid(makeItems(12));
    expect(screen.getByTestId("media-grid")).toBeInTheDocument();
    expect(screen.getByText("Item 0")).toBeInTheDocument();
    expect(screen.getByText("Item 11")).toBeInTheDocument();
    expect(screen.getAllByTestId("poster-card")).toHaveLength(12);
  });

  // Guardia de regresión (A12): por encima del umbral se conmuta a la ruta
  // VIRTUALIZADA, que NO mete todas las tarjetas en el DOM. Aquí solo se
  // comprueba que se usa ese contenedor (y no la ruta directa con las N
  // tarjetas) — el conteo acotado real y el reciclado al scrollear se
  // verifican en navegador (web/verify/, jsdom no tiene layout).
  it("switches to the virtualized container for large catalogues", () => {
    renderGrid(makeItems(2000));
    expect(screen.getByTestId("media-grid-virtualized")).toBeInTheDocument();
    expect(screen.queryByTestId("media-grid")).not.toBeInTheDocument();
    // La ruta directa habría montado 2000 tarjetas; la virtualizada nunca.
    expect(screen.queryAllByTestId("poster-card").length).toBeLessThan(2000);
  });
});
