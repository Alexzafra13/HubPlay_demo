// WhoIsWatching — covers the routing behaviour that broke once
// during a refactor (solo accounts not being bounced home) and the
// error-fallback path (which used to silently render an empty grid
// before the explicit "no pudimos cargar" branch shipped).
//
// The cinematic upgrade — backdrop mosaic / poster wall / hover
// ambient — is intentionally out of scope; it's pure presentation
// and the framer-motion animations don't play well with jsdom.

import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "@/i18n";

const apiMock = vi.hoisted(() => ({
  listProfiles: vi.fn(),
  getMe: vi.fn(),
  getItems: vi.fn(),
  switchProfile: vi.fn(),
  logout: vi.fn(),
}));
vi.mock("@/api/client", () => ({
  api: apiMock,
}));

const navigateMock = vi.hoisted(() => vi.fn());
vi.mock("react-router", async () => {
  const actual = await vi.importActual<typeof import("react-router")>(
    "react-router",
  );
  return { ...actual, useNavigate: () => navigateMock };
});

import WhoIsWatching from "./WhoIsWatching";
import { useAuthStore } from "@/store/auth";

function wrap(node: React.ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return (
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/select-profile"]}>{node}</MemoryRouter>
    </QueryClientProvider>
  );
}

beforeEach(() => {
  apiMock.listProfiles.mockReset();
  apiMock.getMe.mockReset();
  apiMock.getItems.mockReset();
  apiMock.switchProfile.mockReset();
  apiMock.logout.mockReset();
  navigateMock.mockReset();
  useAuthStore.setState({
    user: { id: "u1", username: "alice", display_name: "Alice", role: "user", created_at: "" },
    isAuthenticated: true,
    bootstrapped: true,
  });
  // The poster-wall / mosaic call is harmless to leave failing.
  apiMock.getItems.mockResolvedValue({ items: [], total: 0 });
  apiMock.getMe.mockResolvedValue({
    id: "u1", username: "alice", display_name: "Alice", role: "user", created_at: "",
  });
});

describe("WhoIsWatching", () => {
  it("bounces solo accounts (1 profile) straight to /", async () => {
    apiMock.listProfiles.mockResolvedValueOnce([
      { id: "u1", username: "alice", display_name: "Alice", role: "user", is_active: true, has_pin: false },
    ]);
    render(wrap(<WhoIsWatching />));
    await waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith("/", { replace: true });
    });
  });

  it("shows the explicit error fallback when the profile fetch fails", async () => {
    apiMock.listProfiles.mockRejectedValueOnce(new Error("boom"));
    render(wrap(<WhoIsWatching />));
    await waitFor(() => {
      expect(
        screen.getByText(/no pudimos cargar los perfiles/i),
      ).toBeInTheDocument();
    });
    // Sign-out + continue-anyway recoveries are visible
    expect(screen.getByText(/cerrar sesión/i)).toBeInTheDocument();
    expect(screen.getByText(/continuar al inicio/i)).toBeInTheDocument();
  });

  it("renders the picker with multiple profiles and an avatar tile per one", async () => {
    apiMock.listProfiles.mockResolvedValueOnce([
      { id: "u1", username: "alice", display_name: "Alice", role: "user", is_active: true, has_pin: false },
      { id: "u2", username: "alice/kid", display_name: "Kid", role: "user", is_active: true, has_pin: false, parent_user_id: "u1" },
    ]);
    render(wrap(<WhoIsWatching />));
    await waitFor(() => {
      expect(screen.getByText("Alice")).toBeInTheDocument();
    });
    expect(screen.getByText("Kid")).toBeInTheDocument();
    // Multi-profile must NOT bounce — the picker's the whole point.
    expect(navigateMock).not.toHaveBeenCalledWith("/", { replace: true });
  });

  it("shows the back button that always navigates to / (not history.back)", async () => {
    apiMock.listProfiles.mockResolvedValueOnce([
      { id: "u1", username: "alice", display_name: "Alice", role: "user", is_active: true, has_pin: false },
      { id: "u2", username: "alice/kid", display_name: "Kid", role: "user", is_active: true, has_pin: false, parent_user_id: "u1" },
    ]);
    render(wrap(<WhoIsWatching />));
    await waitFor(() => {
      expect(screen.getByRole("button", { name: /volver/i })).toBeInTheDocument();
    });
    // The button's behaviour (always navigate("/"), never -1) is
    // documented in the page comment because history-back from a
    // fresh login lands on /login again. We don't simulate the
    // click here — the rest of the test suite covers navigate
    // semantics — but pin its presence so a future refactor that
    // drops it gets caught.
  });
});
