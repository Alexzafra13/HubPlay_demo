import type {
  AddEPGSourceRequest,
  AuthResponse,
  AvailableImage,
  BrowseResponse,
  Channel,
  ChannelWithoutEPG,
  ContinueWatchingChannel,
  CreateLibraryRequest,
  EPGProgram,
  HealthResponse,
  ImageInfo,
  ImportPublicIPTVResponse,
  IPTVScheduledJob,
  IPTVScheduledJobKind,
  ItemDetail,
  Library,
  PersonDetail,
  LibraryEPGSource,
  MediaItem,
  PaginatedResponse,
  PatchChannelRequest,
  PreflightResult,
  PublicCountry,
  PublicEPGSource,
  SetupStatus,
  StreamSession,
  SystemCapabilities,
  SystemSettingsResponse,
  SystemStats,
  AuthKey,
  RotateAuthKeyResponse,
  UnhealthyChannel,
  UpdateLibraryRequest,
  UpsertScheduledJobRequest,
  User,
  UserData,
  ApiErrorBody,
  ExternalSubtitleResult,
} from "./types";
import { ApiError } from "./types";
import { getClientCapabilitiesHeader } from "./clientCapabilities";

const USER_KEY = "hubplay_user";

function getCookie(name: string): string | undefined {
  const match = document.cookie.match(new RegExp(`(?:^|; )${name}=([^;]*)`));
  return match ? decodeURIComponent(match[1]) : undefined;
}

interface RequestOptions {
  params?: Record<string, string | number | boolean | undefined>;
  body?: unknown;
  headers?: Record<string, string>;
  /**
   * When true, sets `keepalive: true` on the underlying fetch so the
   * request survives page unload, navigation, or component teardown.
   * Use for "fire-and-forget" telemetry (playback-failure beacon,
   * progress on stream end, etc.) where we genuinely don't care about
   * the response and absolutely don't want the user closing the player
   * to drop the signal. Note: keepalive payloads are capped at 64 KB
   * by the spec, so reserve this for tiny JSON.
   */
  keepalive?: boolean;
}

type AuthEventListener = {
  onTokenRefresh?: (accessToken: string, refreshToken: string) => void;
  onAuthFailure?: () => void;
};

export class ApiClient {
  private baseUrl: string;
  private authListener: AuthEventListener = {};
  /**
   * In-flight refresh promise, used to coalesce concurrent refreshes.
   *
   * When the access token has just expired and the page mounts several
   * queries at once, every one of them hits 401 and would otherwise
   * fire its own `/auth/refresh`. The server is idempotent — it keeps
   * the same refresh token and just mints a new access cookie — so the
   * extra round-trips aren't a correctness issue, but they:
   *   - waste bandwidth and DB writes (each refresh updates last_active),
   *   - produce N overwriting Set-Cookie responses that race the
   *     subsequent retries,
   *   - fan out into N onAuthFailure callbacks if the refresh fails,
   *     bouncing the user to /login N times.
   *
   * Holding the in-flight promise here means every caller awaits the
   * same network request and observes the same outcome.
   */
  private refreshInFlight: Promise<AuthResponse> | null = null;

  constructor(baseUrl = "") {
    this.baseUrl = baseUrl;
  }

  /** Register callbacks to sync auth state (e.g. Zustand store). */
  setAuthListener(listener: AuthEventListener) {
    this.authListener = listener;
  }

  // ─── Core request method ────────────────────────────────────────────────

