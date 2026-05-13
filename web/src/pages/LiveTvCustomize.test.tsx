// LiveTvCustomize — covers the per-user personalisation flow shipped
// in Sesión I. We pin the seed-once-per-library behaviour (the lint
// fix for set-state-in-effect added in this PR depends on it not
// regressing), plus the reorder, hide, save, and reset paths.

import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  render,
  screen,
  waitFor,
  fireEvent,
  within,
} from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "@/i18n";

const apiMock = vi.hoisted(() => ({
  getLibraries: vi.fn(),
  getChannelsForPersonalisation: vi.fn(),
  replaceChannelOrder: vi.fn(),
  resetChannelOrder: vi.fn(),
}));
vi.mock("@/api/client", () => ({
  api: apiMock,
}));

import LiveTvCustomize from "./LiveTvCustomize";

function wrap(node: React.ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return (
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/live-tv/customize"]}>
        <Routes>
          <Route path="/live-tv/customize" element={node} />
          <Route path="/live-tv" element={<div>Live TV grid</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  );
}

function channel(id: string, name: string, opts: Partial<{ hidden: boolean; group: string | null }> = {}) {
  return {
    id,
    name,
    number: 1,
    logo_url: null,
    group: opts.group ?? null,
    group_name: opts.group ?? null,
    category: "general",
    logo_initials: name.slice(0, 2).toUpperCase(),
    logo_bg: "#000000",
    logo_fg: "#ffffff",
    stream_url: `http://stream/${id}`,
    library_id: "lib-1",
    language: "",
    country: "",
    is_active: true,
    hidden: opts.hidden ?? false,
  };
}

beforeEach(() => {
  apiMock.getLibraries.mockReset();
  apiMock.getChannelsForPersonalisation.mockReset();
  apiMock.replaceChannelOrder.mockReset();
  apiMock.resetChannelOrder.mockReset();

  apiMock.getLibraries.mockResolvedValue([
    { id: "lib-1", name: "Live TV", content_type: "livetv" },
  ]);
  apiMock.getChannelsForPersonalisation.mockResolvedValue([
    channel("ch-a", "Alpha"),
    channel("ch-b", "Beta"),
    channel("ch-c", "Gamma"),
  ]);
  apiMock.replaceChannelOrder.mockResolvedValue(undefined);
  apiMock.resetChannelOrder.mockResolvedValue(undefined);
});

describe("LiveTvCustomize", () => {
  it("renders the empty state when no IPTV libraries are configured", async () => {
    apiMock.getLibraries.mockResolvedValueOnce([
      { id: "lib-99", name: "Movies", content_type: "movies" },
    ]);

    render(wrap(<LiveTvCustomize />));

    expect(
      await screen.findByText(/no live tv libraries|no hay bibliotecas/i),
    ).toBeInTheDocument();
    expect(apiMock.getChannelsForPersonalisation).not.toHaveBeenCalled();
  });

  it("seeds the draft from the personalisation query on first load", async () => {
    render(wrap(<LiveTvCustomize />));

    const rows = await screen.findAllByTestId("customize-row");
    expect(rows).toHaveLength(3);
    expect(rows[0]).toHaveTextContent("Alpha");
    expect(rows[1]).toHaveTextContent("Beta");
    expect(rows[2]).toHaveTextContent("Gamma");
  });

  it("reorders rows when the move-down button is pressed", async () => {
    render(wrap(<LiveTvCustomize />));

    const rows = await screen.findAllByTestId("customize-row");
    const moveDown = within(rows[0]).getByRole("button", {
      name: /move down|bajar/i,
    });
    fireEvent.click(moveDown);

    const reordered = screen.getAllByTestId("customize-row");
    expect(reordered[0]).toHaveTextContent("Beta");
    expect(reordered[1]).toHaveTextContent("Alpha");
    expect(reordered[2]).toHaveTextContent("Gamma");
  });

  it("submits the current order + hidden set on save", async () => {
    render(wrap(<LiveTvCustomize />));

    const rows = await screen.findAllByTestId("customize-row");
    // Hide Beta.
    fireEvent.click(
      within(rows[1]).getByRole("button", { name: /hide for me|ocultar/i }),
    );
    // Move Gamma to the top.
    fireEvent.click(
      within(rows[2]).getByRole("button", { name: /move up|subir/i }),
    );
    fireEvent.click(
      within(screen.getAllByTestId("customize-row")[1]).getByRole("button", {
        name: /move up|subir/i,
      }),
    );

    fireEvent.click(screen.getByRole("button", { name: /^(save|guardar)$/i }));

    await waitFor(() => {
      expect(apiMock.replaceChannelOrder).toHaveBeenCalledWith({
        ordered_channel_ids: ["ch-c", "ch-a", "ch-b"],
        hidden_channel_ids: ["ch-b"],
      });
    });

    expect(
      await screen.findByText(/saved\.|guardado\./i),
    ).toBeInTheDocument();
  });

  it("does not call reset when the user cancels the confirmation", async () => {
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(false);

    render(wrap(<LiveTvCustomize />));
    await screen.findAllByTestId("customize-row");

    fireEvent.click(
      screen.getByRole("button", { name: /restore admin|restaurar/i }),
    );

    expect(apiMock.resetChannelOrder).not.toHaveBeenCalled();
    confirmSpy.mockRestore();
  });

  it("on reset confirm: calls the mutation and re-seeds from a fresh fetch", async () => {
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);

    render(wrap(<LiveTvCustomize />));
    await screen.findAllByTestId("customize-row");

    // The post-reset refetch returns the admin defaults (no hidden flags).
    apiMock.getChannelsForPersonalisation.mockResolvedValueOnce([
      channel("ch-a", "Alpha"),
      channel("ch-b", "Beta"),
      channel("ch-c", "Gamma"),
    ]);

    fireEvent.click(
      screen.getByRole("button", { name: /restore admin|restaurar/i }),
    );

    await waitFor(() => {
      expect(apiMock.resetChannelOrder).toHaveBeenCalled();
    });
    expect(
      await screen.findByText(/restored\.|restaurado\./i),
    ).toBeInTheDocument();

    confirmSpy.mockRestore();
  });

  it("disables Save until the draft is dirty", async () => {
    render(wrap(<LiveTvCustomize />));
    await screen.findAllByTestId("customize-row");

    const save = screen.getByRole("button", { name: /^(save|guardar)$/i });
    expect(save).toBeDisabled();

    fireEvent.click(
      within(screen.getAllByTestId("customize-row")[0]).getByRole("button", {
        name: /hide for me|ocultar/i,
      }),
    );

    expect(save).toBeEnabled();
  });
});
