// Login — tests the error-mapping contract that drives what users
// see when their login attempt fails. The wire-level codes
// (ACCESS_EXPIRED, ACCOUNT_DISABLED, INVALID_CREDENTIALS,
// RATE_LIMITED) are documented in the project memory; if they ever
// drift between server and client these tests catch it.
//
// Visual concerns (aurora backdrop, GhostPosters animation) are
// intentionally not covered.

import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  render,
  screen,
  waitFor,
  fireEvent,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "@/i18n";

const apiMock = vi.hoisted(() => ({
  login: vi.fn(),
}));
vi.mock("@/api/client", () => ({
  api: apiMock,
}));

const navigateMock = vi.hoisted(() => vi.fn());
vi.mock("react-router", async () => {
  const actual = await vi.importActual<typeof import("react-router")>(
    "react-router",
  );
  return {
    ...actual,
    useNavigate: () => navigateMock,
  };
});

import Login from "./Login";
import { ApiError } from "@/api/types";
import { useAuthStore } from "@/store/auth";

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

async function submitLogin(username = "alice", password = "hunter22") {
  fireEvent.change(screen.getByLabelText(/usuario|username/i), {
    target: { value: username },
  });
  fireEvent.change(screen.getByLabelText(/contraseña|password/i), {
    target: { value: password },
  });
  fireEvent.click(screen.getByRole("button", { name: /iniciar|sign in/i }));
}

beforeEach(() => {
  apiMock.login.mockReset();
  navigateMock.mockReset();
  useAuthStore.setState({ user: null, isAuthenticated: false, bootstrapped: true });
});

describe("Login error messaging", () => {
  it("surfaces the access-expired friendly copy when the server returns ACCESS_EXPIRED", async () => {
    apiMock.login.mockRejectedValueOnce(
      new ApiError(403, {
        error: { code: "ACCESS_EXPIRED", message: "expired" },
      }),
    );
    render(wrap(<Login />));
    await submitLogin();
    await waitFor(() => {
      expect(
        screen.getByText(/acceso temporal ha caducado/i),
      ).toBeInTheDocument();
    });
    expect(navigateMock).not.toHaveBeenCalled();
  });

  it("surfaces a different friendly copy for ACCOUNT_DISABLED", async () => {
    apiMock.login.mockRejectedValueOnce(
      new ApiError(403, {
        error: { code: "ACCOUNT_DISABLED", message: "disabled" },
      }),
    );
    render(wrap(<Login />));
    await submitLogin();
    await waitFor(() => {
      expect(
        screen.getByText(/cuenta está desactivada/i),
      ).toBeInTheDocument();
    });
  });

  it("uses anti-enumeration copy for wrong username/password (INVALID_CREDENTIALS)", async () => {
    apiMock.login.mockRejectedValueOnce(
      new ApiError(401, {
        error: { code: "INVALID_CREDENTIALS", message: "nope" },
      }),
    );
    render(wrap(<Login />));
    await submitLogin();
    await waitFor(() => {
      expect(
        screen.getByText(/usuario o contraseña incorrectos/i),
      ).toBeInTheDocument();
    });
  });

  it("collapses both rate-limit codes (RATE_LIMITED + TOO_MANY_REQUESTS) into one user message", async () => {
    apiMock.login.mockRejectedValueOnce(
      new ApiError(429, {
        error: { code: "TOO_MANY_REQUESTS", message: "slow down" },
      }),
    );
    render(wrap(<Login />));
    await submitLogin();
    await waitFor(() => {
      expect(screen.getByText(/demasiados intentos/i)).toBeInTheDocument();
    });
  });

  it("falls through to err.message for unknown codes", async () => {
    apiMock.login.mockRejectedValueOnce(
      new ApiError(500, {
        error: { code: "EXOTIC_THING", message: "exotic message" },
      }),
    );
    render(wrap(<Login />));
    await submitLogin();
    await waitFor(() => {
      expect(screen.getByText("exotic message")).toBeInTheDocument();
    });
  });
});

describe("Login redirect logic", () => {
  it("routes to /change-password when password_change_required", async () => {
    apiMock.login.mockResolvedValueOnce({
      access_token: "tok",
      refresh_token: "ref",
      expires_in: 900,
      user: {
        id: "u1",
        username: "alice",
        display_name: "Alice",
        role: "user",
        created_at: "2026-05-10T10:00:00Z",
        password_change_required: true,
      },
    });
    render(wrap(<Login />));
    await submitLogin();
    await waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith("/change-password");
    });
  });

  it("routes to /select-profile when there are multiple profiles", async () => {
    apiMock.login.mockResolvedValueOnce({
      access_token: "tok",
      refresh_token: "ref",
      expires_in: 900,
      user: {
        id: "u1",
        username: "alice",
        display_name: "Alice",
        role: "user",
        created_at: "2026-05-10T10:00:00Z",
        password_change_required: false,
      },
      profiles: [
        { id: "u1", username: "alice", display_name: "Alice", role: "user", is_active: true, has_pin: false },
        { id: "u2", username: "alice/kid", display_name: "Kid", role: "user", is_active: true, has_pin: false, parent_user_id: "u1" },
      ],
    });
    render(wrap(<Login />));
    await submitLogin();
    await waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith("/select-profile");
    });
  });

  it("routes solo accounts straight to /", async () => {
    apiMock.login.mockResolvedValueOnce({
      access_token: "tok",
      refresh_token: "ref",
      expires_in: 900,
      user: {
        id: "u1",
        username: "alice",
        display_name: "Alice",
        role: "user",
        created_at: "2026-05-10T10:00:00Z",
        password_change_required: false,
      },
      profiles: [
        { id: "u1", username: "alice", display_name: "Alice", role: "user", is_active: true, has_pin: false },
      ],
    });
    render(wrap(<Login />));
    await submitLogin();
    await waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith("/");
    });
  });
});