  private async request<T>(
    method: string,
    path: string,
    options: RequestOptions = {},
  ): Promise<T> {
    const { params, body, headers: extraHeaders, keepalive } = options;

    // Build URL with query params
    let url = `${this.baseUrl}${path}`;
    if (params) {
      const searchParams = new URLSearchParams();
      for (const [key, value] of Object.entries(params)) {
        if (value !== undefined && value !== null) {
          searchParams.set(key, String(value));
        }
      }
      const qs = searchParams.toString();
      if (qs) url += `?${qs}`;
    }

    // Build headers
    const headers: Record<string, string> = { ...extraHeaders };

    if (body !== undefined && (method === "POST" || method === "PUT" || method === "PATCH")) {
      headers["Content-Type"] = "application/json";
    }

    // Declare the browser's codec/container capabilities so the server's
    // playback waterfall (DirectPlay → DirectStream → Transcode) can
    // pick the cheapest path that this client can actually decode. The
    // value is cached after the first probe so this is a string concat
    // per request, not an MSE probe per request. Returns null in SSR /
    // pre-MSE environments — server falls back to its conservative web
    // defaults in that case, which is the previous behaviour exactly.
    const caps = getClientCapabilitiesHeader();
    if (caps) {
      headers["X-Hubplay-Client-Capabilities"] = caps;
    }

    // Double-submit CSRF token (read from cookie set by the server)
    if (method !== "GET" && method !== "HEAD" && method !== "OPTIONS") {
      const csrfToken = getCookie("hubplay_csrf");
      if (csrfToken) {
        headers["X-CSRF-Token"] = csrfToken;
      }
    }

    // Retry with exponential backoff for 5xx / network errors (up to 2 retries).
    // Definite-assignment assertion (`!`): response is always assigned before
    // the loop terminates (either `break` or `throw`), but TS can't prove it
    // without inspecting the control flow.
    let response!: Response;
    const maxRetries = 2;
    for (let attempt = 0; ; attempt++) {
      try {
        response = await fetch(url, {
          method,
          headers,
          credentials: "include",
          body: body !== undefined ? JSON.stringify(body) : undefined,
          keepalive,
        });
        // Only retry on server errors for idempotent methods
        const isRetryable = response.status >= 500 && (method === "GET" || method === "HEAD");
        if (!isRetryable || attempt >= maxRetries) break;
      } catch (err) {
        // Network error — retry any method (request never reached server)
        if (attempt >= maxRetries) throw err;
      }
      // Exponential backoff: 500ms, 1000ms
      await new Promise((r) => setTimeout(r, 500 * Math.pow(2, attempt)));
    }

    if (!response.ok) {
      // If token expired, try to refresh once
      if (response.status === 401 && path !== "/auth/refresh" && path !== "/auth/login") {
        try {
          await this.refresh();
          // Retry the original request with the new cookie
          const retryResponse = await fetch(url, {
            method,
            headers,
            credentials: "include",
            body: body !== undefined ? JSON.stringify(body) : undefined,
            keepalive,
          });
          if (retryResponse.ok) {
            if (retryResponse.status === 204) return undefined as T;
            const retryJson = await retryResponse.json();
            if (retryJson && typeof retryJson === "object" && "data" in retryJson) {
              return retryJson.data as T;
            }
            return retryJson as T;
          }
        } catch {
          // Refresh failed — clear auth and notify listener
          localStorage.removeItem(USER_KEY);
          this.authListener.onAuthFailure?.();
          throw new ApiError(401, { error: { code: "session_expired", message: "Session expired" } });
        }
        // Retry also failed — clear auth
        localStorage.removeItem(USER_KEY);
        this.authListener.onAuthFailure?.();
        throw new ApiError(401, { error: { code: "session_expired", message: "Session expired" } });
      }

      let errorBody: ApiErrorBody;
      try {
        errorBody = (await response.json()) as ApiErrorBody;
      } catch {
        errorBody = {
          error: {
            code: "unknown_error",
            message: response.statusText || "An unknown error occurred",
          },
        };
      }
      throw new ApiError(response.status, errorBody);
    }

    // 204 No Content
    if (response.status === 204) {
      return undefined as T;
    }

    const json = await response.json();

    // All API responses wrap payloads in {"data": ...}; unwrap automatically.
    if (json && typeof json === 'object' && 'data' in json) {
      return json.data as T;
    }

    return json as T;
  }

  // ─── Auth ─────────────────────────────────────────────────────────────

  async login(username: string, password: string): Promise<AuthResponse> {
    const data = await this.request<AuthResponse>("POST", "/auth/login", {
      body: { username, password },
    });
    // Tokens are now set as HTTP-only cookies by the server.
    // Only persist user info for UI state.
    return data;
  }

  async refresh(): Promise<AuthResponse> {
    // Coalesce concurrent refreshes. Whichever caller arrives first
    // installs a single in-flight promise; every later caller awaits it
    // and observes the same outcome (success cookies, or the same
    // failure that triggers a single onAuthFailure). The promise is
    // cleared as soon as it settles so the *next* genuine 401 a few
    // minutes later starts a fresh request rather than reusing a
    // resolved one.
    if (this.refreshInFlight) {
      return this.refreshInFlight;
    }
    const inflight = this.doRefresh();
    this.refreshInFlight = inflight;
    // Clear the slot once the promise settles. We attach BOTH handlers
    // (success and failure) instead of `.finally()` because a bare
    // `.finally()` returns a new promise that mirrors inflight's
    // rejection — and since no caller awaits that mirror, vitest
    // (and Node) flag it as an unhandled rejection on every failed
    // refresh. With explicit handlers the chain ends here and the
    // original `inflight` rejection is delivered only to whoever
    // awaited `refresh()`.
    const clear = () => {
      if (this.refreshInFlight === inflight) {
        this.refreshInFlight = null;
      }
    };
    inflight.then(clear, clear);
    return inflight;
  }

