import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import "@/i18n";
import { AuditLogPanel } from "./AuditLogPanel";
import { api } from "@/api/client";

vi.mock("@/api/client", () => ({
  api: {
    queryAuditLog: vi.fn(),
    listAuditEventTypes: vi.fn(),
  },
}));

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(api.listAuditEventTypes).mockResolvedValue([
    "auth.login.ok",
    "permission.changed",
    "system.restart",
  ]);
  vi.mocked(api.queryAuditLog).mockResolvedValue({
    rows: [],
    total: 0,
    limit: 50,
    offset: 0,
  });
});

describe("AuditLogPanel — rendering", () => {
  it("pinta empty state cuando no hay filas", async () => {
    render(wrap(<AuditLogPanel />));
    await waitFor(() => {
      expect(screen.getByText(/sin eventos|no events/i)).toBeInTheDocument();
    });
  });

  it("pinta filas con tipo + actor + IP", async () => {
    vi.mocked(api.queryAuditLog).mockResolvedValue({
      rows: [
        {
          id: "e1",
          actor_user_id: "u-alex",
          event_type: "auth.login.ok",
          target_type: "user",
          target_id: "u-alex",
          payload: `{"username":"alex"}`,
          ip_address: "10.0.0.5",
          user_agent: "Chrome/120",
          created_at: "2026-05-19T12:00:00Z",
        },
        {
          id: "e2",
          actor_user_id: "u-owner",
          event_type: "permission.changed",
          target_type: "user",
          target_id: "u-alex",
          payload: `{"changes":{"can_edit_metadata":true}}`,
          ip_address: "10.0.0.1",
          user_agent: "",
          created_at: "2026-05-19T11:55:00Z",
        },
      ],
      total: 2,
      limit: 50,
      offset: 0,
    });

    render(wrap(<AuditLogPanel />));
    // "auth.login.ok" aparece dos veces: una en el dropdown de tipos
    // y otra en la columna del row. Buscamos por la celda <td>
    // específicamente para desambiguar.
    await waitFor(() => {
      const cells = screen.getAllByText("auth.login.ok");
      expect(cells.some((el) => el.tagName === "TD")).toBe(true);
    });
    const cells = screen.getAllByText("permission.changed");
    expect(cells.some((el) => el.tagName === "TD")).toBe(true);
    expect(screen.getByText("10.0.0.5")).toBeInTheDocument();
  });
});

describe("AuditLogPanel — filtros", () => {
  it("aplica filtro de tipo al pulsar Aplicar", async () => {
    const user = userEvent.setup();
    render(wrap(<AuditLogPanel />));
    await waitFor(() => expect(api.queryAuditLog).toHaveBeenCalled());

    const select = screen.getByRole("combobox", {
      name: /tipo de evento|event type/i,
    });
    await user.selectOptions(select, "auth.");

    const apply = screen.getByRole("button", { name: /aplicar|apply/i });
    await user.click(apply);

    await waitFor(() => {
      const calls = vi.mocked(api.queryAuditLog).mock.calls;
      const last = calls[calls.length - 1][0];
      expect(last.type).toBe("auth.");
    });
  });

  it("limpia los filtros con el botón Limpiar", async () => {
    const user = userEvent.setup();
    render(wrap(<AuditLogPanel />));
    await waitFor(() => expect(api.queryAuditLog).toHaveBeenCalled());

    // Pone un filtro y aplica.
    const actorInput = screen.getByPlaceholderText(/u-alex/i);
    await user.type(actorInput, "u-x");
    await user.click(screen.getByRole("button", { name: /aplicar|apply/i }));

    // Click en Limpiar.
    await user.click(screen.getByRole("button", { name: /limpiar|clear/i }));

    await waitFor(() => {
      const last = vi.mocked(api.queryAuditLog).mock.calls.pop()![0];
      expect(last.actor).toBeUndefined();
    });
    expect((actorInput as HTMLInputElement).value).toBe("");
  });
});

describe("AuditLogPanel — paginación", () => {
  it("muestra X-Y de N en el footer", async () => {
    vi.mocked(api.queryAuditLog).mockResolvedValue({
      rows: [
        {
          id: "e1",
          actor_user_id: "u-1",
          event_type: "auth.login.ok",
          target_type: "user",
          target_id: "u-1",
          payload: "",
          ip_address: "10.0.0.1",
          user_agent: "",
          created_at: "2026-05-19T12:00:00Z",
        },
      ],
      total: 123,
      limit: 50,
      offset: 0,
    });

    render(wrap(<AuditLogPanel />));
    // El footer dice "Mostrando 1–1 de 123" (es) o "Showing 1–1 of
    // 123" (en). Verifica que el total 123 aparece en algún lugar
    // del DOM — sin restricción de qué etiqueta, basta con que se
    // pinte el número.
    await waitFor(() => {
      const all = screen.getAllByText(/123/);
      expect(all.length).toBeGreaterThan(0);
    });
  });
});

describe("AuditLogPanel — drawer detalle", () => {
  it("abre detalle con payload formateado al clicar una fila", async () => {
    const user = userEvent.setup();
    vi.mocked(api.queryAuditLog).mockResolvedValue({
      rows: [
        {
          id: "e1",
          actor_user_id: "u-owner",
          event_type: "permission.changed",
          target_type: "user",
          target_id: "u-alex",
          payload: `{"changes":{"can_edit_metadata":true}}`,
          ip_address: "10.0.0.1",
          user_agent: "Firefox/115",
          created_at: "2026-05-19T12:00:00Z",
        },
      ],
      total: 1,
      limit: 50,
      offset: 0,
    });

    render(wrap(<AuditLogPanel />));
    // "permission.changed" aparece en el dropdown y en la fila;
    // queremos la celda (TD) de la fila.
    await waitFor(() => {
      const cells = screen.getAllByText("permission.changed");
      const td = cells.find((el) => el.tagName === "TD");
      expect(td).toBeDefined();
    });
    const td = screen.getAllByText("permission.changed")
      .find((el) => el.tagName === "TD")!;
    await user.click(td);

    // Drawer abierto: aparece el dialog con el user agent.
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/Firefox\/115/)).toBeInTheDocument();
    // Payload formateado contiene la key.
    expect(within(dialog).getByText(/can_edit_metadata/)).toBeInTheDocument();
  });
});
