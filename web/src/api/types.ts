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

/**
 * Library content type. Backend canonicalises any `tvshows` alias
 * the older clients might send to `shows` at the API boundary, but
 * everywhere downstream — DB rows, scanner, sort helpers, the
 * admin filters by content_type — uses the canonical name. Keep
 * this type aligned with what the server actually stores so any
 * `lib.content_type === "shows"` comparison works without a
 * widening cast.
 */
export type ContentType = "movies" | "shows" | "livetv";

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
  // Per-user state. Only present when the listing endpoint sees an
  // authenticated request — anonymous responses omit the key entirely
  // so it stays `undefined` rather than `null`. Used to render the
  // "watched" check and the in-progress bar on poster/episode cards.
  user_data?: UserData;
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

export interface Chapter {
  start_ticks: number;
  end_ticks: number;
  title: string;
  // Future: BIF-style chapter thumbnails. Backend emits the URL when
  // present, omits the key otherwise — `undefined` means "no
  // pre-rendered preview", not an error.
  image_path?: string;
}

// One candidate from an external subtitle provider (OpenSubtitles
// today). The `file_id` is opaque — pass it back to the download
// endpoint along with `source` to fetch the actual VTT.
export interface ExternalSubtitleResult {
  source: string;     // "opensubtitles", ...
  file_id: string;    // opaque handle for the download endpoint
  language: string;   // ISO 639-1 (en, es, fr, ...)
  format: string;     // "srt" | "ass" — the source format before conversion
  score: number;      // provider-specific quality / popularity score
}

export interface ItemDetail extends MediaItem {
  duration_ticks: number | null;
  media_streams: MediaStream[];
  people: Person[];
  // Optional: backend omits `chapters` entirely when the file has no
  // markers (most non-Blu-ray rips). Empty and absent are equivalent
  // for clients.
  chapters?: Chapter[];
  // Inherited from MediaItem (optional). Re-listed in this interface
  // for documentation only — the backend omits the key entirely when
  // the request is unauthenticated or no row exists, so it parses as
  // `undefined`, never `null`. Keeping the same shape on both
  // interfaces lets `ItemDetail extends MediaItem` stay covariant.
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
  /** When the channel first landed in the library. RFC3339 UTC. May
   * be absent on older DTOs; the hero "newest" mode just sorts absent
   * values to the end. */
  added_at?: string;
  /**
   * Health bucket derived from `consecutive_failures` against the
   * server's UnhealthyThreshold (3).
   * - "ok"        → 0 consecutive failures
   * - "degraded"  → 1–2 (failing but still listed for the user)
   * - "dead"      → ≥3 (server hides these from the main list, but
   *                    the playback-failure beacon may surface them
   *                    transiently in cached client state)
   * Absent on legacy DTOs — treat as "ok".
   */
  health_status?: "ok" | "degraded" | "dead";
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

/**
 * A curated, well-known XMLTV feed the backend ships with. Mirrors
 * `internal/iptv/PublicEPGSource` — the admin UI renders this list in the
 * "Añadir fuente EPG" dropdown so operators don't have to memorise URLs.
 */
export interface PublicEPGSource {
  id: string;
  name: string;
  description: string;
  language: string;
  countries: string[];
  url: string;
}

/**
 * One EPG provider attached to a livetv library. Priority is "lower first";
 * the refresher processes sources in ascending priority order and a channel
 * covered by priority 0 is not overwritten by priority 1.
 *
 * `catalog_id` is empty for custom URLs the admin pasted directly.
 * `last_*` fields are populated by the refresher after each run — the UI
 * uses them to flag broken sources with a status badge.
 */
export interface LibraryEPGSource {
  id: string;
  library_id: string;
  catalog_id: string;
  url: string;
  priority: number;
  last_refreshed_at: string | null;
  last_status: "" | "ok" | "error";
  last_error: string;
  last_program_count: number;
  last_channel_count: number;
  created_at: string;
}

export interface AddEPGSourceRequest {
  catalog_id?: string;
  url?: string;
}

/**
 * A channel the EPG refresher did not match against any source.
 * Returned by `GET /libraries/:id/channels/without-epg`. The admin
 * UI uses this to let operators fix `tvg_id` by hand.
 */
export interface ChannelWithoutEPG {
  id: string;
  library_id: string;
  name: string;
  number: number;
  group_name: string;
  logo_url: string;
  tvg_id: string;
  is_active: boolean;
}

/** PATCH /channels/:id accepts partial edits. tvg_id is pointer-like
 * in JSON: omit the field to keep existing, pass empty string to
 * clear both the column AND the persistent override. */
export interface PatchChannelRequest {
  tvg_id?: string;
}

/**
 * Channel augmented with the timestamp of the caller's most recent
 * playback. Returned by `/me/channels/continue-watching`. Extends
 * Channel so the existing ChannelCard can render it without a
 * parallel component path.
 */
export type ContinueWatchingChannel = Channel & {
  last_watched_at: string;
};

/**
 * IPTV scheduled job — one automated action per library.
 *
 * Mirrors the backend `iptv_scheduled_jobs` row. The list endpoint
 * synthesises a placeholder with `enabled: false` for missing kinds so
 * the UI always renders both rows; persisted rows come back with the
 * real timestamps.
 */
export type IPTVScheduledJobKind = "m3u_refresh" | "epg_refresh";

export interface IPTVScheduledJob {
  library_id: string;
  kind: IPTVScheduledJobKind;
  interval_hours: number;
  enabled: boolean;
  last_run_at?: string;
  last_status: "" | "ok" | "error";
  last_error?: string;
  last_duration_ms: number;
}

export interface UpsertScheduledJobRequest {
  interval_hours: number;
  enabled?: boolean;
}

/**
 * A channel with its opportunistic-probe health fields attached.
 *
 * Mirrors the backend `channelHealthDTO`: it's a regular Channel plus
 * the four probe columns. Serving the same shape means the viewer's
 * "Apagados" rail can feed these rows into the normal ChannelCard
 * without a parallel component path.
 */
export type UnhealthyChannel = Channel & {
  last_probe_at: string | null;
  last_probe_status: "" | "ok" | "error";
  last_probe_error: string;
  consecutive_failures: number;
};

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