  private async doRefresh(): Promise<AuthResponse> {
    // Refresh token is sent automatically via HTTP-only cookie.
    const data = await this.request<AuthResponse>("POST", "/auth/refresh", {
      body: {},
    });
    this.authListener.onTokenRefresh?.(data.access_token, data.refresh_token);
    return data;
  }

  async logout(): Promise<void> {
    try {
      // Server reads refresh token from HTTP-only cookie.
      await this.request<void>("POST", "/auth/logout", {
        body: {},
      });
    } finally {
      localStorage.removeItem(USER_KEY);
    }
  }

  // ─── Setup ────────────────────────────────────────────────────────────

  async getSetupStatus(): Promise<SetupStatus> {
    return this.request<SetupStatus>("GET", "/setup/status");
  }

  async setupCreateAdmin(
    username: string,
    password: string,
    displayName?: string,
  ): Promise<AuthResponse> {
    const data = await this.request<AuthResponse>("POST", "/auth/setup", {
      body: { username, password, display_name: displayName },
    });
    return data;
  }

  async browseDirectories(path?: string): Promise<BrowseResponse> {
    return this.request<BrowseResponse>("POST", "/setup/browse", {
      body: path ? { path } : {},
    });
  }

  async setupCreateLibraries(
    libraries: Array<{ name: string; content_type: string; paths: string[] }>,
  ): Promise<Library[]> {
    return this.request<Library[]>("POST", "/setup/libraries", {
      body: { libraries },
    });
  }

  async setupSettings(settings: Record<string, unknown>): Promise<void> {
    return this.request<void>("POST", "/setup/settings", {
      body: settings,
    });
  }

  async setupComplete(startScan = true): Promise<void> {
    return this.request<void>("POST", "/setup/complete", {
      body: { start_scan: startScan },
    });
  }

  async getSystemCapabilities(): Promise<SystemCapabilities> {
    return this.request<SystemCapabilities>("GET", "/setup/capabilities");
  }

  // ─── Users ────────────────────────────────────────────────────────────

  async getMe(): Promise<User> {
    return this.request<User>("GET", "/me");
  }

  async getUsers(): Promise<User[]> {
    return this.request<User[]>("GET", "/users");
  }

  async createUser(data: {
    username: string;
    password: string;
    display_name?: string;
    role?: string;
  }): Promise<User> {
    return this.request<User>("POST", "/users", { body: data });
  }

  async deleteUser(id: string): Promise<void> {
    return this.request<void>("DELETE", `/users/${id}`);
  }

  // ─── Libraries ────────────────────────────────────────────────────────

  async getLibraries(): Promise<Library[]> {
    return this.request<Library[]>("GET", "/libraries");
  }

  async getLibrary(id: string): Promise<Library> {
    return this.request<Library>("GET", `/libraries/${id}`);
  }

  async createLibrary(data: CreateLibraryRequest): Promise<Library> {
    return this.request<Library>("POST", "/libraries", { body: data });
  }

  async updateLibrary(id: string, data: UpdateLibraryRequest): Promise<Library> {
    return this.request<Library>("PUT", `/libraries/${id}`, { body: data });
  }

  async deleteLibrary(id: string): Promise<void> {
    return this.request<void>("DELETE", `/libraries/${id}`);
  }

  async scanLibrary(id: string, refreshMetadata?: boolean): Promise<void> {
    const qs = refreshMetadata ? "?refresh_metadata=true" : "";
    return this.request<void>("POST", `/libraries/${id}/scan${qs}`);
  }

  async browseLibraryDirectories(path?: string): Promise<BrowseResponse> {
    return this.request<BrowseResponse>("POST", "/libraries/browse", {
      body: path ? { path } : {},
    });
  }

  // ─── Items ────────────────────────────────────────────────────────────

  async getItems(params?: {
    library_id?: string;
    type?: string;
    genre?: string;
    sort_by?: string;
    sort_order?: string;
    offset?: number;
    limit?: number;
  }): Promise<PaginatedResponse<MediaItem>> {
    const { library_id, ...rest } = params ?? {};
    if (library_id) {
      return this.request<PaginatedResponse<MediaItem>>("GET", `/libraries/${library_id}/items`, {
        params: rest as Record<string, string | number | boolean | undefined>,
      });
    }
    return this.request<PaginatedResponse<MediaItem>>("GET", "/items/latest", {
      params: rest as Record<string, string | number | boolean | undefined>,
    });
  }

