import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "@/i18n";

// Hoisted mocks de los dos hooks de datos. useSearch hace /items/search,
// usePeersSearch fan-out a peers federados. Los probamos por separado:
// el estado de Search.tsx depende de las dos respuestas combinadas.
const mocks = vi.hoisted(() => ({
  useSearch: vi.fn(),
  usePeersSearch: vi.fn(),
}));

vi.mock("@/api/hooks", async () => {
  const actual =
    await vi.importActual<typeof import("@/api/hooks")>("@/api/hooks");
  return { ...actual, useSearch: mocks.useSearch };
});
vi.mock("@/api/hooks/federation", async () => {
  const actual = await vi.importActual<
    typeof import("@/api/hooks/federation")
  >("@/api/hooks/federation");
  return { ...actual, usePeersSearch: mocks.usePeersSearch };
});

// Stub de SearchResultsView para aislar la lógica de Search.tsx (heading
// + branches empty/loading/no-results/results) del render de la grid.
vi.mock("@/components/search/SearchResultsView", () => ({
  SearchResultsView: ({
    items,
    peerHits,
  }: {
    items: { id: string }[];
    peerHits: { id: string }[];
  }) => (
    <div data-testid="results">
      <span data-testid="local-count">{items.length}</span>
      <span data-testid="peer-count">{peerHits.length}</span>
    </div>
  ),
  SearchNoResults: ({ query }: { query: string }) => (
    <div data-testid="no-results">no:{query}</div>
  ),
}));

import Search from "./Search";

function Wrapper({
  children,
  initialURL,
}: {
  children: React.ReactNode;
  initialURL: string;
}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[initialURL]}>
        <Routes>
          <Route path="/search" element={children} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  );
}

function setHook(
  hook: ReturnType<typeof vi.fn>,
  data: unknown,
  isFetching = false,
) {
  hook.mockReturnValue({ data, isFetching });
}

beforeEach(() => {
  mocks.useSearch.mockReset();
  mocks.usePeersSearch.mockReset();
  setHook(mocks.useSearch, []);
  setHook(mocks.usePeersSearch, { hits: [] });
});

describe("Search page", () => {
  it("muestra empty state cuando no hay query", () => {
    render(
      <Wrapper initialURL="/search">
        <Search />
      </Wrapper>,
    );
    // EmptyState renderiza el title de i18n; el heading queda con el
    // texto "Buscar" / "Search" sin el formato "Resultados para …".
    expect(screen.queryByTestId("results")).not.toBeInTheDocument();
    expect(screen.queryByTestId("no-results")).not.toBeInTheDocument();
  });

  it("muestra spinner inicial mientras ambos hooks fetchean sin datos", () => {
    setHook(mocks.useSearch, undefined, true);
    setHook(mocks.usePeersSearch, undefined, true);
    render(
      <Wrapper initialURL="/search?q=batman">
        <Search />
      </Wrapper>,
    );
    // El spinner es el único container con role=status que pinta Search
    // mientras `showInitialSpinner` está activo.
    expect(screen.queryByTestId("results")).not.toBeInTheDocument();
    expect(screen.queryByTestId("no-results")).not.toBeInTheDocument();
  });

  it("no-results: query con respuesta vacía local Y peers", () => {
    setHook(mocks.useSearch, []);
    setHook(mocks.usePeersSearch, { hits: [] });
    render(
      <Wrapper initialURL="/search?q=batman">
        <Search />
      </Wrapper>,
    );
    // El debounce de 220 ms del componente no aplica aquí porque
    // `useDebounce` corre con timers reales: el primer render expone
    // el valor inicial del hook (que es el `q` ya leído del URL).
    expect(screen.getByTestId("no-results")).toHaveTextContent("no:batman");
  });

  it("renderiza resultados locales", () => {
    setHook(mocks.useSearch, [
      { id: "m1", title: "Batman Begins", type: "movie" },
      { id: "m2", title: "The Batman", type: "movie" },
    ]);
    setHook(mocks.usePeersSearch, { hits: [] });
    render(
      <Wrapper initialURL="/search?q=batman">
        <Search />
      </Wrapper>,
    );
    expect(screen.getByTestId("local-count")).toHaveTextContent("2");
    expect(screen.getByTestId("peer-count")).toHaveTextContent("0");
  });

  it("incluye hits federados de peers en el resumen", () => {
    setHook(mocks.useSearch, [{ id: "m1", title: "Batman Begins" }]);
    setHook(mocks.usePeersSearch, {
      hits: [
        { peer_id: "p1", peer_name: "Casa", item: { id: "p1m1" } },
        { peer_id: "p2", peer_name: "Cabaña", item: { id: "p2m1" } },
      ],
    });
    render(
      <Wrapper initialURL="/search?q=batman">
        <Search />
      </Wrapper>,
    );
    expect(screen.getByTestId("local-count")).toHaveTextContent("1");
    expect(screen.getByTestId("peer-count")).toHaveTextContent("2");
  });

  it("muestra resultados aunque uno de los hooks aún esté fetcheando", () => {
    // Late-arriving peer hits no deben dejar caer los locales al
    // spinner — el spinner sólo se muestra mientras nada hay en
    // pantalla (`!hasAny`).
    setHook(mocks.useSearch, [{ id: "m1" }]);
    setHook(mocks.usePeersSearch, undefined, true);
    render(
      <Wrapper initialURL="/search?q=batman">
        <Search />
      </Wrapper>,
    );
    expect(screen.getByTestId("results")).toBeInTheDocument();
    expect(screen.getByTestId("local-count")).toHaveTextContent("1");
  });
});
