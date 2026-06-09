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

  it("renders the first items of a short list", () => {
    renderGrid(makeItems(12));
    expect(screen.getByText("Item 0")).toBeInTheDocument();
    expect(screen.getAllByTestId("poster-card").length).toBeGreaterThan(0);
  });

  // Guardia de regresión de la virtualización (A12): con una lista enorme,
  // el grid NO debe meter las miles de tarjetas en el DOM, solo una ventana
  // de filas visibles. En jsdom el virtualizador cae a `estimateSize`
  // (ResizeObserver es no-op), así que el render queda acotado y
  // determinista. El grid acumulativo anterior renderizaba ~todas → este
  // test lo habría hecho fallar.
  it("solo monta una ventana acotada de tarjetas para listas grandes", () => {
    renderGrid(makeItems(2000));
    const cards = screen.getAllByTestId("poster-card");
    expect(cards.length).toBeGreaterThan(0);
    // Muy por debajo de 2000: si alguien rompe la virtualización (vuelve a
    // renderizar todo), este límite salta.
    expect(cards.length).toBeLessThan(300);
    // Y los ítems del final NO están en el DOM hasta scrollear.
    expect(screen.queryByText("Item 1999")).not.toBeInTheDocument();
  });
});
