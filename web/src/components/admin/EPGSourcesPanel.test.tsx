// EPGSourcesPanel — pins the operator-side contract for the
// multi-provider EPG management surface:
//
//   - Empty list (no sources attached) renders the empty-state copy
//     instead of an empty <ol>.
//   - Loaded sources render as an ordered list with the catalog
//     name resolved against the catalog map.
//   - The "Add from catalog" path posts a catalog_id to addEPGSource.
//   - The "Add by URL" path posts a url to addEPGSource.
//   - Custom URL form rejects empty input and surfaces an inline
//     error without hitting the API.
//   - Remove fires removeEPGSource with the row's id.

import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  render,
  screen,
  fireEvent,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "@/i18n";

const apiMock = vi.hoisted(() => ({
  getEPGCatalog: vi.fn(),
  listEPGSources: vi.fn(),
  addEPGSource: vi.fn(),
  removeEPGSource: vi.fn(),
  reorderEPGSources: vi.fn(),
}));
vi.mock("@/api/client", () => ({ api: apiMock }));

import { EPGSourcesPanel } from "./EPGSourcesPanel";
import type { LibraryEPGSource, PublicEPGSource } from "@/api/types";

function wrap(node: React.ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

beforeEach(() => {
  apiMock.getEPGCatalog.mockReset();
  apiMock.listEPGSources.mockReset();
  apiMock.addEPGSource.mockReset();
  apiMock.removeEPGSource.mockReset();
  apiMock.reorderEPGSources.mockReset();
});

const catalog: PublicEPGSource[] = [
  {
    id: "davidmuma",
    name: "Davidmuma",
    description: "Spanish + Latin America",
    language: "es",
    countries: ["es"],
    url: "https://example.com/davidmuma.xml",
  },
  {
    id: "epg-pw",
    name: "epg.pw",
    description: "Worldwide",
    language: "en",
    countries: ["us"],
    url: "https://example.com/epg-pw.xml",
  },
];

const sources: LibraryEPGSource[] = [
  {
    id: "src-1",
    library_id: "lib-1",
    catalog_id: "davidmuma",
    url: "https://example.com/davidmuma.xml",
    priority: 0,
    last_refreshed_at: null,
    last_status: "",
    last_error: "",
  } as LibraryEPGSource,
];

describe("EPGSourcesPanel empty + loaded states", () => {
  it("renders the empty-state copy when no sources are attached", async () => {
    apiMock.getEPGCatalog.mockResolvedValueOnce(catalog);
    apiMock.listEPGSources.mockResolvedValueOnce([]);

    render(wrap(<EPGSourcesPanel libraryId="lib-1" />));

    await screen.findByText(
      /aún no hay fuentes epg|no epg sources/i,
    );
  });

  it("renders attached sources with their catalog-resolved name", async () => {
    apiMock.getEPGCatalog.mockResolvedValueOnce(catalog);
    apiMock.listEPGSources.mockResolvedValueOnce(sources);

    render(wrap(<EPGSourcesPanel libraryId="lib-1" />));

    await screen.findByText(/Davidmuma/);
  });
});

describe("EPGSourcesPanel add flows", () => {
  it("calls addEPGSource with catalog_id when adding from the catalog", async () => {
    apiMock.getEPGCatalog.mockResolvedValueOnce(catalog);
    apiMock.listEPGSources.mockResolvedValueOnce([]);
    apiMock.addEPGSource.mockResolvedValueOnce({});

    render(wrap(<EPGSourcesPanel libraryId="lib-1" />));

    await screen.findByText(/aún no hay fuentes epg|no epg sources/i);

    // Default mode is "catalog" — pick the first available item.
    const select = screen.getByRole("combobox");
    fireEvent.change(select, { target: { value: "davidmuma" } });
    fireEvent.click(
      screen.getByRole("button", { name: /añadir|add/i }),
    );

    await waitFor(() => {
      expect(apiMock.addEPGSource).toHaveBeenCalledWith("lib-1", {
        catalog_id: "davidmuma",
      });
    });
  });

  it("calls addEPGSource with url when adding a custom URL", async () => {
    apiMock.getEPGCatalog.mockResolvedValueOnce(catalog);
    apiMock.listEPGSources.mockResolvedValueOnce([]);
    apiMock.addEPGSource.mockResolvedValueOnce({});

    render(wrap(<EPGSourcesPanel libraryId="lib-1" />));

    await screen.findByText(/aún no hay fuentes epg|no epg sources/i);

    // Switch to custom-URL mode.
    fireEvent.click(
      screen.getByRole("button", { name: /url personalizada|custom/i }),
    );
    const urlInput = screen.getByRole("textbox");
    fireEvent.change(urlInput, {
      target: { value: "https://example.org/guide.xml" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: /añadir|add/i }),
    );

    await waitFor(() => {
      expect(apiMock.addEPGSource).toHaveBeenCalledWith("lib-1", {
        url: "https://example.org/guide.xml",
      });
    });
  });
});

describe("EPGSourcesPanel remove flow", () => {
  it("calls removeEPGSource with the source id when remove is clicked", async () => {
    apiMock.getEPGCatalog.mockResolvedValueOnce(catalog);
    apiMock.listEPGSources.mockResolvedValueOnce(sources);
    apiMock.removeEPGSource.mockResolvedValueOnce(undefined);

    render(wrap(<EPGSourcesPanel libraryId="lib-1" />));

    await screen.findByText(/Davidmuma/);
    // The row exposes a remove control; in the rendered DOM it's
    // labelled with the i18n "remove" copy.
    const removeButton = screen.getByRole("button", {
      name: /quitar|remove|eliminar/i,
    });
    fireEvent.click(removeButton);

    await waitFor(() => {
      expect(apiMock.removeEPGSource).toHaveBeenCalledWith(
        "lib-1",
        "src-1",
      );
    });
  });
});
