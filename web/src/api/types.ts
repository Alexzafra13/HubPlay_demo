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
  /**
   * IPTV-only: ISO 639-1 lowercase codes the M3U import keeps (e.g.
   * ["es", "en"]). Empty array means "no filter, import every
   * channel" — matches the historical default. The backend always
   * returns an array (never null) so consumers can dispatch on
   * `length === 0`.
   */
  language_filter: string[];
  /**
   * IPTV-only: when true, the M3U / EPG fetcher skips TLS
   * certificate verification for THIS library only. Used for
   * providers shipping expired Let's Encrypt or self-signed certs.
   * Off by default. Has no effect on the stream proxy (which keeps
   * strict verification regardless).
   */
  tls_insecure: boolean;
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
  /**
   * IPTV-only. Optional: ISO 639-1 codes to keep at import time. Empty
   * / undefined means no filter.
   */
  language_filter?: string[];
  /**
   * IPTV-only. Skip TLS verification when fetching M3U / EPG.
   * Defaults to false on the server when omitted.
   */
  tls_insecure?: boolean;
}

/**
 * Verdict returned by the M3U preflight probe. Stable string union
 * so the UI can dispatch icons / colours / messages on the value.
 *
 *   ok          — playlist looks valid, safe to save
 *   slow        — TCP connected but no response in 12 s; likely
 *                 still works (provider generating big list), save
 *                 anyway and wait
 *   empty       — 200 OK but body is empty (account has no channels)
 *   html        — got HTML error page instead of M3U (account
 *                 suspended, IP blocked, captive portal)
 *   auth        — 401 / 403 (credentials rejected)
 *   not_found   — 404 (URL wrong)
 *   tls         — certificate / handshake error (suggest TLS toggle)
 *   dns         — host not resolvable (URL typo or ISP block)
 *   connect     — TCP refused (server down)
 *   invalid_url — URL parse error / wrong scheme
 *   unknown     — catch-all so the UI never gets a missing field
 */
export type PreflightStatus =
  | "ok"
  | "slow"
  | "empty"
  | "html"
  | "auth"
  | "not_found"
  | "tls"
  | "dns"
  | "connect"
  | "invalid_url"
  | "unknown";

export interface PreflightResult {
  status: PreflightStatus;
  http_status?: number;
  /** Bytes the upstream advertised in Content-Length, when present. */
  content_length?: number;
  /** First non-blank line of the body, truncated. Useful to debug
   *  HTML error pages without round-tripping the whole response. */
  body_hint?: string;
  elapsed_ms: number;
  /** Spanish operator-facing message, ready to render verbatim. */
  message: string;
}

