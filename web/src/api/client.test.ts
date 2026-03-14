import { describe, it, expect, beforeEach, vi } from "vitest";
import { ApiClient } from "./client";
import { ApiError } from "./types";

function mockFetch(
  body: unknown,
  status = 200,
  ok = true,
): ReturnType<typeof vi.fn> {
  return vi.fn().mockResolvedValue({
    ok,
    status,
    statusText: status === 401 ? "Unauthorized" : "OK",
    json: () => Promise.resolve(body),
  });
}

describe("ApiClient", () => {
  let client: ApiClient;

  beforeEach(() => {
    client = new ApiClient("http://test");
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it("sends GET with auth header when token exists", async () => {
    localStorage.setItem("hubplay_access_token", "my-token");
    const fetch = mockFetch({ status: "ok" });
    vi.stubGlobal("fetch", fetch);

    await client.getHealth();

    expect(fetch).toHaveBeenCalledWith(
      "http://test/api/health",
      expect.objectContaining({
        method: "GET",
        headers: expect.objectContaining({
          Authorization: "Bearer my-token",
        }),
      }),
    );
  });

  it("sends GET without auth header when no token", async () => {
    const fetch = mockFetch({ needs_setup: true });
    vi.stubGlobal("fetch", fetch);

    await client.getSetupStatus();

    const callHeaders = fetch.mock.calls[0][1].headers;
    expect(callHeaders.Authorization).toBeUndefined();
  });

  it("login stores tokens in localStorage", async () => {
    const authResp = {
      access_token: "at",
      refresh_token: "rt",
      expires_in: 900,
      user: { id: "1", username: "admin", display_name: "A", role: "admin", created_at: "" },
    };
    vi.stubGlobal("fetch", mockFetch(authResp));

    const result = await client.login("admin", "pass");

    expect(result.access_token).toBe("at");
    expect(localStorage.getItem("hubplay_access_token")).toBe("at");
    expect(localStorage.getItem("hubplay_refresh_token")).toBe("rt");
  });

  it("throws ApiError on non-ok response", async () => {
    const errorBody = { error: { code: "unauthorized", message: "Bad creds" } };
    vi.stubGlobal("fetch", mockFetch(errorBody, 401, false));

    await expect(client.login("x", "y")).rejects.toThrow(ApiError);
    await expect(client.login("x", "y")).rejects.toMatchObject({
      status: 401,
      code: "unauthorized",
    });
  });

  it("handles 204 No Content", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: true, status: 204, json: () => Promise.reject() }),
    );

    const result = await client.scanLibrary("lib-1");
    expect(result).toBeUndefined();
  });

  it("appends query params correctly", async () => {
    const fetch = mockFetch({ items: [], total: 0, offset: 0, limit: 20 });
    vi.stubGlobal("fetch", fetch);

    await client.searchItems("test query", "movie", 10);

    const url = fetch.mock.calls[0][0] as string;
    expect(url).toContain("q=test+query");
    expect(url).toContain("type=movie");
    expect(url).toContain("limit=10");
  });

  it("logout clears tokens even if API fails", async () => {
    localStorage.setItem("hubplay_access_token", "at");
    localStorage.setItem("hubplay_refresh_token", "rt");

    vi.stubGlobal(
      "fetch",
      vi.fn().mockRejectedValue(new Error("network")),
    );

    await expect(client.logout()).rejects.toThrow();
    expect(localStorage.getItem("hubplay_access_token")).toBeNull();
    expect(localStorage.getItem("hubplay_refresh_token")).toBeNull();
  });
});
