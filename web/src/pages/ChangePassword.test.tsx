// ChangePassword — covers the forced rotation flow that gates the
// app for users with `password_change_required`. Tests pin:
//
//   - Unauthenticated visit → bounce to /login.
//   - Forced mode hides the current-password field by default and
//     reveals it via the "alsoVerifyOld" affordance.
//   - Voluntary mode keeps the current-password field on screen and
//     marks it required.
//   - Inline validation (length + mismatch) surfaces the right
//     i18n copy without calling the API.
//   - On success, refreshMe is invoked and the user is sent home.
//   - On API failure, the error message is rendered and we stay on
//     the page.

import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  render,
  screen,
  waitFor,
  fireEvent,
} from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "@/i18n";

const apiMock = vi.hoisted(() => ({
  changeMyPassword: vi.fn(),
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

import ChangePassword from "./ChangePassword";
import { useAuthStore } from "@/store/auth";

function wrap(node: React.ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return (
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/change-password"]}>
        <Routes>
          <Route path="/change-password" element={node} />
          <Route path="/login" element={<div>Login screen</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  );
}

beforeEach(() => {
  apiMock.changeMyPassword.mockReset();
  navigateMock.mockReset();
});

const refreshMeMock = vi.fn();

function setForcedRotation() {
  useAuthStore.setState({
    user: {
      id: "u-1",
      username: "alice",
      role: "user",
      password_change_required: true,
    } as never,
    isAuthenticated: true,
    bootstrapped: true,
    refreshMe: refreshMeMock,
  });
  refreshMeMock.mockReset();
  refreshMeMock.mockResolvedValue(undefined);
}

function setVoluntaryRotation() {
  useAuthStore.setState({
    user: {
      id: "u-1",
      username: "alice",
      role: "user",
      password_change_required: false,
    } as never,
    isAuthenticated: true,
    bootstrapped: true,
    refreshMe: refreshMeMock,
  });
  refreshMeMock.mockReset();
  refreshMeMock.mockResolvedValue(undefined);
}

describe("ChangePassword routing guards", () => {
  it("redirects to /login when the user is not authenticated", () => {
    useAuthStore.setState({
      user: null,
      isAuthenticated: false,
      bootstrapped: true,
      refreshMe: refreshMeMock,
    });

    render(wrap(<ChangePassword />));

    expect(screen.getByText(/login screen/i)).toBeInTheDocument();
  });
});

describe("ChangePassword forced rotation", () => {
  beforeEach(() => {
    setForcedRotation();
  });

  it("hides the current-password field by default but offers the verify toggle", () => {
    render(wrap(<ChangePassword />));

    expect(
      screen.queryByLabelText(/contraseña actual|current password/i),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", {
        name: /quiero también escribir|i'd like to also type/i,
      }),
    ).toBeInTheDocument();
  });

  it("reveals the current-password field when alsoVerifyOld is clicked", () => {
    render(wrap(<ChangePassword />));

    fireEvent.click(
      screen.getByRole("button", {
        name: /quiero también escribir|i'd like to also type/i,
      }),
    );

    expect(screen.getByLabelText(/contraseña actual|current password/i)).toBeInTheDocument();
  });

  it("submits with empty currentPassword in the default forced flow", async () => {
    apiMock.changeMyPassword.mockResolvedValueOnce(undefined);

    render(wrap(<ChangePassword />));

    fireEvent.change(screen.getByLabelText(/^(contraseña nueva|new password)$/i), {
      target: { value: "supersecret" },
    });
    fireEvent.change(screen.getByLabelText(/repite la contraseña|repeat new password/i), {
      target: { value: "supersecret" },
    });
    fireEvent.click(screen.getByRole("button", { name: /guardar|save/i }));

    await waitFor(() => {
      expect(apiMock.changeMyPassword).toHaveBeenCalledWith(
        "",
        "supersecret",
      );
    });
    await waitFor(() => {
      expect(refreshMeMock).toHaveBeenCalled();
    });
    expect(navigateMock).toHaveBeenCalledWith("/", { replace: true });
  });
});

describe("ChangePassword inline validation", () => {
  beforeEach(() => {
    setForcedRotation();
  });

  it("blocks submission when the new password is shorter than 8 chars", async () => {
    render(wrap(<ChangePassword />));

    fireEvent.change(screen.getByLabelText(/^(contraseña nueva|new password)$/i), {
      target: { value: "short" },
    });
    fireEvent.change(screen.getByLabelText(/repite la contraseña|repeat new password/i), {
      target: { value: "short" },
    });
    fireEvent.click(screen.getByRole("button", { name: /guardar|save/i }));

    await screen.findByText(/al menos 8 caracteres|at least 8 characters/i);
    expect(apiMock.changeMyPassword).not.toHaveBeenCalled();
  });

  it("blocks submission when the confirmation does not match", async () => {
    render(wrap(<ChangePassword />));

    fireEvent.change(screen.getByLabelText(/^(contraseña nueva|new password)$/i), {
      target: { value: "supersecret" },
    });
    fireEvent.change(screen.getByLabelText(/repite la contraseña|repeat new password/i), {
      target: { value: "supersecreT" },
    });
    fireEvent.click(screen.getByRole("button", { name: /guardar|save/i }));

    await screen.findByText(/no coinciden|don't match/i);
    expect(apiMock.changeMyPassword).not.toHaveBeenCalled();
  });

  it("renders the server error message when the API rejects", async () => {
    apiMock.changeMyPassword.mockRejectedValueOnce(
      new Error("Contraseña actual incorrecta"),
    );

    render(wrap(<ChangePassword />));

    fireEvent.change(screen.getByLabelText(/^(contraseña nueva|new password)$/i), {
      target: { value: "supersecret" },
    });
    fireEvent.change(screen.getByLabelText(/repite la contraseña|repeat new password/i), {
      target: { value: "supersecret" },
    });
    fireEvent.click(screen.getByRole("button", { name: /guardar|save/i }));

    await screen.findByText(/contraseña actual incorrecta/i);
    expect(navigateMock).not.toHaveBeenCalled();
  });
});

describe("ChangePassword voluntary rotation", () => {
  beforeEach(() => {
    setVoluntaryRotation();
  });

  it("shows the current-password field upfront and submits it", async () => {
    apiMock.changeMyPassword.mockResolvedValueOnce(undefined);

    render(wrap(<ChangePassword />));

    expect(screen.getByLabelText(/contraseña actual|current password/i)).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText(/contraseña actual|current password/i), {
      target: { value: "old-pass" },
    });
    fireEvent.change(screen.getByLabelText(/^(contraseña nueva|new password)$/i), {
      target: { value: "new-pass-123" },
    });
    fireEvent.change(screen.getByLabelText(/repite la contraseña|repeat new password/i), {
      target: { value: "new-pass-123" },
    });
    fireEvent.click(screen.getByRole("button", { name: /guardar|save/i }));

    await waitFor(() => {
      expect(apiMock.changeMyPassword).toHaveBeenCalledWith(
        "old-pass",
        "new-pass-123",
      );
    });
  });
});
