import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import "@/i18n";
import { IndexersPanel } from "./IndexersPanel";
import { api } from "@/api/client";

vi.mock("@/api/client", () => ({
  api: {
    listIndexers: vi.fn(),
    createIndexer: vi.fn(),
    updateIndexer: vi.fn(),
    deleteIndexer: vi.fn(),
    testIndexer: vi.fn(),
  },
}));

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

beforeEach(() => vi.clearAllMocks());

describe("IndexersPanel", () => {
  it("lists configured indexers with status", async () => {
    vi.mocked(api.listIndexers).mockResolvedValue([
      {
        id: "1",
        name: "prowlarr",
        base_url: "http://localhost:9696",
        enabled: true,
        has_api_key: true,
        reachable: true,
        trackers: ["1337x", "RARBG"],
      },
    ]);
    render(wrap(<IndexersPanel />));
    expect(await screen.findByText("prowlarr")).toBeInTheDocument();
    expect(screen.getByText(/2 trackers/)).toBeInTheDocument();
  });

  it("creates an indexer", async () => {
    vi.mocked(api.listIndexers).mockResolvedValue([]);
    vi.mocked(api.createIndexer).mockResolvedValue({
      id: "2",
      name: "p",
      enabled: true,
      has_api_key: true,
      reachable: false,
    });
    const user = userEvent.setup();
    render(wrap(<IndexersPanel />));

    await screen.findByText(/no indexers added yet|aún no has añadido/i);
    await user.type(screen.getByLabelText(/name|nombre/i), "p");
    await user.type(screen.getByLabelText(/base url|url base/i), "http://localhost:9696");
    await user.click(screen.getByRole("button", { name: /add|añadir/i }));

    await waitFor(() => {
      expect(api.createIndexer).toHaveBeenCalledWith(
        expect.objectContaining({ name: "p", base_url: "http://localhost:9696", enabled: true }),
      );
    });
  });

  it("tests a connection", async () => {
    vi.mocked(api.listIndexers).mockResolvedValue([]);
    vi.mocked(api.testIndexer).mockResolvedValue({ ok: true });
    const user = userEvent.setup();
    render(wrap(<IndexersPanel />));

    await screen.findByText(/no indexers added yet|aún no has añadido/i);
    await user.type(screen.getByLabelText(/base url|url base/i), "http://localhost:9696");
    await user.click(screen.getByRole("button", { name: /test connection|probar conexión/i }));

    await waitFor(() => {
      expect(screen.getByText(/connection ok|conexión correcta/i)).toBeInTheDocument();
    });
  });
});
