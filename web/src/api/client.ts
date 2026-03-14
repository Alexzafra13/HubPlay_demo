import type {
  AuthResponse,
  BrowseResponse,
  Channel,
  CreateLibraryRequest,
  EPGProgram,
  HealthResponse,
  ItemDetail,
  Library,
  MediaItem,
  PaginatedResponse,
  SetupStatus,
  StreamSession,
  SystemCapabilities,
  UpdateLibraryRequest,
  User,
  UserData,
  ApiErrorBody,
} from "./types";
import { ApiError } from "./types";

const TOKEN_KEY = "hubplay_access_token";
const REFRESH_KEY = "hubplay_refresh_token";

interface RequestOptions {
  params?: Record<string, string | number | boolean | undefined>;
  body?: unknown;
  headers?: Record<string, string>;
}

export class ApiClient {
  private baseUrl: string;

  constructor(baseUrl = "") {
    this.baseUrl = baseUrl;
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

    const token = localStorage.getItem(TOKEN_KEY);
    if (token) {
      headers["Authorization"] = `Bearer ${token}`;
    }

    if (body !== undefined && (method === "POST" || method === "PUT" || method === "PATCH")) {
      headers["Content-Type"] = "application/json";
    }

    const response = await fetch(url, {
      method,
      headers,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });

    if (!response.ok) {
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

    return (await response.json()) as T;
  }

  // ─── Auth ─────────────────────────────────────────────────────────────

  async login(username: string, password: string): Promise<AuthResponse> {
    const data = await this.request<AuthResponse>("POST", "/api/auth/login", {
      body: { username, password },
    });
    localStorage.setItem(TOKEN_KEY, data.access_token);
    localStorage.setItem(REFRESH_KEY, data.refresh_token);
    return data;
  }

  async refresh(refreshToken: string): Promise<AuthResponse> {
    const data = await this.request<AuthResponse>("POST", "/api/auth/refresh", {
      body: { refresh_token: refreshToken },
    });
    localStorage.setItem(TOKEN_KEY, data.access_token);
    localStorage.setItem(REFRESH_KEY, data.refresh_token);
    return data;
  }

  async logout(): Promise<void> {
    const refreshToken = localStorage.getItem(REFRESH_KEY);
    try {
      await this.request<void>("POST", "/api/auth/logout", {
        body: { refresh_token: refreshToken },
      });
    } finally {
      localStorage.removeItem(TOKEN_KEY);
      localStorage.removeItem(REFRESH_KEY);
    }
  }

  // ─── Setup ────────────────────────────────────────────────────────────

  async getSetupStatus(): Promise<SetupStatus> {
    return this.request<SetupStatus>("GET", "/api/setup/status");
  }

  async setupCreateAdmin(
    username: string,
    password: string,
    displayName?: string,
  ): Promise<AuthResponse> {
    const data = await this.request<AuthResponse>("POST", "/api/setup/admin", {
      body: { username, password, display_name: displayName },
    });
    localStorage.setItem(TOKEN_KEY, data.access_token);
    localStorage.setItem(REFRESH_KEY, data.refresh_token);
    return data;
  }

  async browseDirectories(path?: string): Promise<BrowseResponse> {
    return this.request<BrowseResponse>("GET", "/api/setup/browse", {
      params: path ? { path } : undefined,
    });
  }

  async setupCreateLibraries(
    libraries: Array<{ name: string; content_type: string; paths: string[] }>,
  ): Promise<Library[]> {
    return this.request<Library[]>("POST", "/api/setup/libraries", {
      body: { libraries },
    });
  }

  async setupSettings(settings: Record<string, unknown>): Promise<void> {
    return this.request<void>("POST", "/api/setup/settings", {
      body: settings,
    });
  }

  async setupComplete(startScan = true): Promise<void> {
    return this.request<void>("POST", "/api/setup/complete", {
      body: { start_scan: startScan },
    });
  }

  async getSystemCapabilities(): Promise<SystemCapabilities> {
    return this.request<SystemCapabilities>("GET", "/api/setup/capabilities");
  }

  // ─── Users ────────────────────────────────────────────────────────────

  async getMe(): Promise<User> {
    return this.request<User>("GET", "/api/users/me");
  }

  async getUsers(): Promise<User[]> {
    return this.request<User[]>("GET", "/api/users");
  }

  async createUser(data: {
    username: string;
    password: string;
    display_name?: string;
    role?: string;
  }): Promise<User> {
    return this.request<User>("POST", "/api/users", { body: data });
  }

  async deleteUser(id: string): Promise<void> {
    return this.request<void>("DELETE", `/api/users/${id}`);
  }

  // ─── Libraries ────────────────────────────────────────────────────────

  async getLibraries(): Promise<Library[]> {
    return this.request<Library[]>("GET", "/api/libraries");
  }

  async getLibrary(id: string): Promise<Library> {
    return this.request<Library>("GET", `/api/libraries/${id}`);
  }

  async createLibrary(data: CreateLibraryRequest): Promise<Library> {
    return this.request<Library>("POST", "/api/libraries", { body: data });
  }

  async updateLibrary(id: string, data: UpdateLibraryRequest): Promise<Library> {
    return this.request<Library>("PUT", `/api/libraries/${id}`, { body: data });
  }

  async deleteLibrary(id: string): Promise<void> {
    return this.request<void>("DELETE", `/api/libraries/${id}`);
  }

  async scanLibrary(id: string): Promise<void> {
    return this.request<void>("POST", `/api/libraries/${id}/scan`);
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
    return this.request<PaginatedResponse<MediaItem>>("GET", "/api/items", {
      params: params as Record<string, string | number | boolean | undefined>,
    });
  }

  async getItem(id: string): Promise<ItemDetail> {
    return this.request<ItemDetail>("GET", `/api/items/${id}`);
  }

  async getItemChildren(id: string): Promise<MediaItem[]> {
    return this.request<MediaItem[]>("GET", `/api/items/${id}/children`);
  }

  async searchItems(
    q: string,
    type?: string,
    limit?: number,
  ): Promise<PaginatedResponse<MediaItem>> {
    return this.request<PaginatedResponse<MediaItem>>("GET", "/api/items/search", {
      params: { q, type, limit },
    });
  }

  async getLatestItems(libraryId?: string, limit?: number): Promise<MediaItem[]> {
    return this.request<MediaItem[]>("GET", "/api/items/latest", {
      params: { library_id: libraryId, limit },
    });
  }

  // ─── Progress / User Data ─────────────────────────────────────────────

  async getProgress(itemId: string): Promise<UserData> {
    return this.request<UserData>("GET", `/api/items/${itemId}/progress`);
  }

  async updateProgress(
    itemId: string,
    data: {
      position_ticks?: number;
      audio_stream_index?: number;
      subtitle_stream_index?: number;
    },
  ): Promise<UserData> {
    return this.request<UserData>("PUT", `/api/items/${itemId}/progress`, {
      body: data,
    });
  }

  async markPlayed(itemId: string): Promise<UserData> {
    return this.request<UserData>("POST", `/api/items/${itemId}/played`);
  }

  async markUnplayed(itemId: string): Promise<UserData> {
    return this.request<UserData>("DELETE", `/api/items/${itemId}/played`);
  }

  async toggleFavorite(itemId: string): Promise<UserData> {
    return this.request<UserData>("POST", `/api/items/${itemId}/favorite`);
  }

  async getContinueWatching(): Promise<MediaItem[]> {
    return this.request<MediaItem[]>("GET", "/api/items/continue-watching");
  }

  async getNextUp(): Promise<MediaItem[]> {
    return this.request<MediaItem[]>("GET", "/api/items/next-up");
  }

  async getFavorites(): Promise<MediaItem[]> {
    return this.request<MediaItem[]>("GET", "/api/items/favorites");
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
    return this.request<StreamSession>("POST", `/api/stream/${itemId}`, {
      body: data ?? {},
    });
  }

  async getStreamInfo(itemId: string): Promise<{
    media_streams: import("./types").MediaStream[];
    playback_methods: string[];
  }> {
    return this.request("GET", `/api/stream/${itemId}/info`);
  }

  // ─── Channels / Live TV ───────────────────────────────────────────────

  async getChannels(libraryId?: string): Promise<Channel[]> {
    return this.request<Channel[]>("GET", "/api/channels", {
      params: { library_id: libraryId },
    });
  }

  async getChannel(id: string): Promise<Channel> {
    return this.request<Channel>("GET", `/api/channels/${id}`);
  }

  async getChannelSchedule(
    id: string,
    from?: string,
    to?: string,
  ): Promise<EPGProgram[]> {
    return this.request<EPGProgram[]>("GET", `/api/channels/${id}/schedule`, {
      params: { from, to },
    });
  }

  async getChannelGroups(libraryId?: string): Promise<string[]> {
    return this.request<string[]>("GET", "/api/channels/groups", {
      params: { library_id: libraryId },
    });
  }

  // ─── System ───────────────────────────────────────────────────────────

  async getHealth(): Promise<HealthResponse> {
    return this.request<HealthResponse>("GET", "/api/health");
  }
}

export const api = new ApiClient();
