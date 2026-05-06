import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "@/i18n";

// Hoisted mock so the api module is replaced before MediaBrowse imports it.
const apiMock = vi.hoisted(() => ({
  getItems: vi.fn(),
  getGenres: vi.fn(),
}));

vi.mock("@/api/client", () => ({
  api: apiMock,
}));

// Stub MediaGrid so we can assert on the items prop without relying on
// the real grid's image loading + intersection-observer pipeline.
vi.mock("@/components/media", async () => {
  const actual =
    await vi.importActual<typeof import("@/components/media")>("@/components/media");
  return {
    ...actual,
    MediaGrid: ({ items }: { items: { id: string; title: string }[] }) => (
      <ul data-testid="media-grid">
        {items.map((item) => (
          <li key={item.id} data-testid="media-item">
            {item.title}
          </li>
        ))}
      </ul>
    ),
  };
});

import MediaBrowse from "./MediaBrowse";

function makeWrapper(initialURL: string) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    return (
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={[initialURL]}>
          <Routes>
            <Route path="/movies" element={children} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    );
  };
}

beforeEach(() => {
  apiMock.getItems.mockReset();
  apiMock.getGenres.mockReset();
  apiMock.getItems.mockResolvedValue({ items: [], total: 0 });
  apiMock.getGenres.mockResolvedValue([]);
});

describe("MediaBrowse — server-driven filters", () => {
  it("forwards URL query params to /items as genre / year / rating / q", async () => {
    apiMock.getItems.mockResolvedValueOnce({
      items: [
        {
          id: "m1",
          type: "movie",
          title: "The Result",
          original_title: null,
          year: 2015,
          sort_title: "result",
        },
      ],
      total: 1,
    });

    const Wrapper = makeWrapper(
      "/movies?q=batman&genre=Action&year_from=2010&year_to=2020&min_rating=7",
    );
    render(
      <Wrapper>
        <MediaBrowse type="movie" />
      </Wrapper>,
    );

    await waitFor(() => {
      expect(apiMock.getItems).toHaveBeenCalled();
    });
    const call = apiMock.getItems.mock.calls[0][0];
    expect(call).toMatchObject({
      type: "movie",
      q: "batman",
      genre: "Action",
      year_from: 2010,
      year_to: 2020,
      min_rating: 7,
    });
    // Default sort still maps to the alphabetical wire pair.
    expect(call.sort_by).toBe("sort_title");
    expect(call.sort_order).toBe("asc");

    expect(await screen.findByText("The Result")).toBeInTheDocument();
  });

  it("does not filter the grid client-side: every item the API returns is shown", async () => {
    apiMock.getItems.mockResolvedValueOnce({
      items: [
        {
          id: "a",
          type: "movie",
          title: "Match",
          original_title: null,
          year: 1999,
          sort_title: "match",
        },
        {
          id: "b",
          type: "movie",
          // Title that wouldn't match the URL `?q=` if filtering were
          // happening locally — proves the page trusts the server's
          // result set rather than re-running a substring filter.
          title: "Completely Different Title",
          original_title: null,
          year: 1999,
          sort_title: "completely different title",
        },
      ],
      total: 2,
    });

    const Wrapper = makeWrapper("/movies?q=match");
    render(
      <Wrapper>
        <MediaBrowse type="movie" />
      </Wrapper>,
    );

    const items = await screen.findAllByTestId("media-item");
    expect(items).toHaveLength(2);
  });

  it("writes filter changes back to the URL query string", async () => {
    apiMock.getGenres.mockResolvedValueOnce([
      { name: "Drama", count: 5 },
      { name: "Action", count: 3 },
    ]);

    const Wrapper = makeWrapper("/movies?filters_open=1");
    render(
      <Wrapper>
        <MediaBrowse type="movie" />
      </Wrapper>,
    );

    // Wait for the genre chips to render (driven by /items/genres).
    const dramaChip = await screen.findByRole("button", { name: /Drama/ });
    fireEvent.click(dramaChip);

    await waitFor(() => {
      const calls = apiMock.getItems.mock.calls.map((c) => c[0]);
      expect(calls.some((p) => p.genre === "Drama")).toBe(true);
    });
  });
});