  async getItem(id: string): Promise<ItemDetail> {
    return this.request<ItemDetail>("GET", `/items/${id}`);
  }

  async getItemChildren(id: string): Promise<MediaItem[]> {
    return this.request<MediaItem[]>("GET", `/items/${id}/children`);
  }

  async getPerson(id: string): Promise<PersonDetail> {
    return this.request<PersonDetail>("GET", `/people/${id}`);
  }

  async searchItems(
    q: string,
    type?: string,
    limit?: number,
  ): Promise<PaginatedResponse<MediaItem>> {
    return this.request<PaginatedResponse<MediaItem>>("GET", "/items/search", {
      params: { q, type, limit },
    });
  }

  async getLatestItems(libraryId?: string, limit?: number): Promise<MediaItem[]> {
    const resp = await this.request<PaginatedResponse<MediaItem>>("GET", "/items/latest", {
      params: { library_id: libraryId, limit },
    });
    return resp.items ?? [];
  }

  // ─── Progress / User Data ─────────────────────────────────────────────

  async getProgress(itemId: string): Promise<UserData> {
    return this.request<UserData>("GET", `/me/progress/${itemId}`);
  }

  async updateProgress(
    itemId: string,
    data: {
      position_ticks?: number;
      audio_stream_index?: number;
      subtitle_stream_index?: number;
    },
    /**
     * Pass `{ keepalive: true }` from teardown paths (player unmount,
     * tab close) so the browser commits the request even after the
     * page is gone. Spec caps keepalive payloads at 64 KiB; the body
     * here is well under 1 KiB so it's always safe.
     */
    options: { keepalive?: boolean } = {},
  ): Promise<UserData> {
    return this.request<UserData>("PUT", `/me/progress/${itemId}`, {
      body: data,
      keepalive: options.keepalive,
    });
  }

  async markPlayed(itemId: string): Promise<UserData> {
    return this.request<UserData>("POST", `/me/progress/${itemId}/played`);
  }

  async markUnplayed(itemId: string): Promise<UserData> {
    return this.request<UserData>("POST", `/me/progress/${itemId}/unplayed`);
  }

  async toggleFavorite(itemId: string): Promise<UserData> {
    return this.request<UserData>("POST", `/me/progress/${itemId}/favorite`);
  }

  async getContinueWatching(): Promise<MediaItem[]> {
    return this.request<MediaItem[]>("GET", "/me/continue-watching");
  }

  async getNextUp(): Promise<MediaItem[]> {
    return this.request<MediaItem[]>("GET", "/me/next-up");
  }

  async getFavorites(): Promise<MediaItem[]> {
    return this.request<MediaItem[]>("GET", "/me/favorites");
  }

  // ─── Streaming ────────────────────────────────────────────────────────

  async createStreamSession(
    itemId: string,
    data?: {
      audio_stream_index?: number;
      subtitle_stream_index?: number;
      start_position_ticks?: number;
      max_video_bitrate?: number;
    },
  ): Promise<StreamSession> {
    return this.request<StreamSession>("POST", `/stream/${itemId}`, {
      body: data ?? {},
    });
  }

  async getStreamInfo(itemId: string): Promise<{
    media_streams: import("./types").MediaStream[];
    playback_methods: string[];
  }> {
    return this.request("GET", `/stream/${itemId}/info`);
  }

  // ─── External subtitles (OpenSubtitles, …) ────────────────────────────
  //
  // The search endpoint returns candidates from every registered subtitle
  // provider. The download endpoint isn't fronted here because the
  // browser hits it directly via a `<track>` element — same-origin
  // cookies carry auth, no need for a JS fetch.
  async searchExternalSubtitles(itemId: string, langs?: string[]): Promise<ExternalSubtitleResult[]> {
    const params = langs && langs.length > 0 ? { lang: langs.join(",") } : undefined;
    return this.request<ExternalSubtitleResult[]>("GET", `/stream/${itemId}/subtitles/external`, { params });
  }

  /**
   * Builds the URL for an external subtitle so a `<track>` element can
   * fetch it directly. No JS fetch — the browser handles auth via
   * same-origin cookies, which is exactly the auth model this app uses.
   */
  externalSubtitleURL(itemId: string, source: string, fileID: string): string {
    return `${this.baseUrl}/stream/${itemId}/subtitles/external/${encodeURIComponent(fileID)}?source=${encodeURIComponent(source)}`;
  }

