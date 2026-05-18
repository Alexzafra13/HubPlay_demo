import type {
  AddEPGSourceRequest,
  AuthResponse,
  AvailableImage,
  BrowseResponse,
  Channel,
  ChannelOrderRequest,
  ChannelWithoutEPG,
  ChannelHealthSummary,
  ContinueWatchingChannel,
  CreateLibraryRequest,
  EPGProgram,
  HealthResponse,
  HomeLayout,
  HomeLiveNowChannel,
  HomeBecauseResponse,
  HomeRecommendedItem,
  HomeTrendingItem,
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
  AdminStreamSession,
  AdminStreamActivityResponse,
  AdminTopItemsResponse,
  SystemSettingsResponse,
  SystemStats,
  AuthKey,
  RotateAuthKeyResponse,
  AdminDatabaseProfiles,
  AdminDatabaseStatus,
  AdminDatabaseTestRequest,
  AdminDatabaseTestResponse,
  AdminDatabaseSaveRequest,
  AdminDatabaseSaveResponse,
  AdminDatabaseMigrateRequest,
  UnhealthyChannel,
  UpdateLibraryRequest,
  UpsertScheduledJobRequest,
  User,
  CreateUserResponse,
  MySession,
  ProfileSummary,
  ResetPasswordResponse,
  UserLibraryAccess,
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

// Hard ceiling on a single fetch attempt. Without a timeout, iOS WebKit
// silently parks XHR/fetch promises when the app backgrounds or the
// network transitions (LTE↔Wi-Fi, captive portal, idle TCP cleanup),
// and the promise never settles — which manifests as a permanent
// "Cargando…" spinner because TanStack Query only flips to isError on a
// rejection. 30s is well above any healthy round-trip (admin folder
// browse over a Windows-Docker bind-mount peaks at ~2s) and short
// enough that a stuck request fails loudly instead of silently.
const REQUEST_TIMEOUT_MS = 30_000;

async function fetchWithTimeout(
  url: string,
  init: RequestInit,
  timeoutMs: number,
): Promise<Response> {
  const controller = new AbortController();
  const id = setTimeout(() => controller.abort(), timeoutMs);
  try {
    return await fetch(url, { ...init, signal: controller.signal });
  } finally {
    clearTimeout(id);
  }
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

    // Para FormData (avatares u otros uploads multipart) NO ponemos
    // Content-Type: el navegador genera el boundary automáticamente y
    // sobreescribirlo aquí lo rompe. Y tampoco hacemos JSON.stringify
    // — lo pasamos en bruto a fetch.
    const isFormData = typeof FormData !== "undefined" && body instanceof FormData;
    if (body !== undefined && !isFormData && (method === "POST" || method === "PUT" || method === "PATCH")) {
      headers["Content-Type"] = "application/json";
    }
    const serializedBody: BodyInit | undefined =
      body === undefined ? undefined : isFormData ? (body as FormData) : JSON.stringify(body);

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
        response = await fetchWithTimeout(
          url,
          {
            method,
            headers,
            credentials: "include",
            body: serializedBody,
            keepalive,
          },
          REQUEST_TIMEOUT_MS,
        );
        // Only retry on server errors for idempotent methods
        const isRetryable = response.status >= 500 && (method === "GET" || method === "HEAD");
        if (!isRetryable || attempt >= maxRetries) break;
      } catch (err) {
        // Network error or timeout — retry any method (request never
        // reached server, so re-sending is safe even for non-idempotent
        // methods).
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
          const retryResponse = await fetchWithTimeout(
            url,
            {
              method,
              headers,
              credentials: "include",
              body: body !== undefined ? JSON.stringify(body) : undefined,
              keepalive,
            },
            REQUEST_TIMEOUT_MS,
          );
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

  /** Create the bootstrap admin. Pass `password = ""` to let the
   *  server auto-generate a temp password — the response then
   *  carries `generated_password` once for the wizard to surface
   *  in the completion step. */
  async setupCreateAdmin(
    username: string,
    password: string,
    displayName?: string,
  ): Promise<AuthResponse & { generated_password?: string }> {
    const data = await this.request<AuthResponse & { generated_password?: string }>(
      "POST",
      "/auth/setup",
      {
        body: { username, password, display_name: displayName },
      },
    );
    return data;
  }

  async browseDirectories(path?: string): Promise<BrowseResponse> {
    return this.request<BrowseResponse>("GET", "/setup/browse", {
      params: path ? { path } : undefined,
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
    /** Optional. When omitted the server generates a temporary password
     *  and returns it once in the response under `generated_password`. */
    password?: string;
    display_name?: string;
    role?: string;
    /** Optional. When non-empty, the server attaches library_access
     *  grants to the new user in the same request. Only valid for
     *  top-level accounts; profile creation rejects this with 400
     *  because grants belong to the parent (ADR-014). */
    grant_library_ids?: string[];
  }): Promise<CreateUserResponse> {
    return this.request<CreateUserResponse>("POST", "/users", { body: data });
  }

  async deleteUser(id: string): Promise<void> {
    return this.request<void>("DELETE", `/users/${id}`);
  }

  /** Admin-only. Generates a fresh temporary password for the target
   *  user, returns it exactly once for the admin to hand off. The
   *  user's must-change flag is set so first login lands on the
   *  ChangePassword screen. */
  async resetUserPassword(id: string): Promise<ResetPasswordResponse> {
    return this.request<ResetPasswordResponse>(
      "POST",
      `/users/${id}/reset-password`,
    );
  }

  /** Self password change. The current password may be empty when the
   *  user is completing a forced rotation (server skips the compare
   *  in that case because the user just authenticated with the
   *  temporary password). */
  async changeMyPassword(currentPassword: string, newPassword: string): Promise<void> {
    return this.request<void>("POST", "/me/password", {
      body: { current_password: currentPassword, new_password: newPassword },
    });
  }

  /** List the profiles under the current account (parent + children).
   *  Used by the "Who's watching?" screen when the frontend lands
   *  via cookie refresh and doesn't have a fresh login response to
   *  consume. */
  async listProfiles(): Promise<ProfileSummary[]> {
    return this.request<ProfileSummary[]>("GET", "/me/profiles");
  }

  /** Switch into a sibling / parent profile. Returns a fresh auth
   *  token for the target. PIN is only required when the target
   *  profile has one set. */
  async switchProfile(profileId: string, pin?: string): Promise<{
    user: User;
    profiles?: ProfileSummary[];
  }> {
    return this.request<{ user: User; profiles?: ProfileSummary[] }>(
      "POST",
      "/auth/switch-profile",
      { body: { profile_id: profileId, pin: pin ?? "" } },
    );
  }

  /** List the caller's active auth sessions ("Tus dispositivos"). */
  async listMySessions(): Promise<MySession[]> {
    return this.request<MySession[]>("GET", "/me/sessions");
  }

  /** Revoke a single auth session (must belong to the caller; the
   *  server returns 404 for foreign sessions to avoid leaking
   *  existence of other users' rows). Revoking the caller's own
   *  session also clears the auth cookies server-side, so the
   *  next request lands on /login cleanly. */
  async revokeMySession(sessionId: string): Promise<void> {
    await this.request<void>("DELETE", `/me/sessions/${sessionId}`);
  }

  // ─── Admin DB backup ───────────────────────────────────────────────

  /** Stream the SQLite snapshot from /admin/system/backup as a Blob.
   *  Caller is responsible for triggering a download (object URL +
   *  anchor click). Sized as a Blob — no JSON wrapping — because the
   *  endpoint serves application/octet-stream and we want to bypass
   *  the standard `request()` JSON decoder. */
  async downloadBackup(): Promise<Blob> {
    const res = await fetch(`${this.baseUrl}/admin/system/backup`, {
      method: "GET",
      credentials: "include",
    });
    if (!res.ok) {
      throw new Error(`backup download failed: ${res.status}`);
    }
    return await res.blob();
  }

  /** Upload a backup file to /admin/system/backup/restore. Server
   *  stages it for application on the next process restart — the
   *  response carries a hint to that effect. */
  async restoreBackup(file: File): Promise<{
    staged: boolean;
    size_bytes: number;
    uploaded_filename: string;
    applies_on: string;
  }> {
    const fd = new FormData();
    fd.append("backup", file);
    const headers: Record<string, string> = {};
    const csrf = getCookie("hubplay_csrf");
    if (csrf) headers["X-CSRF-Token"] = csrf;
    const res = await fetch(`${this.baseUrl}/admin/system/backup/restore`, {
      method: "POST",
      credentials: "include",
      headers,
      body: fd,
    });
    if (!res.ok) {
      let msg = `restore upload failed: ${res.status}`;
      try {
        const body = (await res.json()) as { error?: { message?: string } };
        if (body?.error?.message) msg = body.error.message;
      } catch {
        // body wasn't JSON; keep the default message
      }
      throw new Error(msg);
    }
    const json = (await res.json()) as { data: {
      staged: boolean; size_bytes: number; uploaded_filename: string; applies_on: string;
    } };
    return json.data;
  }

  /** Admin profile creation. Wraps POST /users with parent_user_id
   *  set so the server creates a child row. Password / username are
   *  ignored on the wire — the server synthesises them. */
  async createProfile(parentUserId: string, displayName: string): Promise<CreateUserResponse> {
    return this.request<CreateUserResponse>("POST", "/users", {
      body: {
        parent_user_id: parentUserId,
        display_name: displayName,
      },
    });
  }

  /** Sets or clears (empty string) a profile's PIN. Caller must be
   *  the parent of the profile or an admin. */
  async setUserPIN(userId: string, pin: string): Promise<void> {
    return this.request<void>("PUT", `/users/${userId}/pin`, {
      body: { pin },
    });
  }

  /** Sets or clears (empty string) a profile's content cap. Empty
   *  rating means "no restriction". Admin-only — caller gate is
   *  enforced server-side. */
  async setUserContentRating(userId: string, rating: string): Promise<void> {
    return this.request<void>("PUT", `/users/${userId}/content-rating`, {
      body: { rating },
    });
  }

  /** Rename a user (admin) or one of your own profile children
   *  (parent of profile). Server enforces the same matrix the SetPIN
   *  endpoint uses: admin OR parent-of-target OR self. */
  async setUserDisplayName(userId: string, displayName: string): Promise<void> {
    return this.request<void>("PUT", `/users/${userId}/display-name`, {
      body: { display_name: displayName },
    });
  }

  /** Set / clear the avatar colour override. Empty hex clears the
   *  override → frontend falls back to the deterministic palette.
   *  Same matrix as setUserDisplayName. */
  async setUserAvatarColor(userId: string, hex: string): Promise<void> {
    return this.request<void>("PUT", `/users/${userId}/avatar-color`, {
      body: { avatar_color: hex },
    });
  }

  /** Sube una imagen como avatar del usuario autenticado. El navegador
   *  pone el boundary multipart automáticamente cuando el body es un
   *  FormData; nosotros sólo metemos el File en el campo "avatar". */
  async uploadMyAvatar(file: File): Promise<{ avatar_image_url: string }> {
    const form = new FormData();
    form.append("avatar", file);
    return this.request<{ avatar_image_url: string }>("POST", "/me/avatar", {
      body: form,
    });
  }

  /** Quita el avatar subido. Idempotente: 204 también si no había. */
  async deleteMyAvatar(): Promise<void> {
    return this.request<void>("DELETE", "/me/avatar");
  }

  /** Promote / demote between user and admin. The primary admin
   *  (oldest by created_at) is gated server-side and returns 403. */
  async setUserRole(userId: string, role: "user" | "admin"): Promise<void> {
    return this.request<void>("PUT", `/users/${userId}/role`, {
      body: { role },
    });
  }

  /** Soft-disable / re-enable a user. is_active=false rejects login
   *  and middleware; the row stays put so flipping back true
   *  restores access without a recovery flow. */
  async setUserActive(userId: string, isActive: boolean): Promise<void> {
    return this.request<void>("PUT", `/users/${userId}/active`, {
      body: { is_active: isActive },
    });
  }

  /** Sets a temporary access window in days; 0 (or omitted) clears
   *  the deadline = permanent access. Server computes the actual
   *  expiry timestamp as now + N days. */
  async setUserAccess(userId: string, durationDays: number): Promise<void> {
    return this.request<void>("PUT", `/users/${userId}/access`, {
      body: { duration_days: durationDays },
    });
  }

  /** Reads the library_access grants for a user. Profile ids are
   *  normalised to their parent server-side; the response flags this
   *  with `is_inherited` so the UI can render a read-only inherited
   *  view without an extra round-trip to look up the parent. */
  async getUserLibraryAccess(userId: string): Promise<UserLibraryAccess> {
    return this.request<UserLibraryAccess>(
      "GET",
      `/users/${userId}/library-access`,
    );
  }

  /** Replaces the user's library_access grant set in one transactional
   *  diff. Passing `[]` clears every grant. Only valid against
   *  top-level accounts — profile targets are rejected with 400. */
  async setUserLibraryAccess(
    userId: string,
    libraryIds: string[],
  ): Promise<void> {
    return this.request<void>("PUT", `/users/${userId}/library-access`, {
      body: { library_ids: libraryIds },
    });
  }

  /** Shortcut for "give user X their own IPTV list": creates a
   *  livetv library with the supplied M3U/EPG and grants access ONLY
   *  to this user in one server-side transaction. Saves the admin
   *  from the two-step "create lib at /admin/libraries → tick the
   *  checkbox in /admin/users" dance. Returns the created library. */
  async createPersonalIPTVLibrary(
    userId: string,
    data: {
      name: string;
      m3u_url: string;
      epg_url?: string;
      language_filter?: string[];
      tls_insecure?: boolean;
    },
  ): Promise<Library> {
    return this.request<Library>("POST", `/users/${userId}/iptv-libraries`, {
      body: data,
    });
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
    return this.request<BrowseResponse>("GET", "/libraries/browse", {
      params: path ? { path } : undefined,
    });
  }

  // ─── Items ────────────────────────────────────────────────────────────

  async getItems(params?: {
    library_id?: string;
    type?: string;
    genre?: string;
    year_from?: number;
    year_to?: number;
    min_rating?: number;
    q?: string;
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
    // Cross-library browse — `/items` is the paginated endpoint that
    // mirrors `/libraries/{id}/items`. We used to fall back to
    // `/items/latest` here, but that route caps at 50 results and
    // doesn't paginate, so the Movies/Series grids appeared truncated
    // for any catalogue beyond 50 items.
    return this.request<PaginatedResponse<MediaItem>>("GET", "/items", {
      params: rest as Record<string, string | number | boolean | undefined>,
    });
  }

  async getItem(id: string): Promise<ItemDetail> {
    return this.request<ItemDetail>("GET", `/items/${id}`);
  }

  async getItemChildren(id: string): Promise<MediaItem[]> {
    return this.request<MediaItem[]>("GET", `/items/${id}/children`);
  }

  // "More like this" rail. Backend calls TMDb /recommendations and
  // cross-references each candidate against the user's library so each
  // entry is marked in_library + local_id when the user already has
  // it. Empty list when the item has no TMDb match — the rail just
  // hides itself in that case.
  async getItemRecommendations(id: string): Promise<{ items: import("./types").Recommendation[] }> {
    return this.request<{ items: import("./types").Recommendation[] }>(
      "GET",
      `/items/${id}/recommendations`,
    );
  }

  // Admin: candidatos TMDb para reidentificar un item. La query y el año
  // son opcionales — sin ellos el backend usa el título y año actuales
  // del item como semilla. Cada candidato trae poster_url para que el
  // diálogo pueda renderizar la lista visual estilo Plex/Jellyfin.
  async getIdentifyCandidates(
    id: string,
    options?: { query?: string; year?: number },
  ): Promise<import("./types").IdentifyCandidate[]> {
    return this.request<import("./types").IdentifyCandidate[]>(
      "GET",
      `/items/${id}/identify/candidates`,
      {
        params: {
          query: options?.query,
          year: options?.year,
        },
      },
    );
  }

  // Admin: edición manual de metadatos. Bloquea el item al guardar
  // para que el siguiente "Refresh metadata" no pise la edición.
  // Todos los campos son opcionales; sólo los suministrados se aplican.
  async updateItemMetadata(
    id: string,
    patch: {
      title?: string;
      original_title?: string;
      year?: number;
      overview?: string;
      tagline?: string;
    },
  ): Promise<{
    item_id: string;
    title: string;
    original_title: string;
    year: number;
    metadata_locked: boolean;
  }> {
    return this.request("PATCH", `/items/${id}/metadata`, { body: patch });
  }

  // Admin: toggle del candado de metadatos sin tocar el contenido.
  async setItemMetadataLock(
    id: string,
    locked: boolean,
  ): Promise<{ item_id: string; metadata_locked: boolean }> {
    return this.request("PUT", `/items/${id}/metadata/lock`, {
      body: { locked },
    });
  }

  // Admin: busca logos en la base pública de iptv-org y los aplica
  // como overrides URL a los canales sin logo. Devuelve el count.
  async refreshLogosFromIPTVOrg(libraryId: string): Promise<{ library_id: string; updated: number }> {
    return this.request(
      "POST",
      `/libraries/${encodeURIComponent(libraryId)}/iptv/refresh-logos-from-iptv-org`,
    );
  }

  // Admin: aplica un match TMDb concreto al item. El backend borra
  // imágenes y metadata previos y reescribe título, overview, géneros,
  // estudio, reparto e imágenes con los del externalID elegido.
  async applyIdentify(
    id: string,
    payload: { provider?: string; external_id: string },
  ): Promise<{ item_id: string; provider: string; external_id: string }> {
    return this.request<{
      item_id: string;
      provider: string;
      external_id: string;
    }>("POST", `/items/${id}/identify`, {
      body: { provider: payload.provider ?? "tmdb", external_id: payload.external_id },
    });
  }

  async getPerson(id: string): Promise<PersonDetail> {
    return this.request<PersonDetail>("GET", `/people/${id}`);
  }

  // Studio browse + detail. The detail endpoint returns the studio
  // header (logo, name) plus every item the catalogue has from this
  // studio, sorted year-desc. Drives the click-on-the-studio-mark
  // collection page on /studios/{slug}.
  async getStudios(): Promise<{ studios: import("./types").StudioListEntry[] }> {
    return this.request<{ studios: import("./types").StudioListEntry[] }>(
      "GET",
      "/studios",
    );
  }

  async getStudio(slug: string): Promise<import("./types").StudioDetail> {
    return this.request<import("./types").StudioDetail>(
      "GET",
      `/studios/${encodeURIComponent(slug)}`,
    );
  }

  // Movie collections (Jellyfin-style sagas). Detail endpoint resolves
  // the canonical "collection:<tmdb_id>" id directly — no slug
  // encoding needed (the id is colon-separated lowercase ASCII so
  // it's URL-safe out of the box).
  async getCollections(): Promise<{
    collections: import("./types").CollectionListEntry[];
  }> {
    return this.request<{ collections: import("./types").CollectionListEntry[] }>(
      "GET",
      "/collections",
    );
  }

  async getCollection(
    id: string,
  ): Promise<import("./types").CollectionDetail> {
    return this.request<import("./types").CollectionDetail>(
      "GET",
      `/collections/${encodeURIComponent(id)}`,
    );
  }

  // Backend returns { data: MediaItem[], total: N } and our `request`
  // helper auto-unwraps the `data` envelope, so the actual resolved
  // value is the array — not a PaginatedResponse. Earlier callers
  // typed this as PaginatedResponse and reached for `.items`, which
  // silently produced empty results in the global search page.
  // Returning MediaItem[] matches what the wire actually delivers.
  async searchItems(
    q: string,
    type?: string,
    limit?: number,
  ): Promise<MediaItem[]> {
    return this.request<MediaItem[]>("GET", "/items/search", {
      params: { q, type, limit },
    });
  }

  // Catalogue-wide genre vocabulary for the filter panel. Cached
  // separately from items so the chip list doesn't keep flickering as
  // items pages stream in. Optional `type` scopes to movies-only or
  // series-only — empty = full union.
  async getGenres(type?: string): Promise<{ name: string; count: number }[]> {
    return this.request<{ name: string; count: number }[]>("GET", "/items/genres", {
      params: { type },
    });
  }

  async getLatestItems(
    libraryId?: string,
    limit?: number,
    type?: "movie" | "series" | "season" | "episode",
  ): Promise<MediaItem[]> {
    const resp = await this.request<PaginatedResponse<MediaItem>>("GET", "/items/latest", {
      params: { library_id: libraryId, limit, type },
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

  async removeFromContinueWatching(itemId: string): Promise<void> {
    await this.request<void>("DELETE", `/me/continue-watching/${itemId}`);
  }

  async getNextUp(): Promise<MediaItem[]> {
    return this.request<MediaItem[]>("GET", "/me/next-up");
  }

  async getFavorites(): Promise<MediaItem[]> {
    return this.request<MediaItem[]>("GET", "/me/favorites");
  }

  // ─── Home (configurable home page) ────────────────────────────────────

  async getHomeLayout(): Promise<HomeLayout> {
    return this.request<HomeLayout>("GET", "/me/home/layout");
  }

  async putHomeLayout(layout: HomeLayout): Promise<HomeLayout> {
    return this.request<HomeLayout>("PUT", "/me/home/layout", { body: layout });
  }

  async getHomeTrending(limit?: number): Promise<HomeTrendingItem[]> {
    const resp = await this.request<{ items: HomeTrendingItem[]; total: number }>(
      "GET",
      "/me/home/trending",
      { params: { limit } },
    );
    return resp.items ?? [];
  }

  async getHomeRecommended(limit?: number): Promise<HomeRecommendedItem[]> {
    const resp = await this.request<{ items: HomeRecommendedItem[]; total: number }>(
      "GET",
      "/me/home/recommended",
      { params: { limit } },
    );
    return resp.items ?? [];
  }

  /** "Porque viste X" rail. Returns the seed (most recently
   *  completed watch) plus items sharing genres with it. seed is
   *  null when the caller has no completed watches yet — caller
   *  hides the rail. */
  async getHomeBecauseYouWatched(limit?: number): Promise<HomeBecauseResponse> {
    return this.request<HomeBecauseResponse>(
      "GET",
      "/me/home/because-you-watched",
      { params: { limit } },
    );
  }

  async getHomeLiveNow(limit?: number): Promise<HomeLiveNowChannel[]> {
    const resp = await this.request<{ items: HomeLiveNowChannel[]; total: number }>(
      "GET",
      "/me/home/live-now",
      { params: { limit } },
    );
    return resp.items ?? [];
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

  // stopStreamSession releases the server-side ffmpeg session and frees its
  // slot. Called from player teardown (close button, episode switch) and from
  // the pagehide/usePlayback cleanup hook so the user closing the tab doesn't
  // leak ~90s of CPU to the idle reaper.
  //
  // `keepalive: true` lets the request survive page unload (covers pagehide
  // / bfcache / iOS Safari). Routing it through `request` instead of a raw
  // fetch picks up the standard CSRF double-submit (X-CSRF-Token header from
  // the hubplay_csrf cookie) — without it the DELETE 403'd in production and
  // we depended on the idle reaper to clean up. Errors are swallowed at the
  // call sites; the reaper is still the safety net if even keepalive drops.
  async stopStreamSession(itemId: string): Promise<void> {
    return this.request<void>("DELETE", `/stream/${itemId}/session`, {
      keepalive: true,
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

  // ─── Federated subtitles (HubPlay peer) ───────────────────────────────
  //
  // When the user is playing a federated item, embedded subtitles live
  // on the remote server's filesystem. We expose them via a session-
  // keyed proxy: list once at mount, then a `<track>` element fetches
  // each picked .vtt directly. Same auth shape as the external subs
  // path — same-origin cookies do the work, no JS fetch required.
  async listFederatedSubtitles(
    peerId: string,
    sessionId: string,
  ): Promise<Array<{
    index: number;
    codec: string;
    language: string;
    title: string;
    forced: boolean;
    default: boolean;
  }>> {
    return this.request("GET", `/me/peers/${encodeURIComponent(peerId)}/stream/session/${encodeURIComponent(sessionId)}/subtitles`);
  }

  /**
   * Builds the URL for a federated subtitle so a `<track>` element can
   * fetch it directly. The proxy keeps the request same-origin so the
   * peer's hostname stays invisible to the browser.
   */
  federatedSubtitleURL(peerId: string, sessionId: string, trackIndex: number): string {
    return `${this.baseUrl}/me/peers/${encodeURIComponent(peerId)}/stream/session/${encodeURIComponent(sessionId)}/subtitles/${trackIndex}`;
  }

  // ─── Channels / Live TV ───────────────────────────────────────────────

  async getChannels(libraryId?: string): Promise<Channel[]> {
    if (!libraryId) return [];
    return this.request<Channel[]>("GET", `/libraries/${libraryId}/channels`);
  }

  /** Personalisation-panel variant: returns every channel the user
   *  can access (including their hidden ones) plus their `hidden` +
   *  `user_position` fields so the panel can render the full editable
   *  list. The regular Live TV view uses getChannels() instead. */
  /** Admin curation panel view. Returns every channel (including
   *  admin-hidden) with the admin overlay's positions and a `hidden`
   *  flag per row. Distinct from getChannelsForPersonalisation
   *  because the personalisation panel must NOT see admin-hidden
   *  channels (hard constraint), but the curation panel must in
   *  order to un-hide them. */
  // Admin: override del logo de un canal con una URL externa. Survives
  // M3U refreshes (override row keyed por stream_url). Limpia cualquier
  // archivo subido anteriormente del disco.
  async setChannelLogoURL(channelId: string, logoURL: string): Promise<void> {
    await this.request<{ channel_id: string; logo_url: string }>(
      "PUT",
      `/channels/${encodeURIComponent(channelId)}/logo`,
      { body: { logo_url: logoURL } },
    );
  }

  // Admin: sube un archivo de logo. Multipart con campo `file`. Reusa
  // las mismas validaciones del upload de pósters (MaxUploadBytes 10MB,
  // MIME sniffeado de los bytes, decompression-bomb guard).
  async uploadChannelLogo(channelId: string, file: File): Promise<void> {
    const fd = new FormData();
    fd.append("file", file);
    await this.request<{ channel_id: string; logo_file: string }>(
      "POST",
      `/channels/${encodeURIComponent(channelId)}/logo/upload`,
      { body: fd },
    );
  }

  // Admin: borra el override de logo. Idempotente.
  async clearChannelLogo(channelId: string): Promise<void> {
    await this.request<void>(
      "DELETE",
      `/channels/${encodeURIComponent(channelId)}/logo`,
    );
  }

  async getChannelsForLibraryAdmin(libraryId: string): Promise<Channel[]> {
    return this.request<Channel[]>(
      "GET",
      `/libraries/${libraryId}/channels/admin-view`,
    );
  }

  async replaceLibraryChannelOrder(
    libraryId: string,
    req: ChannelOrderRequest,
  ): Promise<void> {
    await this.request<{ status: "ok" }>(
      "PUT",
      `/libraries/${libraryId}/channels/order`,
      { body: req },
    );
  }

  async resetLibraryChannelOrder(libraryId: string): Promise<void> {
    await this.request<{ status: "ok" }>(
      "DELETE",
      `/libraries/${libraryId}/channels/order`,
    );
  }

  async setLibraryChannelVisibility(
    libraryId: string,
    channelId: string,
    hidden: boolean,
  ): Promise<void> {
    await this.request<{ status: "ok" }>(
      "PUT",
      `/libraries/${libraryId}/channels/${encodeURIComponent(channelId)}/admin-visibility`,
      { body: { hidden } },
    );
  }

  async getChannelsForPersonalisation(libraryId: string): Promise<Channel[]> {
    return this.request<Channel[]>("GET", `/libraries/${libraryId}/channels`, {
      params: { include_hidden: "true" },
    });
  }

  async replaceChannelOrder(req: ChannelOrderRequest): Promise<void> {
    await this.request<{ status: "ok" }>("PUT", "/me/iptv/channels/order", {
      body: req,
    });
  }

  async resetChannelOrder(): Promise<void> {
    await this.request<{ status: "ok" }>("DELETE", "/me/iptv/channels/order");
  }

  async setChannelVisibility(channelId: string, hidden: boolean): Promise<void> {
    await this.request<{ status: "ok" }>(
      "PUT",
      `/me/iptv/channels/${encodeURIComponent(channelId)}/visibility`,
      { body: { hidden } },
    );
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

  async getChannelHealthSummary(
    libraryId: string,
  ): Promise<ChannelHealthSummary> {
    return this.request<ChannelHealthSummary>(
      "GET",
      `/libraries/${libraryId}/channels/health-summary`,
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

  async updateServerIdentity(input: {
    name: string;
    avatar_color: string;
  }): Promise<import("./types").FederationServerInfo> {
    return this.request("PUT", "/admin/peers/identity", { body: input });
  }

  /** Sube una imagen como foto del servidor (visible para peers
   *  cuando hagan probe). Mismo formato que uploadMyAvatar: el
   *  navegador pone el boundary multipart automáticamente cuando
   *  el body es un FormData. */
  async uploadServerAvatar(
    file: File,
  ): Promise<import("./types").FederationServerInfo> {
    const form = new FormData();
    form.append("avatar", file);
    return this.request("POST", "/admin/peers/identity/avatar", { body: form });
  }

  /** Quita la foto del servidor. Idempotente: 204 también si no había. */
  async deleteServerAvatar(): Promise<void> {
    return this.request<void>("DELETE", "/admin/peers/identity/avatar");
  }

  async listPeers(): Promise<import("./types").FederationPeer[]> {
    return this.request("GET", "/admin/peers");
  }

  async revokePeer(id: string): Promise<void> {
    return this.request<void>("DELETE", `/admin/peers/${id}`);
  }

  /** Re-probea el /federation/info del peer y persiste el nombre +
   *  color + URL de la foto actualizados. Idempotente. */
  async refreshPeer(id: string): Promise<import("./types").FederationPeer> {
    return this.request("POST", `/admin/peers/${id}/refresh`);
  }

  // ─── Pairing requests "Steam-style" (migration 048) ──────────────────

  /** Lista todas las pairing requests (incoming + outgoing). */
  async listPairingRequests(): Promise<
    import("./types").FederationPendingRequest[]
  > {
    return this.request("GET", "/admin/peers/pairing-requests");
  }

  /** Envia una pairing request al servidor en `baseURL`. */
  async sendPairingRequest(
    baseURL: string,
  ): Promise<import("./types").FederationPendingRequest> {
    return this.request("POST", "/admin/peers/pairing-requests/send", {
      body: { base_url: baseURL },
    });
  }

  /** Acepta una incoming pending. Devuelve el Peer ya paired. */
  async acceptPairingRequest(
    id: string,
  ): Promise<import("./types").FederationPeer> {
    return this.request("POST", `/admin/peers/pairing-requests/${id}/accept`);
  }

  /** Rechaza una incoming pending. */
  async declinePairingRequest(id: string): Promise<void> {
    return this.request<void>(
      "POST",
      `/admin/peers/pairing-requests/${id}/decline`,
    );
  }

  /** Cancela una outgoing pending. Notifica best-effort al remoto. */
  async cancelPairingRequest(id: string): Promise<void> {
    return this.request<void>("DELETE", `/admin/peers/pairing-requests/${id}`);
  }

  // ─── Notifications inbox (migration 049) ─────────────────────────────

  /** Lista las ultimas N notificaciones del usuario + unread_count. */
  async listMyNotifications(): Promise<
    import("./types").NotificationsResponse
  > {
    return this.request("GET", "/me/notifications");
  }

  /** Marca una notificacion como leida. Idempotente. */
  async markNotificationRead(id: string): Promise<void> {
    return this.request<void>("POST", `/me/notifications/${id}/read`);
  }

  /** Marca todas las del usuario como leidas. Devuelve marked_count. */
  async markAllNotificationsRead(): Promise<{ marked_count: number }> {
    return this.request("POST", "/me/notifications/read-all");
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

  // Device authorization grant — operator (this browser, logged-in)
  // approves a user_code that a separate device (TV, CLI, etc.) is
  // polling for. The device's next /poll receives a JWT pair.
  async approveDeviceCode(userCode: string): Promise<{ approved: boolean }> {
    return this.request("POST", "/auth/device/approve", { body: { user_code: userCode } });
  }

  // Starts a device-pairing flow on behalf of THIS browser. The
  // in-app "Vincular este dispositivo" UI calls this, displays
  // user_code + a QR pointing at verification_uri_complete, and
  // waits on /auth/device/events (SSE) for the operator to approve
  // from another device.
  async startDeviceCode(
    deviceName: string,
  ): Promise<import("./types").DeviceStartResponse> {
    return this.request("POST", "/auth/device/start", {
      body: { device_name: deviceName },
    });
  }

  // Single poll after an SSE "approved" event. On 200 the server
  // sets HTTP-only auth cookies so the caller is logged in for the
  // next /api/v1 request — the JSON tokens in the body are for
  // native clients (TVs, CLI) and can be ignored by the browser.
  async pollDeviceCode(
    deviceCode: string,
  ): Promise<{ access_token: string; refresh_token: string; expires_at: string }> {
    return this.request("POST", "/auth/device/poll", {
      body: { device_code: deviceCode },
    });
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

  // ─── User-facing federation (Phase 4) ──────────────────────────────────

  async listMyPeers(): Promise<import("./types").FederationConnectedPeer[]> {
    return this.request("GET", "/me/peers");
  }

  async listAllPeerLibraries(): Promise<import("./types").FederationUnifiedLibrary[]> {
    return this.request("GET", "/me/peers/libraries");
  }

  async browsePeerLibraries(peerID: string): Promise<import("./types").FederationRemoteLibrary[]> {
    return this.request("GET", `/me/peers/${peerID}/libraries`);
  }

  async browsePeerItems(
    peerID: string,
    libraryID: string,
    opts: { offset?: number; limit?: number } = {},
  ): Promise<import("./types").FederationRemoteItemsResponse> {
    return this.request("GET", `/me/peers/${peerID}/libraries/${libraryID}/items`, {
      params: { offset: opts.offset, limit: opts.limit },
    });
  }

  async refreshPeerLibrary(peerID: string, libraryID: string): Promise<void> {
    return this.request<void>("POST", `/me/peers/${peerID}/libraries/${libraryID}/refresh`);
  }

  // Federated full-text search — fan-out to every paired peer with a
  // ~2s per-peer timeout server-side. A peer that errors / times out
  // is silently skipped: a single misbehaving server cannot blank the
  // user-visible result page.
  async searchPeers(
    q: string,
    perPeerLimit?: number,
  ): Promise<import("./types").FederationSearchResponse> {
    return this.request<import("./types").FederationSearchResponse>(
      "GET",
      "/me/peers/search",
      { params: { q, limit: perPeerLimit } },
    );
  }

  // "Recently added on peers" — server fans out to every paired peer
  // and returns each peer's freshest items merged with origin
  // attribution. Same wire shape as searchPeers (FederationSearchHit)
  // so the home rail can reuse the same renderers.
  async getPeerRecent(
    perPeerLimit?: number,
  ): Promise<import("./types").FederationSearchResponse> {
    return this.request<import("./types").FederationSearchResponse>(
      "GET",
      "/me/peers/recent",
      { params: { limit: perPeerLimit } },
    );
  }

  // startPeerStreamSession asks our origin to broker a stream session
  // on a peer for one of their items. Returns the same-origin master
  // playlist URL the HLS player should load -- never the peer's URL.
  // The user's client capabilities are appended automatically by
  // request() when the X-Hubplay-Client-Capabilities header is set
  // through the same path the local /stream/{id}/info call uses.
  async startPeerStreamSession(
    peerID: string,
    itemID: string,
  ): Promise<import("./types").PeerStreamSessionResponse> {
    return this.request("POST", `/me/peers/${peerID}/stream/${itemID}/session`, {
      body: {},
    });
  }

  // Cross-peer playback state (federation_progress, migration 028).
  // Same role as the local /me/progress/{itemId} pair, but routed to
  // federation_progress instead of user_data because federated items
  // never live in the local items table.
  async getPeerItemProgress(
    peerID: string,
    itemID: string,
  ): Promise<import("./types").PeerItemProgress> {
    return this.request("GET", `/me/peers/${peerID}/items/${itemID}/progress`);
  }
  async updatePeerItemProgress(
    peerID: string,
    itemID: string,
    data: { position_ticks: number; duration_ticks?: number; completed?: boolean },
    options: { keepalive?: boolean } = {},
  ): Promise<void> {
    await this.request("POST", `/me/peers/${peerID}/items/${itemID}/progress`, {
      body: data,
      keepalive: options.keepalive,
    });
  }
  async getPeerContinueWatching(
    limit?: number,
  ): Promise<import("./types").PeerContinueWatchingItem[]> {
    return this.request("GET", "/me/peers/continue-watching", {
      params: { limit },
    });
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

  /** Storage breakdown: disco fisico via statfs (gopsutil) + peso
   *  por biblioteca via SUM(items.size). Cero filesystem I/O para
   *  el peso - el scanner ya captura Size en cada item. */
  async getAdminStorageDisks(): Promise<
    import("./types").AdminStorageDisksResponse
  > {
    return this.request("GET", "/admin/system/storage/disks");
  }

  /** Recientemente añadido del dashboard admin. Mezcla movies +
   *  series rolled-up por actividad (con conteo de nuevos episodios).
   *  Nunca devuelve episodios sueltos. */
  async getAdminRecentlyAdded(
    limit = 12,
  ): Promise<import("./types").AdminRecentlyAddedResponse> {
    return this.request("GET", "/admin/system/recently-added", {
      params: { limit },
    });
  }

  async getAdminStreamActivity(days = 14): Promise<AdminStreamActivityResponse> {
    return this.request<AdminStreamActivityResponse>(
      "GET",
      "/admin/system/stream-activity",
      { params: { days } },
    );
  }

  async getAdminTopItems(days = 7, limit = 5): Promise<AdminTopItemsResponse> {
    return this.request<AdminTopItemsResponse>(
      "GET",
      "/admin/system/top-items",
      { params: { days, limit } },
    );
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

  // Admin "Now Playing" panel — every active stream session the
  // server is currently servicing. Polled every ~5s by the dashboard;
  // the response is sorted server-side by StartedAt descending so the
  // freshest session is first.
  async listAdminStreamSessions(): Promise<AdminStreamSession[]> {
    return this.request<AdminStreamSession[]>("GET", "/admin/system/sessions");
  }

  // Idempotent: killing a session that has already ended (idle reaper,
  // user-driven teardown, ffmpeg crash) returns 204 same as a real
  // kill, so the admin button never surfaces a misleading error.
  async killAdminStreamSession(sessionID: string): Promise<void> {
    return this.request<void>("DELETE", `/admin/system/sessions/${encodeURIComponent(sessionID)}`);
  }

  async resetSystemSetting(key: string): Promise<SystemSettingsResponse> {
    return this.request<SystemSettingsResponse>(
      "DELETE",
      `/admin/system/settings/${encodeURIComponent(key)}`,
    );
  }

  // ─── Admin: database driver + DSN management ──────────────────────────

  async getAdminDatabase(): Promise<AdminDatabaseStatus> {
    return this.request<AdminDatabaseStatus>("GET", "/admin/system/db");
  }

  async getAdminDatabaseProfiles(): Promise<AdminDatabaseProfiles> {
    return this.request<AdminDatabaseProfiles>("GET", "/admin/system/db/profiles");
  }

  async testAdminDatabase(req: AdminDatabaseTestRequest): Promise<AdminDatabaseTestResponse> {
    return this.request<AdminDatabaseTestResponse>("POST", "/admin/system/db/test", { body: req });
  }

  async saveAdminDatabase(req: AdminDatabaseSaveRequest): Promise<AdminDatabaseSaveResponse> {
    return this.request<AdminDatabaseSaveResponse>("PUT", "/admin/system/db", { body: req });
  }

  async restartServer(): Promise<{ restart_scheduled: boolean }> {
    return this.request<{ restart_scheduled: boolean }>("POST", "/admin/system/restart");
  }

  // migrateDatabase streams NDJSON events through the response body.
  // Returns the raw Response so the caller can use the ReadableStream
  // reader to render live progress in the panel.
  async migrateDatabase(
    req: AdminDatabaseMigrateRequest,
  ): Promise<Response> {
    // Use the underlying fetch so we get the raw stream; the
    // request<T> helper auto-parses JSON which would buffer the
    // entire response.
    const res = await fetch("/api/v1/admin/system/db/migrate", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(req),
    });
    if (!res.ok) {
      const text = await res.text();
      throw new Error(text || `HTTP ${res.status}`);
    }
    return res;
  }

  // ─── Setup wizard: database step ──────────────────────────────────────

  async getSetupDatabaseProfiles(): Promise<AdminDatabaseProfiles> {
    return this.request<AdminDatabaseProfiles>("GET", "/setup/db/profiles");
  }

  async testSetupDatabase(req: AdminDatabaseTestRequest): Promise<AdminDatabaseTestResponse> {
    return this.request<AdminDatabaseTestResponse>("POST", "/setup/db/test", { body: req });
  }

  async saveSetupDatabase(req: AdminDatabaseSaveRequest): Promise<AdminDatabaseSaveResponse> {
    return this.request<AdminDatabaseSaveResponse>("POST", "/setup/db", { body: req });
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