export interface UpdateLibraryRequest {
  name?: string;
  paths?: string[];
  /** IPTV-only. Pass null/undefined to leave unchanged; empty string clears. */
  m3u_url?: string;
  /** IPTV-only. Same null-vs-empty semantics as m3u_url. */
  epg_url?: string;
  /**
   * IPTV-only. undefined = leave unchanged. Empty array = clear the
   * filter (no language filtering at import). Non-empty = replace.
   */
  language_filter?: string[];
  /** IPTV-only. undefined = leave unchanged. */
  tls_insecure?: boolean;
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
  duration_ticks: number | null;
  premiere_date: string | null;
  poster_url: string | null;
  backdrop_url: string | null;
  logo_url: string | null;
  // Cheap loading-placeholder data for the primary poster, computed
  // server-side at image-ingest time. PosterCard paints `poster_color`
  // as the card background while the real <img> decodes so cards never
  // pop from grey to image. `poster_blurhash` is the canonical
  // BlurHash string for future client-side decoding; the web client
  // only consumes the colour today, but the field is on the wire so
  // native clients can use it without another round-trip.
  poster_color?: string;
  poster_color_muted?: string;
  poster_blurhash?: string;
  // Studio / network that produced the show (e.g. "HBO", "Disney+").
  // Backend pulls it from the metadata table when available; absent
  // for items without a TMDb match. Rendered next to the meta badges
  // on the series hero so the page reads more like Plex / Jellyfin.
  studio?: string;
  // Series-only: aggregate of how many episodes the authenticated user
  // has watched out of the total under this show. Computed server-side
  // in the GetItem handler and only present for authenticated calls
  // against a series — absent on movies / seasons / episodes and on
  // anonymous responses. The hero renders "Has visto X de Y" when set.
  episode_progress?: { total: number; watched: number };
  // External provider IDs keyed by provider name (imdb, tmdb, tvdb,
  // wikidata, ...). Surfaced from the item's external_ids table; the
  // detail page uses them to render "Open in IMDb" / "Open in TMDb"
  // entries in the kebab menu when present. Absent when no external
  // ids are stored — matches the current scanner output for items
  // that didn't get a TMDb match.
  external_ids?: Record<string, string>;
  parent_id: string | null;
  series_id: string | null;
  // Episode-only enrichment: when this item is an episode, the
  // backend folds in the parent series' breadcrumb + visual fallbacks
  // so the detail page can render a polished hero without the client
  // having to climb episode → season → series itself. Absent on
  // non-episodes and on episodes whose hierarchy lookup failed.
  series_title?: string;
  series_poster_url?: string;
  series_backdrop_url?: string;
  series_logo_url?: string;
  // Pre-computed dominant + dark-muted colours of the primary backdrop
  // (or poster, when no backdrop exists), formatted as CSS rgb()
  // strings. The SeriesHero gradient consumes these on first paint —
  // when absent (older rows scanned before extraction shipped), the
  // hook falls back to runtime `node-vibrant` extraction.
  backdrop_colors?: { vibrant?: string; muted?: string };
  // Best-matched trailer for the SeriesHero pre-play overlay. Picked
  // at scan time (TMDb /videos), embedded muted with autoplay so the
  // hero plays a Netflix-style preview a couple of seconds after the
  // user lands on the page. `key` is platform-specific; `site` selects
  // the embed URL ("YouTube" or "Vimeo"). Absent when no trailer was
  // available — in that case the hero just shows the static backdrop.
  trailer?: { key: string; site: string };
  season_number: number | null;
  episode_number: number | null;
  // Season-only: number of direct episode children. The Children
  // endpoint folds it into the season summary so the SeasonGrid card
  // can render "9 episodios" without an extra round-trip per season.
  episode_count?: number;
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

// Server-side response shape for GET /people/{id}. Distinct from
// `Person` (which is the per-item credit row in an item's cast strip)
// because the person page bundles the filmography + a richer image
// surface that the cast strip doesn't need.
export interface FilmographyEntry {
  item_id: string;
  type: "movie" | "series";
  title: string;
  year?: number;
  role: string;
  character?: string;
  sort_order: number;
}

export interface PersonDetail {
  id: string;
  name: string;
  type: string;
  // Built server-side as `/api/v1/people/{id}/thumb` only when a thumb
  // file actually exists on disk. Absent on people scanned without a
  // TMDb match or where the photo download failed.
  image_url?: string;
  filmography: FilmographyEntry[];
}

export interface Person {
  // Stable id — uuid. Used by /people/{id}/thumb (handled
  // server-side) and the upcoming person detail page route.
  id: string;
  name: string;
  role: string;
  // Character name for actors ("Wanda Maximoff"). Empty for crew
  // (director, writer) where the role IS the character.
  character?: string;
  // Profile photo URL pointing at /api/v1/people/{id}/thumb when the
  // scanner persisted a photo. Absent when the provider didn't ship
  // one or download failed — UI falls back to an initial-letter chip.
  image_url?: string;
  sort_order: number;
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
  media_streams: MediaStream[];
  // Cast / crew. Server omits the key entirely when no rows are
  // stored, so the field is optional on the wire side too. Detail
  // page guards on `?.length > 0` already; absent === empty list.
  people?: Person[];
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

// ─── Admin: System stats ───────────────────────────────────────────────────
//
// Wire shape of GET /admin/system/stats. Separate from HealthResponse —
// /health is the public liveness probe, this is the admin panel data and
// is allowed to evolve. Keep these interfaces aligned with internal/api/
// handlers/system.go.

export interface SystemStats {
  server: SystemServerStats;
  database: SystemDatabaseStats;
  ffmpeg: SystemFFmpegStats;
  runtime: SystemRuntimeStats;
  streaming: SystemStreamingStats;
  storage: SystemStorageStats;
  libraries: SystemLibraryStats;
}

export interface SystemServerStats {
  version: string;
  go_version: string;
  started_at: string;
  uptime_seconds: number;
  bind_address: string;
  base_url: string;
  /** ISO timestamp of the server's clock at the moment of the snapshot. */
  server_time: string;
  /** IANA timezone name the server's runtime uses. */
  timezone: string;
}

export interface SystemDatabaseStats {
  ok: boolean;
  error?: string;
  path?: string;
  size_bytes: number;
}

export interface SystemFFmpegStats {
  found: boolean;
  path: string;
  /**
   * Reflects the operator's `hardware_acceleration.enabled` config.
   * When false, no accelerator detection has been run; the panel shows
   * an actionable hint pointing at the right config key instead of a
   * confusing "no accelerators detected" badge.
   */
  hw_accel_enabled: boolean;
  hw_accels_available: string[];
  hw_accel_selected: string;
  hw_accel_encoder: string;
}

export interface SystemLibraryStats {
  total: number;
  items_total: number;
  by_type: SystemLibraryTypeStats[];
}

export interface SystemLibraryTypeStats {
  /** "movies" | "shows" | "livetv" — same vocabulary as Library.content_type. */
  content_type: string;
  /** Number of libraries of this content type. */
  count: number;
  /** Total items across libraries of this content type. */
  items: number;
}

export interface SystemRuntimeStats {
  goroutines: number;
  memory_alloc_mb: number;
  memory_sys_mb: number;
  gc_pause_ms: number;
  num_gc: number;
  cpu_count: number;
  os: string;
  arch: string;
}

export interface SystemStreamingStats {
  transcode_sessions_active: number;
  transcode_sessions_max: number;
}

export interface SystemStorageStats {
  image_dir_path?: string;
  image_dir_bytes: number;
  transcode_cache_path?: string;
  transcode_cache_bytes: number;
}

// ─── Admin: signing keys ───────────────────────────────────────────────────

export interface AuthKey {
  id: string;
  created_at: string;
  retired_at?: string;
  is_primary: boolean;
}

export interface RotateAuthKeyResponse {
  id: string;
  created_at: string;
  overlap_seconds: number;
}

// SystemSetting describes one row in the runtime settings allowlist
// (server.base_url, hardware_acceleration.enabled, …). Backend layers
// the YAML default under any DB override; the UI shows `effective`,
// flags `override` so the operator knows whether the value came from
// app_settings, and surfaces `restart_needed` for keys (HWAccel) that
// require a container restart to take effect.
export interface SystemSetting {
  key: string;
  default: string;
  effective: string;
  override: boolean;
  restart_needed: boolean;
  hint: string;
  allowed_values?: string[];
}

export interface SystemSettingsResponse {
  settings: SystemSetting[];
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
