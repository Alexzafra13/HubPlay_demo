import { describe, it, expect, beforeEach } from "vitest";
import { useAuthStore } from "./auth";
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
    });
    localStorage.clear();
  });

  it("starts unauthenticated", () => {
    const state = useAuthStore.getState();
    expect(state.user).toBeNull();
    expect(state.isAuthenticated).toBe(false);
  });

  it("setAuth stores user in state and localStorage", () => {
    useAuthStore.getState().setAuth(testUser);

    const state = useAuthStore.getState();
    expect(state.user).toEqual(testUser);
    expect(state.isAuthenticated).toBe(true);

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

  it("loadFromStorage restores state", () => {
    localStorage.setItem("hubplay_user", JSON.stringify(testUser));

    useAuthStore.getState().loadFromStorage();

    const state = useAuthStore.getState();
    expect(state.user).toEqual(testUser);
    expect(state.isAuthenticated).toBe(true);
  });

  it("loadFromStorage handles corrupted JSON", () => {
    localStorage.setItem("hubplay_user", "not-json");

    useAuthStore.getState().loadFromStorage();

    expect(useAuthStore.getState().user).toBeNull();
    expect(localStorage.getItem("hubplay_user")).toBeNull();
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
