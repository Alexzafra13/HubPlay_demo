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
    client = new ApiClient("http://test/api/v1");
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it("sends GET with credentials include", async () => {
    const fetch = mockFetch({ status: "ok" });
    vi.stubGlobal("fetch", fetch);

    await client.getHealth();

    expect(fetch).toHaveBeenCalledWith(
      "http://test/api/v1/health",
      expect.objectContaining({
        method: "GET",
        credentials: "include",
      }),
    );
  });

  it("does not set Authorization header (cookies handle auth)", async () => {
    const fetch = mockFetch({ needs_setup: true });
    vi.stubGlobal("fetch", fetch);

    await client.getSetupStatus();

    const callHeaders = fetch.mock.calls[0][1].headers;
    expect(callHeaders.Authorization).toBeUndefined();
  });

  it("login returns auth data without storing tokens in localStorage", async () => {
    const authResp = {
      access_token: "at",
      refresh_token: "rt",
      expires_in: 900,
      user: { id: "1", username: "admin", display_name: "A", role: "admin", created_at: "" },
    };
    vi.stubGlobal("fetch", mockFetch(authResp));

    const result = await client.login("admin", "pass");

    expect(result.access_token).toBe("at");
    // Tokens should NOT be stored in localStorage
    expect(localStorage.getItem("hubplay_access_token")).toBeNull();
    expect(localStorage.getItem("hubplay_refresh_token")).toBeNull();
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

  it("calls onTokenRefresh listener after successful refresh", async () => {
    const authResp = {
      access_token: "new-at",
      refresh_token: "new-rt",
      expires_in: 900,
      user: { id: "1", username: "admin", display_name: "A", role: "admin", created_at: "" },
    };
    vi.stubGlobal("fetch", mockFetch(authResp));

    const onTokenRefresh = vi.fn();
    client.setAuthListener({ onTokenRefresh });

    await client.refresh();

    expect(onTokenRefresh).toHaveBeenCalledWith("new-at", "new-rt");
  });

  it("coalesces concurrent refresh calls into a single network request", async () => {
    const authResp = {
      access_token: "new-at",
      refresh_token: "new-rt",
      expires_in: 900,
      user: { id: "1", username: "admin", display_name: "A", role: "admin", created_at: "" },
    };

    // Stall the response so multiple callers pile onto the same in-flight
    // promise before any of them resolves.
    let resolveFetch!: (value: unknown) => void;
    const fetchMock = vi.fn().mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveFetch = resolve;
        }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const onTokenRefresh = vi.fn();
    client.setAuthListener({ onTokenRefresh });

    // Fire five concurrent refreshes BEFORE the network call completes.
    const promises = [
      client.refresh(),
      client.refresh(),
      client.refresh(),
      client.refresh(),
      client.refresh(),
    ];

    // Exactly one fetch should have been issued.
    expect(fetchMock).toHaveBeenCalledTimes(1);

    // Now release the response and check every caller observed it.
    resolveFetch({
      ok: true,
      status: 200,
      json: () => Promise.resolve(authResp),
    });

    const results = await Promise.all(promises);
    expect(results).toHaveLength(5);
    for (const r of results) {
      expect(r.access_token).toBe("new-at");
    }

    // Listener fired exactly once even though five callers awaited.
    expect(onTokenRefresh).toHaveBeenCalledTimes(1);

    // After settling, a fresh refresh starts a new network call.
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: () => Promise.resolve(authResp),
      }),
    );
    await client.refresh();
    // The second listener call comes from the post-settlement refresh.
    expect(onTokenRefresh).toHaveBeenCalledTimes(2);
  });

  it("calls onAuthFailure on 401 when refresh fails", async () => {
    // First call: 401, second call (refresh): also fails
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({
        ok: false,
        status: 401,
        statusText: "Unauthorized",
        json: () => Promise.resolve({ error: { code: "expired", message: "Token expired" } }),
      })
      .mockRejectedValueOnce(new Error("refresh failed"));

    vi.stubGlobal("fetch", fetchMock);

    const onAuthFailure = vi.fn();
    client.setAuthListener({ onAuthFailure });

    await expect(client.getMe()).rejects.toThrow(ApiError);
    expect(onAuthFailure).toHaveBeenCalledOnce();
  });

  it("calls onAuthFailure on 401 when no cookie-based refresh succeeds", async () => {
    // 401 on initial request, refresh also fails with 401
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({
        ok: false,
        status: 401,
        statusText: "Unauthorized",
        json: () => Promise.resolve({ error: { code: "expired", message: "Token expired" } }),
      })
      .mockRejectedValueOnce(new Error("refresh failed"));

    vi.stubGlobal("fetch", fetchMock);

    const onAuthFailure = vi.fn();
    client.setAuthListener({ onAuthFailure });

    await expect(client.getMe()).rejects.toThrow(ApiError);
    expect(onAuthFailure).toHaveBeenCalledOnce();
  });

  it("does not call onAuthFailure on non-401 errors", async () => {
    vi.stubGlobal(
      "fetch",
      mockFetch({ error: { code: "not_found", message: "Not found" } }, 404, false),
    );

    const onAuthFailure = vi.fn();
    client.setAuthListener({ onAuthFailure });

    await expect(client.getItem("x")).rejects.toThrow(ApiError);
    expect(onAuthFailure).not.toHaveBeenCalled();
  });

  it("logout clears user from localStorage even if API fails", async () => {
    localStorage.setItem("hubplay_user", '{"id":"1"}');

    vi.stubGlobal(
      "fetch",
      vi.fn().mockRejectedValue(new Error("network")),
    );

    await expect(client.logout()).rejects.toThrow();
    expect(localStorage.getItem("hubplay_user")).toBeNull();
  });

  it("getBulkSchedule sends channels via POST body, not GET query", async () => {
    // A library with hundreds of channels produces a query string long
    // enough to hit a 414 at common reverse-proxy defaults; switching
    // to POST moves the payload into the body and sidesteps it.
    const fetch = mockFetch({ data: { "c-1": [], "c-2": [] } });
    vi.stubGlobal("fetch", fetch);

    await client.getBulkSchedule(["c-1", "c-2"]);

    const [url, init] = fetch.mock.calls[0];
    expect(url).toBe("http://test/api/v1/channels/schedule");
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body as string)).toEqual({
      channels: ["c-1", "c-2"],
      from: undefined,
      to: undefined,
    });
  });

  it("getBulkSchedule short-circuits on empty channel list (no fetch)", async () => {
    const fetch = mockFetch({});
    vi.stubGlobal("fetch", fetch);

    const result = await client.getBulkSchedule([]);
    expect(result).toEqual({});
    expect(fetch).not.toHaveBeenCalled();
  });

  it("getBulkSchedule chunks oversized lists into 1000-channel batches and merges results", async () => {
    // Backends commonly cap bulk requests around 5000 channels; libraries
    // larger than that used to crash the page with TOO_MANY_CHANNELS.
    // The client now splits into batches of 1000 and merges the responses
    // — this test pins both the batch size and the merge contract.
    const ids = Array.from({ length: 2500 }, (_, i) => `c-${i}`);
    let callIdx = 0;
    const fetch = vi.fn().mockImplementation(async () => {
      // Each batch returns a disjoint slice of channels; we use the
      // call index to emit a different keyspace per batch so the merge
      // can be validated.
      const slice = ids.slice(callIdx * 1000, (callIdx + 1) * 1000);
      callIdx++;
      const data: Record<string, unknown[]> = {};
      for (const id of slice) data[id] = [];
      return {
        ok: true,
        status: 200,
        headers: new Headers({ "Content-Type": "application/json" }),
        json: async () => ({ data }),
      };
    });
    vi.stubGlobal("fetch", fetch);

    const result = await client.getBulkSchedule(ids);

    // 2500 ids → 3 batches (1000, 1000, 500). All channels present.
    expect(fetch).toHaveBeenCalledTimes(3);
    expect(Object.keys(result)).toHaveLength(2500);
    expect(result["c-0"]).toEqual([]);
    expect(result["c-2499"]).toEqual([]);

    // Verify the request bodies are properly disjoint slices.
    const bodies = fetch.mock.calls.map((c) =>
      JSON.parse((c[1] as RequestInit).body as string),
    );
    expect(bodies[0].channels).toHaveLength(1000);
    expect(bodies[1].channels).toHaveLength(1000);
    expect(bodies[2].channels).toHaveLength(500);
    expect(bodies[0].channels[0]).toBe("c-0");
    expect(bodies[2].channels[499]).toBe("c-2499");
  });
});

