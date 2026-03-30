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
      accessToken: null,
      refreshToken: null,
    });
    localStorage.clear();
  });

  it("starts unauthenticated", () => {
    const state = useAuthStore.getState();
    expect(state.user).toBeNull();
    expect(state.accessToken).toBeNull();
    expect(state.refreshToken).toBeNull();
  });

  it("setAuth stores credentials in state and localStorage", () => {
    useAuthStore.getState().setAuth(testUser, "access-tok", "refresh-tok");

    const state = useAuthStore.getState();
    expect(state.user).toEqual(testUser);
    expect(state.accessToken).toBe("access-tok");
    expect(state.refreshToken).toBe("refresh-tok");

    expect(localStorage.getItem("hubplay_access_token")).toBe("access-tok");
    expect(localStorage.getItem("hubplay_refresh_token")).toBe("refresh-tok");
    expect(JSON.parse(localStorage.getItem("hubplay_user")!)).toEqual(
      testUser,
    );
  });

  it("logout clears state and localStorage", () => {
    useAuthStore.getState().setAuth(testUser, "a", "r");
    useAuthStore.getState().logout();

    const state = useAuthStore.getState();
    expect(state.user).toBeNull();
    expect(state.accessToken).toBeNull();
    expect(localStorage.getItem("hubplay_access_token")).toBeNull();
  });

  it("loadFromStorage restores state", () => {
    localStorage.setItem("hubplay_access_token", "a");
    localStorage.setItem("hubplay_refresh_token", "r");
    localStorage.setItem("hubplay_user", JSON.stringify(testUser));

    useAuthStore.getState().loadFromStorage();

    const state = useAuthStore.getState();
    expect(state.user).toEqual(testUser);
    expect(state.accessToken).toBe("a");
  });

  it("loadFromStorage handles corrupted JSON", () => {
    localStorage.setItem("hubplay_access_token", "a");
    localStorage.setItem("hubplay_refresh_token", "r");
    localStorage.setItem("hubplay_user", "not-json");

    useAuthStore.getState().loadFromStorage();

    expect(useAuthStore.getState().user).toBeNull();
    expect(localStorage.getItem("hubplay_access_token")).toBeNull();
  });

  it("updateTokens syncs tokens without touching user", () => {
    useAuthStore.getState().setAuth(testUser, "old-at", "old-rt");

    useAuthStore.getState().updateTokens("new-at", "new-rt");

    const state = useAuthStore.getState();
    expect(state.accessToken).toBe("new-at");
    expect(state.refreshToken).toBe("new-rt");
    expect(state.user).toEqual(testUser);
    expect(state.isAuthenticated).toBe(true);

    expect(localStorage.getItem("hubplay_access_token")).toBe("new-at");
    expect(localStorage.getItem("hubplay_refresh_token")).toBe("new-rt");
    // User unchanged in localStorage
    expect(JSON.parse(localStorage.getItem("hubplay_user")!)).toEqual(testUser);
  });

  it("updateUser updates user in state and localStorage", () => {
    useAuthStore.getState().setAuth(testUser, "a", "r");

    const updated = { ...testUser, display_name: "New Name" };
    useAuthStore.getState().updateUser(updated);

    expect(useAuthStore.getState().user?.display_name).toBe("New Name");
    expect(
      JSON.parse(localStorage.getItem("hubplay_user")!).display_name,
    ).toBe("New Name");
  });
});
