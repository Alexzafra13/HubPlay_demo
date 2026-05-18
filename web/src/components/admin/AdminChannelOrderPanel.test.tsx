// AdminChannelOrderPanel — vitest spec. Locks the admin curation
// wiring: GET admin-view feeds the editor, save POSTs the full
// reordered + hidden set to /libraries/{id}/channels/order, reset
// hits the DELETE.
//
// Reordering in v2 is driven by drag & drop (HTML5 + pointer + keyboard).
// Drag events are unreliable to simulate in jsdom, so we exercise the
// equivalent intent via the position-jump input and bulk-hide bar —
// they hit the same onReorder / onBulkSetHidden code paths the drag
// handlers call.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import "@/i18n";

const apiMock = vi.hoisted(() => ({
  getChannelsForLibraryAdmin: vi.fn(),
  replaceLibraryChannelOrder: vi.fn(),
  resetLibraryChannelOrder: vi.fn(),
}));
vi.mock("@/api/client", () => ({ api: apiMock }));

import { AdminChannelOrderPanel } from "./AdminChannelOrderPanel";

function wrap(node: React.ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return (
    <QueryClientProvider client={client}>
      <MemoryRouter>{node}</MemoryRouter>
    </QueryClientProvider>
  );
}

beforeEach(() => {
  apiMock.getChannelsForLibraryAdmin.mockReset();
  apiMock.replaceLibraryChannelOrder.mockReset();
  apiMock.resetLibraryChannelOrder.mockReset();
  apiMock.replaceLibraryChannelOrder.mockResolvedValue(undefined);
  apiMock.resetLibraryChannelOrder.mockResolvedValue(undefined);
});

describe("AdminChannelOrderPanel", () => {
  it("renders the channel list with admin scope hint and saves the reordered set", async () => {
    apiMock.getChannelsForLibraryAdmin.mockResolvedValue([
      { id: "ch-a", name: "Canal A", group_name: "News", hidden: false },
      { id: "ch-b", name: "Canal B", group_name: "News", hidden: false },
      { id: "ch-c", name: "Canal C", group_name: null, hidden: true },
    ]);

    render(wrap(<AdminChannelOrderPanel libraryId="lib-1" />));

    // Scope hint surfaces — admin must understand they're editing
    // the GLOBAL default, not their personal view.
    await waitFor(() => {
      expect(screen.getByText(/orden por defecto que ven todos|default order/i)).toBeInTheDocument();
    });

    const rows = await screen.findAllByTestId("customize-row");
    expect(rows).toHaveLength(3);

    // Move "Canal A" from position 1 to position 2 via the position-jump
    // input — equivalent to onReorder(0, 1), which is also what a
    // drag from row 0 to row 1 triggers.
    const posBtn = within(rows[0]).getByRole("button", {
      name: /cambiar posición de canal a|change position of canal a/i,
    });
    fireEvent.click(posBtn);
    const posInput = within(rows[0]).getByRole("spinbutton", {
      name: /mover a posición|move to position/i,
    });
    fireEvent.change(posInput, { target: { value: "2" } });
    fireEvent.keyDown(posInput, { key: "Enter" });

    // Save.
    fireEvent.click(screen.getByRole("button", { name: /guardar orden|save order/i }));

    await waitFor(() => {
      expect(apiMock.replaceLibraryChannelOrder).toHaveBeenCalledTimes(1);
    });
    const [libId, payload] = apiMock.replaceLibraryChannelOrder.mock.calls[0];
    expect(libId).toBe("lib-1");
    expect(payload.ordered_channel_ids).toEqual(["ch-b", "ch-a", "ch-c"]);
    // Channel C was admin-hidden on load and remains hidden on save.
    expect(payload.hidden_channel_ids).toContain("ch-c");
  });

  it("bulk-hides every selected channel in one save", async () => {
    apiMock.getChannelsForLibraryAdmin.mockResolvedValue([
      { id: "ch-a", name: "Canal A", group_name: "News", hidden: false },
      { id: "ch-b", name: "Canal B", group_name: "News", hidden: false },
      { id: "ch-c", name: "Canal C", group_name: null, hidden: false },
    ]);

    render(wrap(<AdminChannelOrderPanel libraryId="lib-1" />));

    const rows = await screen.findAllByTestId("customize-row");

    // Select the first two channels.
    fireEvent.click(
      within(rows[0]).getByRole("checkbox", { name: /seleccionar canal a|select canal a/i }),
    );
    fireEvent.click(
      within(rows[1]).getByRole("checkbox", { name: /seleccionar canal b|select canal b/i }),
    );

    // Bulk-hide action bar appears with a single "hide" button.
    const bulkRegion = screen.getByRole("region", {
      name: /acciones en bloque|bulk actions/i,
    });
    fireEvent.click(
      within(bulkRegion).getByRole("button", { name: /^ocultar$|^hide$/i }),
    );

    fireEvent.click(screen.getByRole("button", { name: /guardar orden|save order/i }));

    await waitFor(() => {
      expect(apiMock.replaceLibraryChannelOrder).toHaveBeenCalledTimes(1);
    });
    const [, payload] = apiMock.replaceLibraryChannelOrder.mock.calls[0];
    expect(payload.hidden_channel_ids.sort()).toEqual(["ch-a", "ch-b"]);
  });

  it("filters the list by name while keeping the underlying order intact", async () => {
    apiMock.getChannelsForLibraryAdmin.mockResolvedValue([
      { id: "ch-a", name: "ESPN", group_name: "Sports", hidden: false },
      { id: "ch-b", name: "CNN", group_name: "News", hidden: false },
      { id: "ch-c", name: "Fox Sports", group_name: "Sports", hidden: false },
    ]);

    render(wrap(<AdminChannelOrderPanel libraryId="lib-1" />));

    await screen.findAllByTestId("customize-row");

    const search = screen.getByRole("searchbox", {
      name: /buscar canal o grupo|search channel or group/i,
    });
    fireEvent.change(search, { target: { value: "sport" } });

    const filteredRows = screen.getAllByTestId("customize-row");
    expect(filteredRows).toHaveLength(2);
    expect(within(filteredRows[0]).getByText("ESPN")).toBeInTheDocument();
    expect(within(filteredRows[1]).getByText("Fox Sports")).toBeInTheDocument();
  });

  it("reset confirms then DELETEs the overlay", async () => {
    apiMock.getChannelsForLibraryAdmin.mockResolvedValue([
      { id: "ch-a", name: "A", group_name: null, hidden: false },
    ]);
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);

    render(wrap(<AdminChannelOrderPanel libraryId="lib-1" />));
    await screen.findAllByTestId("customize-row");

    fireEvent.click(screen.getByRole("button", { name: /restaurar orden del m3u|restore m3u order/i }));

    await waitFor(() => {
      expect(apiMock.resetLibraryChannelOrder).toHaveBeenCalledWith("lib-1");
    });

    confirmSpy.mockRestore();
  });
});
