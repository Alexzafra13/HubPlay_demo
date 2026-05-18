import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, act } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "@/i18n";
import type { MediaItem } from "@/api/types";
import { MediaGrid } from "./MediaGrid";

// ─── IntersectionObserver mock ───────────────────────────────────────────────
//
// jsdom doesn't ship one. We replace it with a class that exposes the
// callback so the test can synthesise an intersection event the same
// way the browser would. Each instance captures itself in a registry
// the test reads from.

interface FakeObserver {
  callback: IntersectionObserverCallback;
  options?: IntersectionObserverInit;
  observed: Element[];
  disconnected: boolean;
}

let observers: FakeObserver[] = [];

class MockIntersectionObserver implements IntersectionObserver {
  root: Element | Document | null = null;
  rootMargin = "";
  thresholds: ReadonlyArray<number> = [];
  private record: FakeObserver;

  constructor(cb: IntersectionObserverCallback, options?: IntersectionObserverInit) {
    this.record = { callback: cb, options, observed: [], disconnected: false };
    observers.push(this.record);
  }

  observe(target: Element): void { this.record.observed.push(target); }
  unobserve(): void {}
  disconnect(): void { this.record.disconnected = true; }
  takeRecords(): IntersectionObserverEntry[] { return []; }
}

beforeEach(() => {
  observers = [];
  vi.stubGlobal("IntersectionObserver", MockIntersectionObserver);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

/**
 * Trigger the most recently created observer's callback as if its
 * sentinel had scrolled into view. No-ops when no observer is alive
 * (e.g. all items already visible — sentinel unmounted).
 */
function triggerLatestIntersect() {
  const live = observers.filter((o) => !o.disconnected);
  const latest = live[live.length - 1];
  if (!latest || latest.observed.length === 0) return;
  const entry = {
    isIntersecting: true,
    target: latest.observed[0],
    intersectionRatio: 1,
    boundingClientRect: {} as DOMRectReadOnly,
    intersectionRect: {} as DOMRectReadOnly,
    rootBounds: null,
    time: 0,
  } as IntersectionObserverEntry;
  act(() => {
    latest.callback([entry], latest as unknown as IntersectionObserver);
  });
}

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

// Helper común para los tests: las cards (vía ItemKebab) necesitan
// un QueryClient en context para el botón "Actualizar metadatos",
// aunque los tests no ejerciten ese flujo. retry: false evita esperas.
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
  it("renders SKELETON_COUNT (8) skeleton placeholders when loading", () => {
    const { container } = renderGrid([], true);
    // Match the SKELETON_COUNT constant in MediaGrid exactly. A loose
    // ">= 8" would silently swallow regressions where someone bumps
    // the count without updating the contract.
    const skeletons = container.querySelectorAll(".aspect-\\[2\\/3\\]");
    expect(skeletons).toHaveLength(8);
  });

  it("renders the empty state when not loading and no items", () => {
    renderGrid([], false, "No movies found");
    expect(screen.getByText("No movies found")).toBeInTheDocument();
  });

  it("renders only the first BATCH_SIZE (40) cards when more items are available", () => {
    renderGrid(makeItems(120));
    const links = screen.getAllByRole("link");
    expect(links).toHaveLength(40);
  });

  it("grows visibleCount by BATCH_SIZE when the sentinel intersects", () => {
    renderGrid(makeItems(120));
    expect(screen.getAllByRole("link")).toHaveLength(40);

    triggerLatestIntersect();
    expect(screen.getAllByRole("link")).toHaveLength(80);

    triggerLatestIntersect();
    expect(screen.getAllByRole("link")).toHaveLength(120);
  });

  it("does not mount the sentinel when items < BATCH_SIZE", () => {
    // 30 items < BATCH_SIZE (40), so all fit in the first slice and
    // the sentinel never needs to render. La aserción busca el div
    // específico del sentinel (className="h-1" + aria-hidden) — el
    // resto de [aria-hidden] son iconos de las cards (kebab, etc.)
    // que no nos interesa contar aquí.
    const { container } = renderGrid(makeItems(30));
    expect(container.querySelectorAll('div.h-1[aria-hidden="true"]')).toHaveLength(0);
  });

  it("resets visibleCount when the items reference changes (new search/filter)", () => {
    const client = makeClient();
    const { rerender } = render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <MediaGrid items={makeItems(120, "first")} loading={false} />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    triggerLatestIntersect();
    triggerLatestIntersect();
    expect(screen.getAllByRole("link")).toHaveLength(120);

    // Caller swaps in a fresh array (e.g. new search). The compare-
    // during-render pattern in MediaGrid should snap visibleCount
    // back to BATCH_SIZE so the user lands at the top of the new
    // result set, not deep inside the previous one.
    rerender(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <MediaGrid items={makeItems(80, "second")} loading={false} />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect(screen.getAllByRole("link")).toHaveLength(40);
  });

  it("disconnects the observer when the grid unmounts", () => {
    const { unmount } = renderGrid(makeItems(120));
    expect(observers.some((o) => !o.disconnected)).toBe(true);
    unmount();
    expect(observers.every((o) => o.disconnected)).toBe(true);
  });
});