  // ─── Channels / Live TV ───────────────────────────────────────────────

  async getChannels(libraryId?: string): Promise<Channel[]> {
    if (!libraryId) return [];
    return this.request<Channel[]>("GET", `/libraries/${libraryId}/channels`);
  }

  // Channel favorites. Separate from `getFavorites()` above, which lists
  // favorited *items* (movies/episodes) — channels live in their own table
  // and have their own endpoint family.
  async getChannelFavoriteIDs(): Promise<string[]> {
    return this.request<string[]>("GET", "/favorites/channels/ids");
  }

  async getChannelFavorites(): Promise<Channel[]> {
    return this.request<Channel[]>("GET", "/favorites/channels");
  }

  async addChannelFavorite(channelId: string): Promise<void> {
    await this.request<{ channel_id: string; is_favorite: boolean }>(
      "PUT",
      `/favorites/channels/${channelId}`,
    );
  }

  async removeChannelFavorite(channelId: string): Promise<void> {
    await this.request<{ channel_id: string; is_favorite: boolean }>(
      "DELETE",
      `/favorites/channels/${channelId}`,
    );
  }

  // IPTV playlist / EPG refresh (admin-only). These are the correct
  // "scan" actions for a livetv library — filesystem scan doesn't apply.
  //
  // refreshM3U returns 202 Accepted: the import runs detached on the
  // server because large M3U_PLUS feeds blow past the nginx
  // proxy_read_timeout, and a request-bound context cancellation
  // would tear down the DB transaction mid-write. Completion is
  // signalled through SSE (`playlist.refreshed` /
  // `playlist.refresh_failed`); the mutation hook awaits that event.
  async refreshM3U(
    libraryId: string,
  ): Promise<{ library_id: string; status: string }> {
    return this.request<{ library_id: string; status: string }>(
      "POST",
      `/libraries/${libraryId}/iptv/refresh-m3u`,
    );
  }

  async refreshEPG(libraryId: string): Promise<{ programs_imported: number }> {
    return this.request<{ programs_imported: number }>(
      "POST",
      `/libraries/${libraryId}/iptv/refresh-epg`,
    );
  }

  // Preflight probe for an M3U URL — used by the library Add/Edit
  // modals' "Test connection" button so the admin gets a verdict in
  // ~12s instead of clicking Save and waiting up to 5 min in silence.
  // Admin-only on the backend.
  async preflightM3U(input: {
    m3u_url: string;
    tls_insecure: boolean;
  }): Promise<PreflightResult> {
    return this.request<PreflightResult>("POST", "/iptv/preflight", {
      body: input,
    });
  }

  async getChannel(id: string): Promise<Channel> {
    return this.request<Channel>("GET", `/channels/${id}`);
  }

  async getChannelSchedule(
    id: string,
    from?: string,
    to?: string,
  ): Promise<EPGProgram[]> {
    return this.request<EPGProgram[]>("GET", `/channels/${id}/schedule`, {
      params: { from, to },
    });
  }

  async getBulkSchedule(
    channelIds: string[],
    from?: string,
    to?: string,
  ): Promise<Record<string, EPGProgram[]>> {
    if (channelIds.length === 0) return {};

    // Chunk into batches well under the backend cap of 5000 channels
    // per request. The cap exists to bound memory + DB cost on a
    // single roundtrip; chunking on the client lets us serve libraries
    // of any size without hitting the wall AND parallelises the
    // database work since each batch hits an independent connection.
    //
    // Batch size of 1000 is a deliberate compromise:
    //   - Big enough to keep roundtrip overhead low (1 request per
    //     thousand channels, not per channel).
    //   - Small enough that a single failure recovers cheaply and the
    //     payload stays well below the 1 MiB body cap (1000 × ~36-byte
    //     UUID = 36 KiB, plus framing).
    //   - Below the backend cap (5000) by 5x so we never get a
    //     TOO_MANY_CHANNELS even if the cap is later tightened.
    const BATCH_SIZE = 1000;
    if (channelIds.length <= BATCH_SIZE) {
      return this.request<Record<string, EPGProgram[]>>(
        "POST",
        "/channels/schedule",
        { body: { channels: channelIds, from, to } },
      );
    }

    const batches: string[][] = [];
    for (let i = 0; i < channelIds.length; i += BATCH_SIZE) {
      batches.push(channelIds.slice(i, i + BATCH_SIZE));
    }
    const results = await Promise.all(
      batches.map((batch) =>
        this.request<Record<string, EPGProgram[]>>(
          "POST",
          "/channels/schedule",
          { body: { channels: batch, from, to } },
        ),
      ),
    );
    // Merge batch responses. Channel ids never repeat across batches
    // (we sliced them disjoint), so a flat assign is correct — no
    // dedup needed.
    return Object.assign({}, ...results);
  }

