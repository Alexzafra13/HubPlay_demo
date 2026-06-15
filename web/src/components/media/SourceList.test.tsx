import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import "@/i18n";
import { SourceList } from "./SourceList";
import { api } from "@/api/client";
import { ApiError } from "@/api/types";

vi.mock("@/api/client", () => ({
  api: {
    getMediaSources: vi.fn(),
    torrentStreamURL: (src: string) => `/torrent/stream?src=${encodeURIComponent(src)}`,
  },
}));

function wrap(ui: React.ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>;
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("SourceList", () => {
  it("renders sorted sources with quality, size and seeders", async () => {
    vi.mocked(api.getMediaSources).mockResolvedValue([
      {
        identifier: "abc",
        title: "The Matrix 2160p UHD",
        torrent_url: "magnet:?xt=urn:btih:abc",
        magnet_uri: "magnet:?xt=urn:btih:abc",
        quality: "4K",
        seeders: 120,
        size_bytes: 8_000_000_000,
        infohash: "abc",
      },
    ]);

    render(wrap(<SourceList type="movie" imdbId="tt0133093" />));

    expect(await screen.findByText("The Matrix 2160p UHD")).toBeInTheDocument();
    expect(screen.getByText("4K")).toBeInTheDocument();
    expect(screen.getByText("8.00 GB")).toBeInTheDocument();
    expect(screen.getByText(/120 seeders/)).toBeInTheDocument();
    expect(api.getMediaSources).toHaveBeenCalledWith("movie", "tt0133093");
  });

  it("delegates playback to onPlay when provided", async () => {
    const onPlay = vi.fn();
    const source = {
      identifier: "abc",
      title: "Show S01",
      torrent_url: "magnet:?xt=urn:btih:abc",
      magnet_uri: "magnet:?xt=urn:btih:abc",
      seeders: 5,
    };
    vi.mocked(api.getMediaSources).mockResolvedValue([source]);

    const user = userEvent.setup();
    render(wrap(<SourceList type="series" imdbId="tt0903747" onPlay={onPlay} />));

    await user.click(await screen.findByRole("button", { name: /play|reproducir/i }));
    expect(onPlay).toHaveBeenCalledWith(source);
  });

  it("shows a disabled state when the indexer feature is off", async () => {
    vi.mocked(api.getMediaSources).mockRejectedValue(
      new ApiError(503, { error: { code: "INDEXER_DISABLED", message: "off" } }),
    );

    render(wrap(<SourceList type="movie" imdbId="tt0133093" />));

    await waitFor(() => {
      expect(screen.getByText(/no indexers|sin indexadores/i)).toBeInTheDocument();
    });
  });

  it("shows empty state when no sources are returned", async () => {
    vi.mocked(api.getMediaSources).mockResolvedValue([]);

    render(wrap(<SourceList type="movie" imdbId="tt0133093" />));

    await waitFor(() => {
      expect(
        screen.getByText(/no sources found|no se encontraron fuentes/i),
      ).toBeInTheDocument();
    });
  });
});
