import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import "@/i18n";
import { CorsOriginsPanel } from "./CorsOriginsPanel";
import { api } from "@/api/client";

vi.mock("@/api/client", () => ({
  api: {
    listCorsOrigins: vi.fn(),
    addCorsOrigin: vi.fn(),
    deleteCorsOrigin: vi.fn(),
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

describe("CorsOriginsPanel — load + render", () => {
  it("muestra statics como read-only y dynamics con metadata", async () => {
    vi.mocked(api.listCorsOrigins).mockResolvedValue({
      statics: ["https://static.example.com", "http://localhost:5173"],
      dynamics: [
        {
          origin: "https://added.example.com",
          created_by: "u-1",
          created_at: "2026-05-19T12:00:00Z",
          note: "frontend prod",
        },
      ],
    });

    render(wrap(<CorsOriginsPanel />));

    expect(await screen.findByText("https://static.example.com")).toBeInTheDocument();
    expect(screen.getByText("http://localhost:5173")).toBeInTheDocument();
    expect(screen.getByText("https://added.example.com")).toBeInTheDocument();
    expect(screen.getByText("frontend prod")).toBeInTheDocument();
  });

  it("muestra empty states cuando no hay nada", async () => {
    vi.mocked(api.listCorsOrigins).mockResolvedValue({
      statics: [],
      dynamics: [],
    });

    render(wrap(<CorsOriginsPanel />));

    await waitFor(() => {
      expect(screen.getByText(/aún no has añadido|haven't added/i)).toBeInTheDocument();
    });
  });
});

describe("CorsOriginsPanel — add", () => {
  it("llama a addCorsOrigin con origen + nota", async () => {
    const user = userEvent.setup();
    vi.mocked(api.listCorsOrigins).mockResolvedValue({
      statics: [],
      dynamics: [],
    });
    vi.mocked(api.addCorsOrigin).mockResolvedValue({
      statics: [],
      dynamics: [
        {
          origin: "https://new.example.com",
          created_by: "u-1",
          created_at: "2026-05-19T13:00:00Z",
          note: "test",
        },
      ],
    });

    render(wrap(<CorsOriginsPanel />));
    await screen.findByPlaceholderText(/https:\/\/app/);

    const originInput = screen.getByPlaceholderText(/https:\/\/app/);
    const noteInput = screen.getByPlaceholderText(/nota|note/i);
    const submit = screen.getByRole("button", { name: /añadir|add/i });

    await user.type(originInput, "https://new.example.com");
    await user.type(noteInput, "test");
    await user.click(submit);

    await waitFor(() => {
      expect(api.addCorsOrigin).toHaveBeenCalledWith("https://new.example.com", "test");
    });
  });

  it("muestra feedback de error cuando el backend rechaza", async () => {
    const user = userEvent.setup();
    vi.mocked(api.listCorsOrigins).mockResolvedValue({
      statics: [],
      dynamics: [],
    });
    vi.mocked(api.addCorsOrigin).mockRejectedValue(
      new Error("origin must include a host"),
    );

    render(wrap(<CorsOriginsPanel />));
    await screen.findByPlaceholderText(/https:\/\/app/);

    await user.type(screen.getByPlaceholderText(/https:\/\/app/), "https://");
    await user.click(screen.getByRole("button", { name: /añadir|add/i }));

    // El mensaje del error del backend aparece tal cual (no traducimos).
    expect(await screen.findByText(/origin must include a host/)).toBeInTheDocument();
  });
});

describe("CorsOriginsPanel — delete", () => {
  it("llama a deleteCorsOrigin tras confirmación", async () => {
    const user = userEvent.setup();
    vi.spyOn(window, "confirm").mockReturnValue(true);
    vi.mocked(api.listCorsOrigins).mockResolvedValue({
      statics: [],
      dynamics: [
        {
          origin: "https://to-go.example.com",
          created_by: "u-1",
          created_at: "2026-05-19T12:00:00Z",
          note: "",
        },
      ],
    });
    vi.mocked(api.deleteCorsOrigin).mockResolvedValue();

    render(wrap(<CorsOriginsPanel />));
    await screen.findByText("https://to-go.example.com");

    const removeBtn = screen.getByRole("button", { name: /eliminar|remove/i });
    await user.click(removeBtn);

    await waitFor(() => {
      expect(api.deleteCorsOrigin).toHaveBeenCalledWith("https://to-go.example.com");
    });
  });

  it("no llama a deleteCorsOrigin si el operador cancela el confirm", async () => {
    const user = userEvent.setup();
    vi.spyOn(window, "confirm").mockReturnValue(false);
    vi.mocked(api.listCorsOrigins).mockResolvedValue({
      statics: [],
      dynamics: [
        {
          origin: "https://keep.example.com",
          created_by: "",
          created_at: "",
          note: "",
        },
      ],
    });

    render(wrap(<CorsOriginsPanel />));
    await screen.findByText("https://keep.example.com");

    await user.click(screen.getByRole("button", { name: /eliminar|remove/i }));

    expect(api.deleteCorsOrigin).not.toHaveBeenCalled();
  });
});
