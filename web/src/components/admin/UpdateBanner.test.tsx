import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import "@/i18n";
import { UpdateBanner } from "./UpdateBanner";
import { api } from "@/api/client";
import type { UpdateStatus, UpdatesConfig } from "@/api/types";

vi.mock("@/api/client", () => ({
  api: {
    getUpdateStatus: vi.fn(),
    checkUpdates: vi.fn(),
    getUpdatesConfig: vi.fn(),
    setUpdatesConfig: vi.fn(),
  },
}));

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

function makeStatus(overrides: Partial<UpdateStatus> = {}): UpdateStatus {
  return {
    current: "v0.1.0",
    latest: "",
    has_update: false,
    check_enabled: true,
    user_disabled: false,
    ...overrides,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("UpdateBanner — estados", () => {
  it("muestra el mensaje de capability deshabilitada en build dev", async () => {
    vi.mocked(api.getUpdateStatus).mockResolvedValue(
      makeStatus({ current: "dev", check_enabled: false }),
    );

    render(wrap(<UpdateBanner />));

    await waitFor(() => {
      expect(
        screen.getByText(/comprobaci[oó]n de updates deshabilitada/i),
      ).toBeInTheDocument();
    });
    // No debe pintarse el toggle del admin — el binario no tiene capability.
    expect(screen.queryByTestId("updates-disabled-by-admin")).toBeNull();
  });

  it("muestra el banner 'desactivado por el admin' con botón Activar", async () => {
    vi.mocked(api.getUpdateStatus).mockResolvedValue(
      makeStatus({ user_disabled: true }),
    );

    render(wrap(<UpdateBanner />));

    await waitFor(() => {
      expect(screen.getByTestId("updates-disabled-by-admin")).toBeInTheDocument();
    });
    expect(screen.getByRole("button", { name: /activar/i })).toBeInTheDocument();
  });

  it("click en Activar llama setUpdatesConfig(true)", async () => {
    const user = userEvent.setup();
    vi.mocked(api.getUpdateStatus).mockResolvedValue(
      makeStatus({ user_disabled: true }),
    );
    const fresh: UpdatesConfig = { enabled: true };
    vi.mocked(api.setUpdatesConfig).mockResolvedValue(fresh);

    render(wrap(<UpdateBanner />));

    const btn = await screen.findByRole("button", { name: /activar/i });
    await user.click(btn);

    await waitFor(() => {
      expect(api.setUpdatesConfig).toHaveBeenCalledWith(true);
    });
  });

  it("muestra banner de update disponible cuando has_update=true", async () => {
    vi.mocked(api.getUpdateStatus).mockResolvedValue(
      makeStatus({
        latest: "v0.2.0",
        has_update: true,
        release_url: "https://github.com/Alexzafra13/HubPlay_demo/releases/tag/v0.2.0",
      }),
    );

    render(wrap(<UpdateBanner />));

    await waitFor(() => {
      expect(screen.getByRole("alert")).toBeInTheDocument();
    });
    expect(screen.getByText(/v0\.2\.0/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /ver release/i })).toHaveAttribute(
      "href",
      "https://github.com/Alexzafra13/HubPlay_demo/releases/tag/v0.2.0",
    );
  });

  it("estado al día expone botón Deshabilitar que llama setUpdatesConfig(false)", async () => {
    const user = userEvent.setup();
    vi.mocked(api.getUpdateStatus).mockResolvedValue(
      makeStatus({
        last_checked: "2026-05-20T10:00:00Z",
      }),
    );
    const fresh: UpdatesConfig = { enabled: false };
    vi.mocked(api.setUpdatesConfig).mockResolvedValue(fresh);

    render(wrap(<UpdateBanner />));

    const btn = await screen.findByRole("button", {
      name: /deshabilitar comprobaci[oó]n autom[aá]tica/i,
    });
    await user.click(btn);

    await waitFor(() => {
      expect(api.setUpdatesConfig).toHaveBeenCalledWith(false);
    });
  });

  it("estado al día expone botón Comprobar ahora", async () => {
    vi.mocked(api.getUpdateStatus).mockResolvedValue(makeStatus());

    render(wrap(<UpdateBanner />));

    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: /comprobar ahora/i }),
      ).toBeInTheDocument();
    });
  });
});
