// DevicesPanel — covers the bits that have caused real bugs in the
// past or are likely to: the "Este dispositivo" pill on the
// caller's own session, the confirm prompt before revoking it, and
// the empty state when no sessions are returned.
//
// Network calls are mocked at the api boundary; the EventSource-
// flavoured "live update" hook isn't relevant here (revoke just
// invalidates the query and waits for the refetch).

import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  render,
  screen,
  waitFor,
  fireEvent,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "@/i18n";

const apiMock = vi.hoisted(() => ({
  listMySessions: vi.fn(),
  revokeMySession: vi.fn(),
}));
vi.mock("@/api/client", () => ({
  api: apiMock,
}));

import { DevicesPanel } from "./DevicesPanel";
import type { MySession } from "@/api/types";

function wrap(node: React.ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

const fixture: MySession[] = [
  {
    id: "sess-1",
    device_name: "Chrome on Linux",
    device_id: "dev-1",
    ip_address: "192.0.2.10",
    created_at: "2026-05-10T08:00:00Z",
    last_active_at: new Date(Date.now() - 2 * 60_000).toISOString(),
    expires_at: "2026-06-10T08:00:00Z",
    current: true,
  },
  {
    id: "sess-2",
    device_name: "iPhone 15",
    device_id: "dev-2",
    ip_address: "203.0.113.4",
    created_at: "2026-05-09T08:00:00Z",
    last_active_at: new Date(Date.now() - 60 * 60_000).toISOString(),
    expires_at: "2026-06-09T08:00:00Z",
    current: false,
  },
];

beforeEach(() => {
  apiMock.listMySessions.mockReset();
  apiMock.revokeMySession.mockReset();
});

describe("DevicesPanel", () => {
  it("renders one row per session and marks the current device", async () => {
    apiMock.listMySessions.mockResolvedValueOnce(fixture);
    render(wrap(<DevicesPanel />));

    await waitFor(() =>
      expect(screen.getByText("Chrome on Linux")).toBeInTheDocument(),
    );
    expect(screen.getByText("iPhone 15")).toBeInTheDocument();
    // The "Este dispositivo" pill should appear ONCE — only on the
    // session whose `current` flag is true.
    expect(screen.getAllByText(/este dispositivo/i)).toHaveLength(1);
  });

  it("renders the empty-state copy when the server returns no sessions", async () => {
    apiMock.listMySessions.mockResolvedValueOnce([]);
    render(wrap(<DevicesPanel />));
    await waitFor(() =>
      expect(
        screen.getByText(/no hay sesiones activas/i),
      ).toBeInTheDocument(),
    );
  });

  it("revokes a non-current session without confirm prompt", async () => {
    apiMock.listMySessions.mockResolvedValue(fixture);
    apiMock.revokeMySession.mockResolvedValueOnce(undefined);
    const confirmSpy = vi.spyOn(window, "confirm");

    render(wrap(<DevicesPanel />));
    await waitFor(() =>
      expect(screen.getByText("iPhone 15")).toBeInTheDocument(),
    );

    // The two rows render the same "Cerrar sesión" label; pick the
    // one inside the iPhone row by walking up from its name.
    const iphoneRow = screen.getByText("iPhone 15").closest("li");
    expect(iphoneRow).not.toBeNull();
    fireEvent.click(
      within(iphoneRow!).getByRole("button", { name: /cerrar sesión/i }),
    );

    await waitFor(() =>
      expect(apiMock.revokeMySession).toHaveBeenCalledWith("sess-2"),
    );
    expect(confirmSpy).not.toHaveBeenCalled();
    confirmSpy.mockRestore();
  });

  it("confirms before revoking the current session and bails if the user cancels", async () => {
    apiMock.listMySessions.mockResolvedValue(fixture);
    const confirmSpy = vi
      .spyOn(window, "confirm")
      .mockReturnValueOnce(false);

    render(wrap(<DevicesPanel />));
    await waitFor(() =>
      expect(screen.getByText("Chrome on Linux")).toBeInTheDocument(),
    );

    const currentRow = screen.getByText("Chrome on Linux").closest("li");
    fireEvent.click(
      within(currentRow!).getByRole("button", { name: /cerrar sesión/i }),
    );

    expect(confirmSpy).toHaveBeenCalledOnce();
    expect(apiMock.revokeMySession).not.toHaveBeenCalled();
    confirmSpy.mockRestore();
  });

  it("revokes the current session when the user accepts the confirm", async () => {
    apiMock.listMySessions.mockResolvedValue(fixture);
    apiMock.revokeMySession.mockResolvedValueOnce(undefined);
    const confirmSpy = vi
      .spyOn(window, "confirm")
      .mockReturnValueOnce(true);

    render(wrap(<DevicesPanel />));
    await waitFor(() =>
      expect(screen.getByText("Chrome on Linux")).toBeInTheDocument(),
    );

    const currentRow = screen.getByText("Chrome on Linux").closest("li");
    fireEvent.click(
      within(currentRow!).getByRole("button", { name: /cerrar sesión/i }),
    );

    await waitFor(() =>
      expect(apiMock.revokeMySession).toHaveBeenCalledWith("sess-1"),
    );
    confirmSpy.mockRestore();
  });
});

// Helper to scope queries inside an LI without pulling in extra
// imports at the top.
import { within } from "@testing-library/react";
