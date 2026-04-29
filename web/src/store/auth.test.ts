import { describe, it, expect, beforeEach, vi } from "vitest";
import { useAuthStore } from "./auth";
import { api } from "@/api/client";
import type { User } from "@/api/types";

const testUser: User = {
  id: "u1",
  username: "admin",
  display_name: "Admin",
  role: "admin",
  created_at: "2025-01-01T00:00:00Z",
};

describe("useAuthStore", () => {
  beforeEach(() => {
    useAuthStore.setState({
      user: null,
      isAuthenticated: false,
      bootstrapped: false,
    });
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it("starts unauthenticated and unbootstrapped", () => {
    const state = useAuthStore.getState();
    expect(state.user).toBeNull();
    expect(state.isAuthenticated).toBe(false);
    expect(state.bootstrapped).toBe(false);
  });

  it("setAuth stores user in state and localStorage", () => {
    useAuthStore.getState().setAuth(testUser);

    const state = useAuthStore.getState();
    expect(state.user).toEqual(testUser);
    expect(state.isAuthenticated).toBe(true);
    expect(state.bootstrapped).toBe(true);

    expect(JSON.parse(localStorage.getItem("hubplay_user")!)).toEqual(
      testUser,
    );
    // Tokens should NOT be in localStorage (handled by HTTP-only cookies)
    expect(localStorage.getItem("hubplay_access_token")).toBeNull();
    expect(localStorage.getItem("hubplay_refresh_token")).toBeNull();
  });

  it("logout clears state and localStorage", () => {
    useAuthStore.getState().setAuth(testUser);
    useAuthStore.getState().logout();

    const state = useAuthStore.getState();
    expect(state.user).toBeNull();
    expect(state.isAuthenticated).toBe(false);
    expect(localStorage.getItem("hubplay_user")).toBeNull();
  });

  it("bootstrap restores user and refreshes the access cookie", async () => {
    localStorage.setItem("hubplay_user", JSON.stringify(testUser));
    const refreshSpy = vi
      .spyOn(api, "refresh")
      .mockResolvedValue({} as never);

    await useAuthStore.getState().bootstrap();

    const state = useAuthStore.getState();
    expect(state.user).toEqual(testUser);
    expect(state.isAuthenticated).toBe(true);
    expect(state.bootstrapped).toBe(true);
    expect(refreshSpy).toHaveBeenCalledTimes(1);
  });

  it("bootstrap with corrupted JSON marks bootstrapped without authenticating", async () => {
    localStorage.setItem("hubplay_user", "not-json");

    await useAuthStore.getState().bootstrap();

    const state = useAuthStore.getState();
    expect(state.user).toBeNull();
    expect(state.isAuthenticated).toBe(false);
    expect(state.bootstrapped).toBe(true);
    expect(localStorage.getItem("hubplay_user")).toBeNull();
  });

  it("bootstrap clears state when refresh rejects (expired session)", async () => {
    localStorage.setItem("hubplay_user", JSON.stringify(testUser));
    vi.spyOn(api, "refresh").mockRejectedValue(new Error("expired"));

    await useAuthStore.getState().bootstrap();

    const state = useAuthStore.getState();
    expect(state.user).toBeNull();
    expect(state.isAuthenticated).toBe(false);
    expect(state.bootstrapped).toBe(true);
    expect(localStorage.getItem("hubplay_user")).toBeNull();
  });

  it("bootstrap is idempotent", async () => {
    const refreshSpy = vi.spyOn(api, "refresh");
    await useAuthStore.getState().bootstrap();
    await useAuthStore.getState().bootstrap();
    expect(refreshSpy).not.toHaveBeenCalled();
  });

  it("updateUser updates user in state and localStorage", () => {
    useAuthStore.getState().setAuth(testUser);

    const updated = { ...testUser, display_name: "New Name" };
    useAuthStore.getState().updateUser(updated);

    expect(useAuthStore.getState().user?.display_name).toBe("New Name");
    expect(
      JSON.parse(localStorage.getItem("hubplay_user")!).display_name,
    ).toBe("New Name");
  });
});