  async getChannelGroups(libraryId?: string): Promise<string[]> {
    if (!libraryId) return [];
    return this.request<string[]>("GET", `/libraries/${libraryId}/channels/groups`);
  }

  async getPublicCountries(): Promise<PublicCountry[]> {
    return this.request<PublicCountry[]>("GET", "/iptv/public/countries");
  }

  async getEPGCatalog(): Promise<PublicEPGSource[]> {
    return this.request<PublicEPGSource[]>("GET", "/iptv/epg-catalog");
  }

  async listEPGSources(libraryId: string): Promise<LibraryEPGSource[]> {
    return this.request<LibraryEPGSource[]>(
      "GET",
      `/libraries/${libraryId}/epg-sources`,
    );
  }

  async addEPGSource(
    libraryId: string,
    req: AddEPGSourceRequest,
  ): Promise<LibraryEPGSource> {
    return this.request<LibraryEPGSource>(
      "POST",
      `/libraries/${libraryId}/epg-sources`,
      { body: req },
    );
  }

  async removeEPGSource(libraryId: string, sourceId: string): Promise<void> {
    await this.request<void>(
      "DELETE",
      `/libraries/${libraryId}/epg-sources/${sourceId}`,
    );
  }

  async reorderEPGSources(
    libraryId: string,
    sourceIds: string[],
  ): Promise<LibraryEPGSource[]> {
    return this.request<LibraryEPGSource[]>(
      "PATCH",
      `/libraries/${libraryId}/epg-sources/reorder`,
      { body: { source_ids: sourceIds } },
    );
  }

  // IPTV scheduled jobs (admin). List is readable with ACL; the
  // mutations are admin-only at the route level.
  async listScheduledJobs(libraryId: string): Promise<IPTVScheduledJob[]> {
    return this.request<IPTVScheduledJob[]>(
      "GET",
      `/libraries/${libraryId}/schedule`,
    );
  }

  async upsertScheduledJob(
    libraryId: string,
    kind: IPTVScheduledJobKind,
    req: UpsertScheduledJobRequest,
  ): Promise<IPTVScheduledJob> {
    return this.request<IPTVScheduledJob>(
      "PUT",
      `/libraries/${libraryId}/schedule/${kind}`,
      { body: req },
    );
  }

  async deleteScheduledJob(
    libraryId: string,
    kind: IPTVScheduledJobKind,
  ): Promise<void> {
    await this.request<void>(
      "DELETE",
      `/libraries/${libraryId}/schedule/${kind}`,
    );
  }

  async runScheduledJobNow(
    libraryId: string,
    kind: IPTVScheduledJobKind,
  ): Promise<IPTVScheduledJob | null> {
    // Handler returns 204 when no row exists yet — the client answers
    // null so callers can refetch the list without special-casing.
    return this.request<IPTVScheduledJob | null>(
      "POST",
      `/libraries/${libraryId}/schedule/${kind}/run`,
    );
  }

  // Continue-watching: beacon fired by the live player on first-play
  // + rail query for Discover. Failures on the beacon are non-fatal;
  // the caller logs and moves on.
  async recordChannelWatch(
    channelId: string,
  ): Promise<{ channel_id: string; last_watched_at: string }> {
    return this.request<{ channel_id: string; last_watched_at: string }>(
      "POST",
      `/channels/${channelId}/watch`,
    );
  }

  // Playback-failure beacon: fired by the live player when hls.js
  // raises a fatal error. The server forwards this into the same
  // channel-health pipeline the proxy uses, so a flapping client
  // contributes to the dead-channel signal alongside the active
  // prober. Failures here are non-fatal — the player has already
  // failed, the beacon is just telemetry.
  async reportPlaybackFailure(
    channelId: string,
    kind: "manifest" | "network" | "media" | "timeout" | "unknown",
    details?: string,
  ): Promise<{
    channel_id: string;
    recorded: boolean;
    consecutive_failures?: number;
    health_status?: "ok" | "degraded" | "dead";
    unhealthy_threshold?: number;
    reason?: string;
  }> {
    return this.request(
      "POST",
      `/channels/${channelId}/playback-failure`,
      // keepalive so the beacon survives the user immediately
      // changing the channel, closing the modal, or even the tab —
      // the player is the most likely place for "I'm leaving NOW"
      // teardown to race the request.
      { body: { kind, details }, keepalive: true },
    );
  }

