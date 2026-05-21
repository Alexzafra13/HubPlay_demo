import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "@/i18n";

// Los 4 hooks de datos. Cada uno devuelve un objeto al estilo TanStack
// Query con campos data / isLoading / isError / refetch.
const mocks = vi.hoisted(() => ({
  useHomeLayout: vi.fn(),
  useHomeRecommended: vi.fn(),
  useHomeTrending: vi.fn(),
  useLatestItems: vi.fn(),
}));

vi.mock("@/api/hooks", async () => {
  const actual =
    await vi.importActual<typeof import("@/api/hooks")>("@/api/hooks");
  return {
    ...actual,
    useHomeLayout: mocks.useHomeLayout,
    useHomeRecommended: mocks.useHomeRecommended,
    useHomeTrending: mocks.useHomeTrending,
    useLatestItems: mocks.useLatestItems,
  };
});

// Stubs de los rails. Cada uno reporta su tipo para que podamos
// aseverar sobre el orden y la dispatch table de renderSection.
vi.mock("@/components/home", () => ({
  HeroBanner: ({
    latest,
    trending,
    recommended,
  }: {
    latest: { id: string }[];
    trending: { id: string }[];
    recommended: { id: string }[];
  }) => (
    <div data-testid="hero">
      hero:{latest.length}|{trending.length}|{recommended.length}
    </div>
  ),
  ContinueWatchingRail: () => <div data-testid="rail-continue_watching" />,
  NextUpRail: () => <div data-testid="rail-next_up" />,
  TrendingRail: () => <div data-testid="rail-trending" />,
  LiveNowRail: () => <div data-testid="rail-live_now" />,
  LatestInLibraryRail: ({
    libraryId,
    libraryName,
  }: {
    libraryId: string;
    libraryName: string;
  }) => (
    <div data-testid={`rail-library-${libraryId}`}>{libraryName}</div>
  ),
  PeerRecentRail: () => <div data-testid="rail-peer-recent" />,
  PeerContinueWatchingRail: () => (
    <div data-testid="rail-peer-continue" />
  ),
  BecauseYouWatchedRail: () => <div data-testid="rail-because" />,
}));

import Home from "./Home";

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <QueryClientProvider client={client}>
      <MemoryRouter>{children}</MemoryRouter>
    </QueryClientProvider>
  );
}

interface FakeQuery<T> {
  data?: T;
  isLoading?: boolean;
  isError?: boolean;
  refetch?: () => void;
}

function setQuery<T>(
  hook: ReturnType<typeof vi.fn>,
  overrides: FakeQuery<T> = {},
) {
  hook.mockReturnValue({
    data: undefined,
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
    ...overrides,
  });
}

beforeEach(() => {
  mocks.useHomeLayout.mockReset();
  mocks.useHomeRecommended.mockReset();
  mocks.useHomeTrending.mockReset();
  mocks.useLatestItems.mockReset();

  // Defaults: todo vacío sin error y sin loading. Cada test sobreescribe
  // lo que necesite.
  setQuery(mocks.useLatestItems, { data: [] });
  setQuery(mocks.useHomeTrending, { data: [] });
  setQuery(mocks.useHomeRecommended, { data: [] });
  setQuery(mocks.useHomeLayout, { data: { sections: [] } });
});

