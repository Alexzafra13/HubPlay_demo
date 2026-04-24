import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import "@/i18n";
import AccountStep from "./AccountStep";
import { useAuthStore } from "@/store/auth";
import { ApiError } from "@/api/types";

// ─── Mocks ───────────────────────────────────────────────────────────────────

const mutateMock = vi.fn();

vi.mock("@/api/hooks", () => ({
  useSetupCreateAdmin: () => ({
    mutate: mutateMock,
    isPending: false,
    isError: false,
    isSuccess: false,
  }),
}));

const loginMock = vi.fn();

vi.mock("@/api/client", () => ({
  api: {
    login: (username: string, password: string) => loginMock(username, password),
  },
}));

// ─── Helpers ────────────────────────────────────────────────────────────────

function fillForm({
  username = "alice",
  password = "password1",
  confirm = "password1",
  displayName = "",
}: {
  username?: string;
  password?: string;
  confirm?: string;
  displayName?: string;
} = {}) {
  fireEvent.change(screen.getByLabelText(/Username/i), {
    target: { value: username },
  });
  if (displayName) {
    fireEvent.change(screen.getByLabelText(/Display Name/i), {
      target: { value: displayName },
    });
  }
  const passwordInputs = screen.getAllByLabelText(/Password/i);
  // Order: Password, Confirm Password
  fireEvent.change(passwordInputs[0], { target: { value: password } });
  fireEvent.change(passwordInputs[1], { target: { value: confirm } });
}

function submit() {
  fireEvent.click(screen.getByRole("button", { name: /Create Account/i }));
}

describe("AccountStep", () => {
  beforeEach(() => {
    mutateMock.mockReset();
    loginMock.mockReset();
    useAuthStore.setState({ user: null, isAuthenticated: false });
    localStorage.clear();
  });

  it("blocks submit and shows a username error when it is too short", () => {
    const onNext = vi.fn();
    render(<AccountStep onNext={onNext} />);

    fillForm({ username: "ab" });
    submit();

    expect(screen.getByText(/Username must be at least 3/)).toBeInTheDocument();
    expect(mutateMock).not.toHaveBeenCalled();
    expect(onNext).not.toHaveBeenCalled();
  });

  it("blocks submit and shows a password error when it is too short", () => {
    const onNext = vi.fn();
    render(<AccountStep onNext={onNext} />);

    fillForm({ password: "short", confirm: "short" });
    submit();

    expect(screen.getByText(/Password must be at least 8/)).toBeInTheDocument();
    expect(mutateMock).not.toHaveBeenCalled();
    expect(onNext).not.toHaveBeenCalled();
  });

  it("blocks submit when password and confirmation do not match", () => {
    const onNext = vi.fn();
    render(<AccountStep onNext={onNext} />);

    fillForm({ password: "password1", confirm: "password2" });
    submit();

    expect(screen.getByText(/Passwords do not match/)).toBeInTheDocument();
    expect(mutateMock).not.toHaveBeenCalled();
    expect(onNext).not.toHaveBeenCalled();
  });

  it("calls the create-admin mutation with trimmed inputs on happy path", () => {
    render(<AccountStep onNext={vi.fn()} />);

    fillForm({
      username: "  alice  ",
      password: "password1",
      confirm: "password1",
      displayName: "  Alice A.  ",
    });
    submit();

    expect(mutateMock).toHaveBeenCalledTimes(1);
    const [payload] = mutateMock.mock.calls[0];
    expect(payload).toEqual({
      username: "alice",
      password: "password1",
      display_name: "Alice A.",
    });
  });

  it("on success: stores the user in auth and calls onNext with the normalized data", () => {
    const onNext = vi.fn();
    render(<AccountStep onNext={onNext} />);

    fillForm();
    submit();

    const [, handlers] = mutateMock.mock.calls[0];
    const fakeUser = {
      id: "u1",
      username: "alice",
      display_name: "",
      role: "admin",
      created_at: "2026-04-24T00:00:00Z",
    };
    handlers.onSuccess({ user: fakeUser });

    expect(useAuthStore.getState().user).toEqual(fakeUser);
    expect(onNext).toHaveBeenCalledWith({
      username: "alice",
      password: "password1",
      displayName: undefined,
    });
  });

  it("on generic server error: surfaces the message in the form", async () => {
    render(<AccountStep onNext={vi.fn()} />);

    fillForm();
    submit();

    const [, handlers] = mutateMock.mock.calls[0];
    await handlers.onError(new Error("boom from backend"));

    expect(await screen.findByText("boom from backend")).toBeInTheDocument();
  });

  it("SETUP_COMPLETED: retries via login, stores the user, and advances", async () => {
    const onNext = vi.fn();
    render(<AccountStep onNext={onNext} />);

    fillForm();
    submit();

    const [, handlers] = mutateMock.mock.calls[0];
    const existingUser = {
      id: "u1",
      username: "alice",
      display_name: "",
      role: "admin",
      created_at: "2026-04-24T00:00:00Z",
    };
    loginMock.mockResolvedValueOnce({
      access_token: "a",
      refresh_token: "r",
      expires_in: 900,
      user: existingUser,
    });

    await handlers.onError(
      new ApiError(409, {
        error: { code: "SETUP_COMPLETED", message: "already done" },
      }),
    );

    expect(loginMock).toHaveBeenCalledWith("alice", "password1");
    expect(useAuthStore.getState().user).toEqual(existingUser);
    expect(onNext).toHaveBeenCalledWith({
      username: "alice",
      password: "password1",
      displayName: undefined,
    });
  });

  it("SETUP_COMPLETED but login fails: shows the admin-exists message and does not advance", async () => {
    const onNext = vi.fn();
    render(<AccountStep onNext={onNext} />);

    fillForm();
    submit();

    const [, handlers] = mutateMock.mock.calls[0];
    loginMock.mockRejectedValueOnce(new Error("bad password"));

    await handlers.onError(
      new ApiError(409, {
        error: { code: "SETUP_COMPLETED", message: "already done" },
      }),
    );

    expect(onNext).not.toHaveBeenCalled();
    expect(
      await screen.findByText(/Admin account already exists/),
    ).toBeInTheDocument();
  });

  it("re-hydrates from initialData when the user steps back into the form", () => {
    render(
      <AccountStep
        onNext={vi.fn()}
        initialData={{
          username: "bob",
          password: "secret123",
          displayName: "Bobby",
        }}
      />,
    );

    expect(screen.getByLabelText(/Username/i)).toHaveValue("bob");
    expect(screen.getByLabelText(/Display Name/i)).toHaveValue("Bobby");
    const [pw, confirm] = screen.getAllByLabelText(/Password/i);
    expect(pw).toHaveValue("secret123");
    // Confirm is pre-filled from password so the user can immediately resubmit.
    expect(confirm).toHaveValue("secret123");
  });
});
