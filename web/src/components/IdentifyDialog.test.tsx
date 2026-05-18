// IdentifyDialog — vitest spec. Cubre el camino dorado (busca,
// elige candidato, aplica) más los estados degradados (lista vacía,
// error de TMDb, fallo al aplicar) para que la UI de admin no se
// quede silenciosa cuando algo falla en producción.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "@/i18n";

import type { ItemDetail } from "@/api/types";

const apiMock = vi.hoisted(() => ({
  getIdentifyCandidates: vi.fn(),
  applyIdentify: vi.fn(),
}));
vi.mock("@/api/client", () => ({ api: apiMock }));

import { IdentifyDialog } from "./IdentifyDialog";

function wrap(node: React.ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

const stubItem: ItemDetail = {
  id: "item-1",
  library_id: "lib-1",
  type: "movie",
  title: "Wrong Title",
  year: 1999,
  media_streams: [],
} as unknown as ItemDetail;

beforeEach(() => {
  apiMock.getIdentifyCandidates.mockReset();
  apiMock.applyIdentify.mockReset();
  apiMock.applyIdentify.mockResolvedValue({
    item_id: "item-1",
    provider: "tmdb",
    external_id: "550",
  });
});

describe("IdentifyDialog", () => {
  it("does not query TMDb until the user presses Search", () => {
    render(wrap(<IdentifyDialog isOpen={true} onClose={vi.fn()} item={stubItem} />));

    // Pre-search hint visible, no fetch fired.
    expect(
      screen.getByText(/escribe el título correcto|type the correct title/i),
    ).toBeInTheDocument();
    expect(apiMock.getIdentifyCandidates).not.toHaveBeenCalled();
  });

  it("seeds the title + year from the current item and fetches on Search", async () => {
    apiMock.getIdentifyCandidates.mockResolvedValue([
      {
        external_id: "550",
        provider: "tmdb",
        title: "Fight Club",
        year: 1999,
        overview: "An insomniac office worker...",
        poster_url: "https://image.tmdb.org/poster.jpg",
        score: 0.95,
      },
    ]);

    render(wrap(<IdentifyDialog isOpen={true} onClose={vi.fn()} item={stubItem} />));

    // Title and year inputs are pre-filled.
    const titleInput = screen.getByLabelText(/título|title/i) as HTMLInputElement;
    const yearInput = screen.getByLabelText(/año|year/i) as HTMLInputElement;
    expect(titleInput.value).toBe("Wrong Title");
    expect(yearInput.value).toBe("1999");

    // User corrects the title and searches.
    fireEvent.change(titleInput, { target: { value: "Fight Club" } });
    fireEvent.click(screen.getByRole("button", { name: /buscar|^search$/i }));

    await waitFor(() => {
      expect(apiMock.getIdentifyCandidates).toHaveBeenCalledWith("item-1", {
        query: "Fight Club",
        year: 1999,
      });
    });

    expect(await screen.findByTestId("identify-candidate")).toBeInTheDocument();
    expect(screen.getByText("Fight Club")).toBeInTheDocument();
  });

  it("applies the selected candidate and closes on success", async () => {
    apiMock.getIdentifyCandidates.mockResolvedValue([
      {
        external_id: "550",
        provider: "tmdb",
        title: "Fight Club",
        year: 1999,
        overview: "",
        poster_url: "",
        score: 0.95,
      },
    ]);
    const onClose = vi.fn();

    render(wrap(<IdentifyDialog isOpen={true} onClose={onClose} item={stubItem} />));

    fireEvent.click(screen.getByRole("button", { name: /buscar|^search$/i }));

    // The card button doubles as the radio (data-testid + role="radio"
    // live on the same node), so click it directly.
    const candidate = await screen.findByTestId("identify-candidate");
    fireEvent.click(candidate);

    // Apply is enabled now.
    const applyBtn = screen.getByRole("button", { name: /aplicar match|apply match/i });
    expect(applyBtn).toBeEnabled();
    fireEvent.click(applyBtn);

    await waitFor(() => {
      expect(apiMock.applyIdentify).toHaveBeenCalledWith("item-1", {
        provider: "tmdb",
        external_id: "550",
      });
    });
    expect(onClose).toHaveBeenCalled();
  });

  it("disables Apply when no candidate has been selected", async () => {
    apiMock.getIdentifyCandidates.mockResolvedValue([]);

    render(wrap(<IdentifyDialog isOpen={true} onClose={vi.fn()} item={stubItem} />));

    fireEvent.click(screen.getByRole("button", { name: /buscar|^search$/i }));

    await screen.findByText(/ningún match|no matches/i);

    expect(
      screen.getByRole("button", { name: /aplicar match|apply match/i }),
    ).toBeDisabled();
  });

  it("surfaces a search error so the operator knows TMDb failed", async () => {
    apiMock.getIdentifyCandidates.mockRejectedValue(new Error("502"));

    render(wrap(<IdentifyDialog isOpen={true} onClose={vi.fn()} item={stubItem} />));

    fireEvent.click(screen.getByRole("button", { name: /buscar|^search$/i }));

    expect(
      await screen.findByText(/no se ha podido consultar tmdb|could not query tmdb/i),
    ).toBeInTheDocument();
  });
});