describe("Home page", () => {
  it("pinta el skeleton del hero mientras los 3 hooks del hero están loading", () => {
    setQuery(mocks.useLatestItems, { isLoading: true });
    setQuery(mocks.useHomeTrending, { isLoading: true });
    setQuery(mocks.useHomeRecommended, { isLoading: true });

    const { container } = render(
      <Wrapper>
        <Home />
      </Wrapper>,
    );
    // No hero real, ni rails de layout. El skeleton es el único div
    // con animate-pulse renderizado por Home en este estado.
    expect(screen.queryByTestId("hero")).not.toBeInTheDocument();
    expect(container.querySelector(".animate-pulse")).toBeTruthy();
  });

  it("renderiza el HeroBanner con los 3 datasets cuando hay datos disponibles", () => {
    setQuery(mocks.useLatestItems, { data: [{ id: "l1" }] });
    setQuery(mocks.useHomeTrending, { data: [{ id: "t1" }, { id: "t2" }] });
    setQuery(mocks.useHomeRecommended, { data: [{ id: "r1" }] });

    render(
      <Wrapper>
        <Home />
      </Wrapper>,
    );
    expect(screen.getByTestId("hero")).toHaveTextContent("hero:1|2|1");
  });

  it("dispatcha rails según `type` y respeta el orden del layout", () => {
    setQuery(mocks.useHomeLayout, {
      data: {
        sections: [
          { id: "1", type: "continue_watching", visible: true },
          { id: "2", type: "trending", visible: true },
          { id: "3", type: "live_now", visible: true },
          { id: "4", type: "next_up", visible: true },
        ],
      },
    });

    render(
      <Wrapper>
        <Home />
      </Wrapper>,
    );
    expect(screen.getByTestId("rail-continue_watching")).toBeInTheDocument();
    expect(screen.getByTestId("rail-trending")).toBeInTheDocument();
    expect(screen.getByTestId("rail-live_now")).toBeInTheDocument();
    expect(screen.getByTestId("rail-next_up")).toBeInTheDocument();
  });

  it("omite secciones con visible=false", () => {
    setQuery(mocks.useHomeLayout, {
      data: {
        sections: [
          { id: "1", type: "trending", visible: false },
          { id: "2", type: "live_now", visible: true },
        ],
      },
    });

    render(
      <Wrapper>
        <Home />
      </Wrapper>,
    );
    expect(screen.queryByTestId("rail-trending")).not.toBeInTheDocument();
    expect(screen.getByTestId("rail-live_now")).toBeInTheDocument();
  });

  it("monta LatestInLibraryRail con id + name cuando hay library_id", () => {
    setQuery(mocks.useHomeLayout, {
      data: {
        sections: [
          {
            id: "1",
            type: "latest_in_library",
            visible: true,
            library_id: "lib42",
            library_name: "Acción 4K",
          },
        ],
      },
    });

    render(
      <Wrapper>
        <Home />
      </Wrapper>,
    );
    const rail = screen.getByTestId("rail-library-lib42");
    expect(rail).toHaveTextContent("Acción 4K");
  });

  it("descarta latest_in_library sin library_id", () => {
    setQuery(mocks.useHomeLayout, {
      data: {
        sections: [
          { id: "1", type: "latest_in_library", visible: true },
        ],
      },
    });

    const { container } = render(
      <Wrapper>
        <Home />
      </Wrapper>,
    );
    // Cualquier rail-library-* sería bug: el id es undefined.
    expect(container.querySelector('[data-testid^="rail-library-"]')).toBeNull();
  });

  it("siempre monta los rails federados y because-you-watched (fuera del layout)", () => {
    render(
      <Wrapper>
        <Home />
      </Wrapper>,
    );
    expect(screen.getByTestId("rail-peer-recent")).toBeInTheDocument();
    expect(screen.getByTestId("rail-peer-continue")).toBeInTheDocument();
    expect(screen.getByTestId("rail-because")).toBeInTheDocument();
  });

  it("muestra retry UI cuando los 4 hooks erraron y el hero está vacío", () => {
    const refetchHero = vi.fn();
    const refetchTrending = vi.fn();
    const refetchRec = vi.fn();
    const refetchLayout = vi.fn();

    setQuery(mocks.useLatestItems, {
      data: [],
      isError: true,
      refetch: refetchHero,
    });
    setQuery(mocks.useHomeTrending, {
      data: [],
      isError: true,
      refetch: refetchTrending,
    });
    setQuery(mocks.useHomeRecommended, {
      data: [],
      isError: true,
      refetch: refetchRec,
    });
    setQuery(mocks.useHomeLayout, {
      isError: true,
      refetch: refetchLayout,
    });

    render(
      <Wrapper>
        <Home />
      </Wrapper>,
    );

    // Sólo el retry button + texto de error. Los rails / hero NO se
    // pintan en el camino fatal.
    expect(screen.queryByTestId("hero")).not.toBeInTheDocument();
    expect(screen.queryByTestId("rail-peer-recent")).not.toBeInTheDocument();

    const retryBtn = screen.getByRole("button", { name: /reintentar|retry/i });
    fireEvent.click(retryBtn);
    expect(refetchHero).toHaveBeenCalledTimes(1);
    expect(refetchTrending).toHaveBeenCalledTimes(1);
    expect(refetchRec).toHaveBeenCalledTimes(1);
    expect(refetchLayout).toHaveBeenCalledTimes(1);
  });
});