  async listContinueWatchingChannels(
    limit?: number,
  ): Promise<ContinueWatchingChannel[]> {
    return this.request<ContinueWatchingChannel[]>(
      "GET",
      "/me/channels/continue-watching",
      limit ? { params: { limit } } : undefined,
    );
  }

  async listUnhealthyChannels(
    libraryId: string,
    threshold?: number,
  ): Promise<UnhealthyChannel[]> {
    return this.request<UnhealthyChannel[]>(
      "GET",
      `/libraries/${libraryId}/channels/unhealthy`,
      threshold ? { params: { threshold } } : undefined,
    );
  }

  async resetChannelHealth(channelId: string): Promise<void> {
    await this.request<void>("POST", `/channels/${channelId}/reset-health`);
  }

  async disableChannel(channelId: string): Promise<void> {
    await this.request<void>("POST", `/channels/${channelId}/disable`);
  }

  async enableChannel(channelId: string): Promise<void> {
    await this.request<void>("POST", `/channels/${channelId}/enable`);
  }

  async getMyPreferences(): Promise<Record<string, string>> {
    return this.request<Record<string, string>>("GET", "/me/preferences");
  }

  async setMyPreference(key: string, value: string): Promise<void> {
    await this.request<void>(
      "PUT",
      `/me/preferences/${encodeURIComponent(key)}`,
      { body: { value } },
    );
  }

  async listChannelsWithoutEPG(libraryId: string): Promise<ChannelWithoutEPG[]> {
    return this.request<ChannelWithoutEPG[]>(
      "GET",
      `/libraries/${libraryId}/channels/without-epg`,
    );
  }

  async patchChannel(
    channelId: string,
    req: PatchChannelRequest,
  ): Promise<ChannelWithoutEPG> {
    return this.request<ChannelWithoutEPG>(
      "PATCH",
      `/channels/${channelId}`,
      { body: req },
    );
  }

  async importPublicIPTV(country: string, name?: string): Promise<ImportPublicIPTVResponse> {
    return this.request<ImportPublicIPTVResponse>("POST", "/iptv/public/import", {
      body: { country, name },
    });
  }

  // ─── Providers ──────────────────────────────────────────────────────

  async getProviders(): Promise<
    Array<{
      name: string;
      type: string;
      status: string;
      priority: number;
      has_api_key: boolean;
      config?: Record<string, string>;
    }>
  > {
    return this.request("GET", "/providers");
  }

  async updateProvider(
    name: string,
    data: { api_key?: string; status?: string; priority?: number; config?: Record<string, string> },
  ): Promise<{ name: string; status: string; priority: number }> {
    return this.request("PUT", `/providers/${name}`, { body: data });
  }

  // ─── Federation (server peering) ──────────────────────────────────────

  async getServerIdentity(): Promise<import("./types").FederationServerInfo> {
    return this.request("GET", "/admin/peers/identity");
  }

  async listPeers(): Promise<import("./types").FederationPeer[]> {
    return this.request("GET", "/admin/peers");
  }

  async revokePeer(id: string): Promise<void> {
    return this.request<void>("DELETE", `/admin/peers/${id}`);
  }

  async probePeer(baseURL: string): Promise<import("./types").FederationServerInfo> {
    return this.request("POST", "/admin/peers/probe", { body: { base_url: baseURL } });
  }

  async acceptInvite(baseURL: string, code: string): Promise<import("./types").FederationPeer> {
    return this.request("POST", "/admin/peers/accept", { body: { base_url: baseURL, code } });
  }

  async listInvites(): Promise<import("./types").FederationInvite[]> {
    return this.request("GET", "/admin/peers/invites");
  }

  async generateInvite(): Promise<import("./types").FederationInvite> {
    return this.request("POST", "/admin/peers/invites", { body: {} });
  }

  async listPeerShares(peerID: string): Promise<import("./types").FederationLibraryShare[]> {
    return this.request("GET", `/admin/peers/${peerID}/shares`);
  }

  async createPeerShare(
    peerID: string,
    data: {
      library_id: string;
      can_browse: boolean;
      can_play: boolean;
      can_download: boolean;
      can_livetv: boolean;
    },
  ): Promise<import("./types").FederationLibraryShare> {
    return this.request("POST", `/admin/peers/${peerID}/shares`, { body: data });
  }

