import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import "@/i18n";
import { FolderBrowser } from "./FolderBrowser";
import { api } from "@/api/client";

vi.mock("@/api/client", () => ({
  api: {
    browseUploadFolders: vi.fn(),
    createUploadFolder: vi.fn(),
  },
}));

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

const LIBS = [
  {
    id: "lib-mov",
    name: "Movies",
    content_type: "movies",
    paths: ["/data/movies"],
    settings: {},
    scan_mode: "auto",
    refresh_interval: "",
    language_filter: "",
    tls_insecure: false,
    m3u_url: "",
    epg_url: "",
    item_count: 0,
    scan_status: "idle",
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
  },
] as unknown as import("@/api/types").Library[];

beforeEach(() => {
  vi.clearAllMocks();
});

describe("FolderBrowser", () => {
  it("muestra la lista de subdirs y permite navegar", async () => {
    const user = userEvent.setup();
    vi.mocked(api.browseUploadFolders).mockResolvedValue({
      library_id: "lib-mov",
      library_name: "Movies",
      path: "",
      directories: [
        { name: "Action", path: "Action" },
        { name: "Drama", path: "Drama" },
      ],
    });

    const onChange = vi.fn();
    render(
      wrap(
        <FolderBrowser
          libraries={LIBS}
          libraryID="lib-mov"
          path=""
          onChange={onChange}
        />,
      ),
    );

    expect(await screen.findByText("Action")).toBeInTheDocument();
    expect(screen.getByText("Drama")).toBeInTheDocument();

    await user.click(screen.getByText("Drama"));
    expect(onChange).toHaveBeenCalledWith("lib-mov", "Drama");
  });

  it("muestra breadcrumb cuando hay path", async () => {
    vi.mocked(api.browseUploadFolders).mockResolvedValue({
      library_id: "lib-mov",
      library_name: "Movies",
      path: "Movies/Drama",
      directories: [],
    });

    render(
      wrap(
        <FolderBrowser
          libraries={LIBS}
          libraryID="lib-mov"
          path="Movies/Drama"
          onChange={vi.fn()}
        />,
      ),
    );

    await waitFor(() => expect(api.browseUploadFolders).toHaveBeenCalled());
    // Los segmentos del path aparecen como botones del breadcrumb.
    expect(screen.getByRole("button", { name: "Movies" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Drama" })).toBeInTheDocument();
  });

  it("navega un nivel arriba con el botón", async () => {
    const user = userEvent.setup();
    vi.mocked(api.browseUploadFolders).mockResolvedValue({
      library_id: "lib-mov",
      library_name: "Movies",
      path: "Movies/Drama",
      directories: [],
    });

    const onChange = vi.fn();
    render(
      wrap(
        <FolderBrowser
          libraries={LIBS}
          libraryID="lib-mov"
          path="Movies/Drama"
          onChange={onChange}
        />,
      ),
    );

    await screen.findByRole("button", { name: /subir un nivel|up one level/i });
    await user.click(
      screen.getByRole("button", { name: /subir un nivel|up one level/i }),
    );
    expect(onChange).toHaveBeenCalledWith("lib-mov", "Movies");
  });

  it("crea una carpeta nueva y navega a ella", async () => {
    const user = userEvent.setup();
    vi.mocked(api.browseUploadFolders).mockResolvedValue({
      library_id: "lib-mov",
      library_name: "Movies",
      path: "",
      directories: [],
    });
    vi.mocked(api.createUploadFolder).mockResolvedValue();

    const onChange = vi.fn();
    render(
      wrap(
        <FolderBrowser
          libraries={LIBS}
          libraryID="lib-mov"
          path=""
          onChange={onChange}
        />,
      ),
    );

    await waitFor(() => expect(api.browseUploadFolders).toHaveBeenCalled());
    // Click "Nueva carpeta" expande el form.
    await user.click(
      screen.getByRole("button", { name: /nueva carpeta|new folder/i }),
    );
    await user.type(
      screen.getByPlaceholderText(/nombre de la carpeta|folder name/i),
      "Sci-Fi",
    );
    await user.click(screen.getByRole("button", { name: /^crear$|^create$/i }));

    await waitFor(() => {
      expect(api.createUploadFolder).toHaveBeenCalledWith("lib-mov", "Sci-Fi");
    });
    // Navega a la carpeta recién creada.
    expect(onChange).toHaveBeenCalledWith("lib-mov", "Sci-Fi");
  });

  it("muestra error inline si el backend rechaza la carpeta", async () => {
    const user = userEvent.setup();
    vi.mocked(api.browseUploadFolders).mockResolvedValue({
      library_id: "lib-mov",
      library_name: "Movies",
      path: "",
      directories: [],
    });
    vi.mocked(api.createUploadFolder).mockRejectedValue(
      new Error("upload subpath is invalid"),
    );

    render(
      wrap(
        <FolderBrowser
          libraries={LIBS}
          libraryID="lib-mov"
          path=""
          onChange={vi.fn()}
        />,
      ),
    );

    await waitFor(() => expect(api.browseUploadFolders).toHaveBeenCalled());
    await user.click(
      screen.getByRole("button", { name: /nueva carpeta|new folder/i }),
    );
    await user.type(
      screen.getByPlaceholderText(/nombre de la carpeta|folder name/i),
      "..",
    );
    await user.click(screen.getByRole("button", { name: /^crear$|^create$/i }));

    expect(
      await screen.findByText(/upload subpath is invalid/i),
    ).toBeInTheDocument();
  });
});
