// AdminChannelOrderPanel — vitest spec. Locks the admin curation
// wiring: GET admin-view feeds the editor, save POSTs the full
// reordered + hidden set to /libraries/{id}/channels/order, reset
// hits the DELETE.

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

    // Move A down (swap A and B). Click the down arrow on the first row.
    fireEvent.click(within(rows[0]).getByRole("button", { name: /bajar|move down/i }));

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