  async deletePeerShare(peerID: string, shareID: string): Promise<void> {
    return this.request<void>("DELETE", `/admin/peers/${peerID}/shares/${shareID}`);
  }

  // ─── Images ────────────────────────────────────────────────────────────

  async getItemImages(itemId: string): Promise<ImageInfo[]> {
    return this.request<ImageInfo[]>("GET", `/items/${itemId}/images`);
  }

  async getAvailableImages(itemId: string, type?: string): Promise<AvailableImage[]> {
    return this.request<AvailableImage[]>("GET", `/items/${itemId}/images/available`, {
      params: { type },
    });
  }

  async selectImage(itemId: string, type: string, data: { url: string; width: number; height: number }): Promise<ImageInfo> {
    return this.request<ImageInfo>("PUT", `/items/${itemId}/images/${type}/select`, { body: data });
  }

  async uploadImage(itemId: string, type: string, file: File): Promise<ImageInfo> {
    const formData = new FormData();
    formData.append("file", file);

    const uploadHeaders: Record<string, string> = {};
    const csrfToken = getCookie("hubplay_csrf");
    if (csrfToken) {
      uploadHeaders["X-CSRF-Token"] = csrfToken;
    }

    const response = await fetch(`${this.baseUrl}/items/${itemId}/images/${type}/upload`, {
      method: "POST",
      credentials: "include",
      headers: uploadHeaders,
      body: formData,
    });

    if (!response.ok) {
      throw new Error("Upload failed");
    }

    const json = await response.json();
    if (json && typeof json === "object" && "data" in json) {
      return json.data as ImageInfo;
    }
    return json as ImageInfo;
  }

  async setImagePrimary(itemId: string, imageId: string): Promise<ImageInfo> {
    return this.request<ImageInfo>("PUT", `/items/${itemId}/images/${imageId}/primary`);
  }

  async deleteImage(itemId: string, imageId: string): Promise<void> {
    return this.request<void>("DELETE", `/items/${itemId}/images/${imageId}`);
  }

  async refreshLibraryImages(libraryId: string): Promise<{ updated: number }> {
    return this.request<{ updated: number }>("POST", `/libraries/${libraryId}/images/refresh`);
  }

  // ─── System ───────────────────────────────────────────────────────────

  async getHealth(): Promise<HealthResponse> {
    return this.request<HealthResponse>("GET", "/health");
  }

  // Rich admin-only system snapshot. Backed by /admin/system/stats —
  // separate from /health because that one has to stay tiny for ops
  // tooling (Docker healthcheck, k8s liveness) while the panel can grow.
  //
  // Note: request<T> auto-unwraps the {"data": ...} envelope, so we
  // type T as the inner payload, not the envelope.
  async getSystemStats(): Promise<SystemStats> {
    return this.request<SystemStats>("GET", "/admin/system/stats");
  }

  // Runtime settings the admin can edit from the panel — replaces the
  // need to SSH into the host to change server.base_url or the
  // hardware-acceleration flags. The endpoint is whitelisted on the
  // server so a typo in `key` is rejected before touching the DB.
  async getSystemSettings(): Promise<SystemSettingsResponse> {
    return this.request<SystemSettingsResponse>("GET", "/admin/system/settings");
  }

  async updateSystemSetting(key: string, value: string): Promise<SystemSettingsResponse> {
    return this.request<SystemSettingsResponse>("PUT", "/admin/system/settings", {
      body: { key, value },
    });
  }

  async resetSystemSetting(key: string): Promise<SystemSettingsResponse> {
    return this.request<SystemSettingsResponse>(
      "DELETE",
      `/admin/system/settings/${encodeURIComponent(key)}`,
    );
  }

  // ─── Admin: signing keys ──────────────────────────────────────────────

  async listAuthKeys(): Promise<AuthKey[]> {
    return this.request<AuthKey[]>("GET", "/admin/auth/keys");
  }

  async rotateAuthKey(overlapSeconds?: number): Promise<RotateAuthKeyResponse> {
    const body = overlapSeconds === undefined ? undefined : { overlap_seconds: overlapSeconds };
    return this.request<RotateAuthKeyResponse>("POST", "/admin/auth/keys/rotate", { body });
  }

  async pruneAuthKeys(beforeSeconds?: number): Promise<{ pruned: number }> {
    const body = beforeSeconds === undefined ? undefined : { before_seconds: beforeSeconds };
    return this.request<{ pruned: number }>("POST", "/admin/auth/keys/prune", { body });
  }
}

export const api = new ApiClient("/api/v1");
