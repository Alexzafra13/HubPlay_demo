import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

// Mock del cliente y del SSE para que el bell no abra una conexion
// real en jsdom (que no soporta EventSource bien).
vi.mock("@/api/client", () => ({
  api: {
    listMyNotifications: vi.fn(),
    markNotificationRead: vi.fn(),
    markAllNotificationsRead: vi.fn(),
  },
}));

vi.mock("@/hooks/useUserEventStream", () => ({
  useUserEventStream: vi.fn(),
}));

import "@/i18n";
import { api } from "@/api/client";
import { NotificationsBell } from "./NotificationsBell";
import type { NotificationsResponse } from "@/api/types";

const apiMock = api as unknown as {
  listMyNotifications: ReturnType<typeof vi.fn>;
};

function wrap() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <NotificationsBell />
      </QueryClientProvider>
    </MemoryRouter>
  );
}

beforeEach(() => {
  apiMock.listMyNotifications.mockReset();
});

describe("NotificationsBell", () => {
  // "Si no hay no aparezca nada" — el cliente lo pidio explicito.
  // Cuando el inbox esta vacio y unread = 0 no renderizamos nada.
  it("renderiza nada cuando no hay notificaciones (ni leidas ni unread)", async () => {
    const empty: NotificationsResponse = { data: [], unread_count: 0 };
    apiMock.listMyNotifications.mockResolvedValue(empty);
    const { container } = render(wrap());
    // Esperamos a que la query resuelva — el componente devuelve
    // null mientras isLoading sin data previa.
    await waitFor(() => {
      expect(container.firstChild).toBeNull();
    });
  });

  // Con unread > 0 hay badge visible. El aria-label debe incluir
  // el count para que screen readers lo anuncien.
  it("muestra badge + aria-label con count cuando hay no leidas", async () => {
    const resp: NotificationsResponse = {
      data: [
        {
          id: "n1",
          kind: "federation.pairing_request_received",
          title: "Nueva petición",
          created_at: new Date().toISOString(),
        },
      ],
      unread_count: 1,
    };
    apiMock.listMyNotifications.mockResolvedValue(resp);
    render(wrap());
    const btn = await screen.findByRole("button", {
      name: /1 unread|1 sin leer/i,
    });
    expect(btn).toBeTruthy();
    expect(btn.textContent).toContain("1");
  });

  // Click abre el dropdown con role=menu.
  it("abre el dropdown al click", async () => {
    const resp: NotificationsResponse = {
      data: [
        {
          id: "n1",
          kind: "federation.pairing_request_received",
          title: "Hola",
          body: "Cuerpo",
          created_at: new Date().toISOString(),
        },
      ],
      unread_count: 1,
    };
    apiMock.listMyNotifications.mockResolvedValue(resp);
    render(wrap());
    const btn = await screen.findByRole("button", {
      name: /1 unread|1 sin leer/i,
    });
    fireEvent.click(btn);
    const menu = await screen.findByRole("menu");
    expect(menu).toBeTruthy();
    expect(menu.textContent).toContain("Hola");
  });

  // Si hay notifs leidas pero ninguna unread, el bell sigue visible
  // para que el user pueda consultar el historial. Solo cuando esta
  // todo vacio se oculta entero.
  it("mantiene visible el bell aunque unread=0 si hay historico", async () => {
    const resp: NotificationsResponse = {
      data: [
        {
          id: "n1",
          kind: "federation.pairing_request_accepted",
          title: "Ya emparejado",
          read_at: new Date().toISOString(),
          created_at: new Date().toISOString(),
        },
      ],
      unread_count: 0,
    };
    apiMock.listMyNotifications.mockResolvedValue(resp);
    render(wrap());
    const btn = await screen.findByRole("button", {
      name: /notificaciones|notifications/i,
    });
    // Sin badge porque unread=0, pero presente.
    expect(btn).toBeTruthy();
  });
});
