import type {
  AuthResponse,
  AvailableImage,
  BrowseResponse,
  Channel,
  CreateLibraryRequest,
  EPGProgram,
  HealthResponse,
  ImageInfo,
  ImportPublicIPTVResponse,
  ItemDetail,
  Library,
  MediaItem,
  PaginatedResponse,
  PublicCountry,
  SetupStatus,
  StreamSession,
  SystemCapabilities,
  UpdateLibraryRequest,
  User,
  UserData,
  ApiErrorBody,
} from "./types";
import { ApiError } from "./types";

const USER_KEY = "hubplay_user";

interface RequestOptions {
  params?: Record<string, string | number | boolean | undefined>;
  body?: unknown;
  headers?: Record<string, string>;
}

type AuthEventListener = {
  onTokenRefresh?: (accessToken: string, refreshToken: string) => void;
  onAuthFailure?: () => void;
};

export class ApiClient {
  private baseUrl: string;
  private authListener: AuthEventListener = {};

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
    const { params, body, headers: extraHeaders } = options;

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

    const response = await fetch(url, {
      method,
      headers,
      credentials: "include",
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });

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
  ): Promise<UserData> {
    return this.request<UserData>("PUT", `/me/progress/${itemId}`, {
      body: data,
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

  // ─── Channels / Live TV ───────────────────────────────────────────────

  async getChannels(libraryId?: string): Promise<Channel[]> {
    if (!libraryId) return [];
    return this.request<Channel[]>("GET", `/libraries/${libraryId}/channels`);
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
    return this.request<Record<string, EPGProgram[]>>("GET", "/channels/schedule", {
      params: { channels: channelIds.join(","), from, to },
    });
  }

  async getChannelGroups(libraryId?: string): Promise<string[]> {
    if (!libraryId) return [];
    return this.request<string[]>("GET", `/libraries/${libraryId}/channels/groups`);
  }

  async getPublicCountries(): Promise<PublicCountry[]> {
    return this.request<PublicCountry[]>("GET", "/iptv/public/countries");
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

    const response = await fetch(`${this.baseUrl}/items/${itemId}/images/${type}/upload`, {
      method: "POST",
      credentials: "include",
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
}

export const api = new ApiClient("/api/v1");
