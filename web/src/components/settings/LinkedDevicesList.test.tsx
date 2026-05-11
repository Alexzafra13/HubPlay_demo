import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

vi.mock("@/api/client", () => ({
  api: {
    revokeMySession: vi.fn(),
  },
}));

import { api } from "@/api/client";
import type { MySession } from "@/api/types";
import { LinkedDevicesList } from "./LinkedDevicesList";

const apiMock = api as unknown as { revokeMySession: ReturnType<typeof vi.fn> };

function wrap(node: React.ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

const linkedSession: MySession = {
  id: "sess-linked",
  device_name: "Living-room TV",
  device_id: "device-code-abcdef0123456789",
  ip_address: "192.0.2.10",
  created_at: "2026-05-10T08:00:00Z",
  last_active_at: new Date(Date.now() - 5 * 60_000).toISOString(),
  expires_at: "2026-06-10T08:00:00Z",
  current: false,
  auth_method: "device_link",
};
const webSession: MySession = {
  ...linkedSession,
  id: "sess-web",
  device_name: "Chrome on Linux",
  device_id: "browser-1",
  auth_method: "password",
};

beforeEach(() => {
  apiMock.revokeMySession.mockReset();
});

describe("LinkedDevicesList", () => {
  // Hidden state is the happy path on a fresh server. Verifying we
  // render nothing keeps the /link page free of empty stubs that add
  // visual noise during the most common first-time pairing flow.
  it("renders nothing when no sessions are device-linked", () => {
    const { container } = render(wrap(<LinkedDevicesList sessions={[webSession]} />));
    expect(container).toBeEmptyDOMElement();
  });

  // The filter must exclude password-minted sessions; otherwise the
  // /link surface duplicates what Settings → Tus dispositivos already
  // shows and loses its specific value.
  it("filters out password sessions and renders only device-linked rows", async () => {
    render(
      wrap(
        <LinkedDevicesList
          sessions={[webSession, linkedSession]}
        />,
      ),
    );
    expect(await screen.findByText("Living-room TV")).toBeInTheDocument();
    expect(screen.queryByText("Chrome on Linux")).not.toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: /dispositivos vinculados/i }),
    ).toBeInTheDocument();
  });

  // Revoking from /link should not prompt confirm — the operator is
  // clearly on a separate device when looking at the list, so the
  // "you might log yourself out" guardrail from DevicesPanel is not
  // relevant here.
  it("revokes a row without a confirm prompt", async () => {
    apiMock.revokeMySession.mockResolvedValueOnce(undefined);
    const confirmSpy = vi.spyOn(window, "confirm");

    render(wrap(<LinkedDevicesList sessions={[linkedSession]} />));
    await screen.findByText("Living-room TV");

    fireEvent.click(screen.getByRole("button", { name: /cerrar sesión/i }));
    await waitFor(() =>
      expect(apiMock.revokeMySession).toHaveBeenCalledWith("sess-linked"),
    );
    expect(confirmSpy).not.toHaveBeenCalled();
  });
});