// Causa raíz del "no salen las pistas de audio/subtítulos" (reporte de
// usuario 2026-06-10): el backend emite stream_type/stream_index y
// omite los campos vacíos, pero todos los consumers del player hablan
// type/index con nulls. getItem debe normalizar en la frontera.
describe("ApiClient — normalización de media_streams", () => {
  it("getItem convierte la forma del wire al tipo del cliente", async () => {
    const wireItem = {
      id: "it-1",
      type: "movie",
      title: "The Batman",
      media_streams: [
        { stream_index: 0, stream_type: "video", codec: "h264", is_default: true, width: 1920, height: 800, profile: "High" },
        { stream_index: 1, stream_type: "audio", codec: "eac3", is_default: true, channels: 6, language: "spa" },
        // SUBRIP sin language/title omitidos parcialmente, como hace el backend.
        { stream_index: 2, stream_type: "subtitle", codec: "subrip", is_default: false, language: "spa", title: "Castellano Forzados" },
        { stream_index: 3, stream_type: "subtitle", codec: "subrip", is_default: false },
      ],
    };
    vi.stubGlobal("fetch", mockFetch({ data: wireItem }));
    const client = new ApiClient("http://test/api/v1");

    const item = await client.getItem("it-1");
    const subs = item.media_streams!.filter((s) => s.type === "subtitle");

    expect(subs).toHaveLength(2);
    expect(subs[0]).toMatchObject({
      index: 2,
      type: "subtitle",
      codec: "subrip",
      language: "spa",
      title: "Castellano Forzados",
    });
    // Campos omitidos por el wire → null explícito, no undefined.
    expect(subs[1].language).toBeNull();
    expect(subs[1].title).toBeNull();
    expect(subs[1].index).toBe(3);
    // Los extras del wire (profile, width…) sobreviven para la vista
    // de información del fichero.
    const video = item.media_streams!.find((s) => s.type === "video")!;
    expect((video as unknown as Record<string, unknown>).profile).toBe("High");
    expect(item.media_streams!.find((s) => s.type === "audio")!.channels).toBe(6);
  });
});
