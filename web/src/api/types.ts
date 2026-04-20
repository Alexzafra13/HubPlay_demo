// ─── User & Auth ────────────────────────────────────────────────────────────

export interface User {
  id: string;
  username: string;
  display_name: string;
  role: string;
  created_at: string;
}

export interface AuthResponse {
  access_token: string;
  refresh_token: string;
  expires_in: number;
  user: User;
}

export interface LoginRequest {
  username: string;
  password: string;
}

export interface RegisterRequest {
  username: string;
  password: string;
  display_name?: string;
}

// ─── Libraries ──────────────────────────────────────────────────────────────

export type ContentType = "movies" | "tvshows" | "livetv";

export type ScanStatus = "idle" | "scanning" | "error";

export interface PathStatus {
  path: string;
  accessible: boolean;
}

export interface Library {
  id: string;
  name: string;
  content_type: ContentType;
  paths: string[];
  path_status?: PathStatus[];
  item_count: number;
  scan_mode: string;
  scan_status: ScanStatus;
  created_at: string;
  /** IPTV-only: M3U playlist URL (empty for non-livetv libraries). */
  m3u_url?: string;
  /** IPTV-only: XMLTV EPG URL (empty when not configured). */
  epg_url?: string;
}

export interface CreateLibraryRequest {
  name: string;
  content_type: ContentType;
  /** Filesystem paths — required for movies/shows/music, empty for livetv. */
  paths: string[];
  /** IPTV-only (content_type === "livetv"). Required when livetv is selected. */
  m3u_url?: string;
  /** IPTV-only. Optional: RefreshM3U auto-discovers XMLTV from the playlist header. */
  epg_url?: string;
}

export interface UpdateLibraryRequest {
  name?: string;
  paths?: string[];
  /** IPTV-only. Pass null/undefined to leave unchanged; empty string clears. */
  m3u_url?: string;
  /** IPTV-only. Same null-vs-empty semantics as m3u_url. */
  epg_url?: string;
}

// ─── Media Items ────────────────────────────────────────────────────────────

export type MediaType = "movie" | "series" | "season" | "episode";

export interface MediaItem {
  id: string;
  type: MediaType;
  title: string;
  original_title: string | null;
  year: number | null;
  sort_title: string;
  overview: string | null;
  tagline: string | null;
  genres: string[];
  community_rating: number | null;
  content_rating: string | null;
  runtime_ticks: number | null;
  premiere_date: string | null;
  poster_url: string | null;
  backdrop_url: string | null;
  logo_url: string | null;
  parent_id: string | null;
  series_id: string | null;
  season_number: number | null;
  episode_number: number | null;
  path: string | null;
}

export type StreamType = "video" | "audio" | "subtitle";

export interface MediaStream {
  index: number;
  type: StreamType;
  codec: string;
  language: string | null;
  title: string | null;
  channels: number | null;
  width: number | null;
  height: number | null;
  bitrate: number | null;
  is_default: boolean;
  is_forced: boolean;
  hdr_type: string | null;
}

export interface Person {
  name: string;
  role: string;
  type: string;
  image_url: string | null;
}

export interface UserData {
  progress: {
    position_ticks: number;
    percentage: number;
    audio_stream_index: number | null;
    subtitle_stream_index: number | null;
  };
  is_favorite: boolean;
  played: boolean;
  play_count: number;
  last_played_at: string | null;
}

export interface ItemDetail extends MediaItem {
  duration_ticks: number | null;
  media_streams: MediaStream[];
  people: Person[];
  user_data: UserData | null;
}

// ─── Live TV ────────────────────────────────────────────────────────────────

/**
 * Canonical channel category. Derived server-side from the raw M3U
 * `group-title` via `iptv.Canonical` and kept in sync by both the
 * Go constant set and this union. When adding a category here, add
 * it to `internal/iptv/categories.go` too.
 */
export type ChannelCategory =
  | "general"
  | "news"
  | "sports"
  | "movies"
  | "music"
  | "entertainment"
  | "kids"
  | "culture"
  | "documentaries"
  | "international"
  | "travel"
  | "religion"
  | "adult";

export interface Channel {
  id: string;
  name: string;
  number: number;
  /** Upstream logo URL from the M3U. May be missing, broken, or slow. */
  logo_url: string | null;
  /** Raw M3U `group-title` — kept for operators and legacy clients. */
  group: string | null;
  /** Same as `group` — preferred name going forward. */
  group_name: string | null;
  /** Canonical, UI-stable category the frontend keys off for chips and i18n. */
  category: ChannelCategory;
  /** 1–3 uppercase chars derived from the channel name — always populated. */
  logo_initials: string;
  /** `#RRGGBB` background of the fallback logo — deterministic per channel. */
  logo_bg: string;
  /** `#RRGGBB` foreground of the fallback logo (picked for contrast). */
  logo_fg: string;
  stream_url: string;
  library_id: string;
  /** BCP-47 or raw M3U `tvg-language`. May be empty. */
  language: string;
  /** ISO-like country code from M3U (e.g. "ES"). May be empty. */
  country: string;
  is_active?: boolean;
}

export interface EPGProgram {
  id: string;
  channel_id: string;
  title: string;
  description: string | null;
  start_time: string;
  end_time: string;
  category: string | null;
  icon_url: string | null;
}

export interface PublicCountry {
  code: string;
  name: string;
  flag: string;
}

export interface ImportPublicIPTVResponse {
  library_id: string;
  name: string;
  country: string;
  m3u_url: string;
}

// ─── Streaming ──────────────────────────────────────────────────────────────

export type PlaybackMethod = "direct_play" | "direct_stream" | "transcode";

export interface StreamSession {
  session_id: string;
  session_token: string;
  playback_method: PlaybackMethod;
  master_playlist: string | null;
  direct_url: string | null;
}

// ─── Generic Responses ──────────────────────────────────────────────────────

export interface PaginatedResponse<T> {
  items: T[];
  total: number;
  offset: number;
  limit: number;
  next_cursor?: string;
}

export interface HealthResponse {
  status: string;
  version: string;
  uptime: number;
  database: string;
  ffmpeg: string;
  active_streams: number;
  active_transcodes: number;
}

export interface SetupStatus {
  needs_setup: boolean;
  current_step: "account" | "libraries" | "settings" | "complete" | "";
}

export interface BrowseDirectory {
  name: string;
  path: string;
}

export interface BrowseResponse {
  current: string;
  parent: string;
  directories: BrowseDirectory[];
}

export interface SystemCapabilities {
  ffmpeg_path: string;
  ffmpeg_found: boolean;
  hw_accels: string[];
}

// ─── Images ────────────────────────────────────────────────────────────────

export interface ImageInfo {
  id: string;
  type: string;
  path: string;
  width?: number;
  height?: number;
  blurhash?: string;
  is_primary: boolean;
}

export interface AvailableImage {
  url: string;
  type: string;
  language: string;
  width: number;
  height: number;
  score: number;
}

// ─── Errors ─────────────────────────────────────────────────────────────────

export interface ApiErrorBody {
  error: {
    code: string;
    message: string;
    details?: Record<string, unknown>;
  };
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly details?: Record<string, unknown>;

  constructor(status: number, body: ApiErrorBody) {
    super(body.error.message);
    this.name = "ApiError";
    this.status = status;
    this.code = body.error.code;
    this.details = body.error.details;
  }
}
