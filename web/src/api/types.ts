// ─── Federation ─────────────────────────────────────────────────────────────

// ServerInfo describes a HubPlay server's public identity. Returned both
// by GET /federation/info (the local server's own info) and by the admin
// probe endpoint when fetching a remote's identity. The pubkey + fingerprint
// are non-secret by design — the corresponding Ed25519 private key never
// leaves the server. Admins compare fingerprints out-of-band before pairing.
export interface FederationServerInfo {
  server_uuid: string;
  name: string;
  version: string;
  public_key: string;            // base64
  pubkey_fingerprint: string;    // "a8f3:k2m9:x4p1:c7e2"
  pubkey_words: string[];        // 6 short words for voice confirmation
  supported_scopes: string[];
  advertised_url: string;
  admin_contact?: string;
  // avatar_color: hex tipo "#1d4ed8" elegido por el admin como
  // fallback del avatar del servidor cuando no hay foto subida.
  // Vacío = el peer cae a su paleta determinista a partir del nombre.
  avatar_color?: string;
  // avatar_image_url: URL absoluta servida por el propio servidor
  // (incluye scheme/host). Vacío significa "sin foto subida".
  avatar_image_url?: string;
}

export type FederationPeerStatus = "pending" | "paired" | "revoked";

export interface FederationPeer {
  id: string;
  server_uuid: string;
  name: string;
  base_url: string;
  status: FederationPeerStatus;
  fingerprint: string;
  public_key: string;
  created_at: string;
  paired_at?: string;
  last_seen_at?: string;
  last_seen_status_code?: number;
  revoked_at?: string;
  // Branding del peer remoto, capturado en el handshake y refrescable
  // con POST /admin/peers/{id}/refresh. Vacío = PeersTable cae a la
  // paleta determinista derivada del server_uuid + iniciales.
  avatar_color?: string;
  avatar_image_url?: string;
}

export interface FederationInvite {
  id: string;
  code: string;
  expires_at: string;
}

// FederationPendingRequest — una entrada del inbox de pairing
// requests (flow Steam-style, sin codigo). incoming = alguien
// nos quiere emparejar; outgoing = nosotros le enviamos peticion
// a alguien y esperamos respuesta.
export type FederationPendingRequestDirection = "incoming" | "outgoing";
export type FederationPendingRequestStatus =
  | "pending"
  | "accepted"
  | "declined"
  | "cancelled"
  | "expired";

export interface FederationPendingRequest {
  id: string;
  direction: FederationPendingRequestDirection;
  peer_server_uuid: string;
  peer_name: string;
  peer_base_url: string;
  peer_public_key: string; // base64
  peer_avatar_color?: string;
  peer_avatar_image_url?: string;
  fingerprint: string;
  fingerprint_words: string[];
  created_at: string;
  expires_at: string;
  status: FederationPendingRequestStatus;
  responded_at?: string;
}

// FederationLibraryShare — per-library opt-in. The presence of a row
// for (peer_id, library_id) means the peer can see that library; the
// boolean scopes refine what they can do (browse/play/download/livetv).
// Default scopes when a share is first created: browse + play; download
// and livetv are off by default because they consume our resources
// (disk + upstream bandwidth) and need explicit admin opt-in.
export interface FederationLibraryShare {
  id: string;
  peer_id: string;
  library_id: string;
  can_browse: boolean;
  can_play: boolean;
  can_download: boolean;
  can_livetv: boolean;
  created_at: string;
}

// User-facing peer summary — what /api/v1/me/peers returns. Slim
// shape: no audit details, no public_key bytes (the user has no use
// for them; admins do). Only paired peers appear here.
export interface FederationConnectedPeer {
  id: string;
  server_uuid: string;
  name: string;
  base_url: string;
  status: "paired";
  fingerprint: string;
}

// Library exposed by a peer to us. The `scopes` reflect what the
// peer's admin has GRANTED us — useful so the UI can show / hide
// affordances (e.g. "Download" button only when can_download).
export interface FederationRemoteLibrary {
  id: string;
  name: string;
  content_type: string;
  scopes: {
    can_browse: boolean;
    can_play: boolean;
    can_download: boolean;
    can_livetv: boolean;
  };
}

// One item in a peer's library catalog.
//
// `poster_url` is synthesized server-side as a same-origin path that
// resolves through our local proxy: the user's browser only ever
// fetches images from this server, never from the peer directly.
// Absent when the peer didn't have a primary image for the item;
// the card then falls back to the dominant-colour placeholder.
export interface FederationRemoteItem {
  id: string;
  type: string;
  title: string;
  year?: number;
  overview?: string;
  poster_url?: string;
  // Pre-extracted dominant swatches from the peer's primary image,
  // same wire shape the local Item carries. PeerItemDetail consumes
  // these so the page-wide aurora paints on first render without
  // running node-vibrant in the browser. Absent for older peers that
  // pre-date the federation-side color plumbing OR for items whose
  // primary image hasn't been extracted yet — frontend falls back to
  // runtime extraction in either case.
  backdrop_colors?: { vibrant?: string; muted?: string };
}

// Paginated response for items + cache freshness flag. The UI shows
// a "cached / offline" badge when from_cache is true AND the peer is
// known to be offline (or browsing took the stale-fallback path).
export interface FederationRemoteItemsResponse {
  items: FederationRemoteItem[];
  total: number;
  from_cache: boolean;
}

// PeerStreamSessionResponse — what /me/peers/{peerID}/stream/{itemId}/session
// returns once the API client unwraps the `{"data": ...}` envelope.
// `master_playlist_url` is same-origin (proxied through our server
// with our peer JWT); the user's player only ever asks our origin.
// `strategy` mirrors the peer's stream.Decide() outcome and drives
// the local UI's "remuxing", "transcoding to 1080p", etc. labels
// just as the local /stream/{id}/info response does.
export interface PeerStreamSessionResponse {
  strategy: "direct_play" | "direct_stream" | "transcode";
  master_playlist_url: string;
  peer_session_id: string;
}

// One row of the federated-search response. Wire shape emitted by
// GET /api/v1/me/peers/search?q=... after the consumer-side fan-out
// across every paired peer. The poster_url is already same-origin
// (proxied through us — see FederationRemoteItem) and library_id is
// what the UI needs to route a click into
// /peers/{peer_id}/libraries/{library_id}/items/{id}, the same detail
// route the per-peer browse path uses.
export interface FederationSearchHit {
  peer_id: string;
  peer_name: string;
  library_id: string;
  id: string;
  type: string;
  title: string;
  year?: number;
  overview?: string;
  poster_url?: string;
  backdrop_colors?: { vibrant?: string; muted?: string };
}

export interface FederationSearchResponse {
  hits: FederationSearchHit[];
}

// Cross-peer playback state (federation_progress, migration 028).
// Same shape as UserData.progress for federated items; the player
// uses position_ticks for resume and the Continue Watching rail
// renders percentage from (position / duration).
export interface PeerItemProgress {
  item_id: string;
  peer_id: string;
  position_ticks: number;
  duration_ticks: number;
  completed: boolean;
  last_played_at?: string;
}

// One row of the cross-peer Continue Watching rail. Mirrors the
// local rail's wire shape closely enough that the LandscapeCard
// component can render either with a small adapter -- the extra
// peer_id / peer_name fields enable the badge + click routing into
// the federated detail page.
export interface PeerContinueWatchingItem {
  id: string;
  peer_id: string;
  peer_name: string;
  library_id: string;
  type: string;
  title: string;
  year?: number;
  overview?: string;
  poster_url?: string;
  position_ticks: number;
  duration_ticks: number;
  percentage: number;
  last_played_at: string;
}

// Unified row: one library × one peer, flattened across all paired
// peers in our network. Used by the "/peers" landing page so the UI
// renders a single grid instead of nested peer→library navigation.
export interface FederationUnifiedLibrary {
  peer_id: string;
  peer_name: string;
  peer_fingerprint: string;
  library_id: string;
  library_name: string;
  content_type: string;
  can_play: boolean;
  can_download: boolean;
  can_livetv: boolean;
}

// ─── User & Auth ────────────────────────────────────────────────────────────

export interface User {
  id: string;
  username: string;
  display_name: string;
  role: string;
  created_at: string;
  // Profile-tree fields surfaced by /me, /users, and the upcoming
  // /auth/profiles list. All four are optional on the wire because
  // legacy responses (older deploys, federated peers) may not carry
  // them yet.
  parent_user_id?: string;
  password_change_required?: boolean;
  has_pin?: boolean;
  max_content_rating?: string;
  is_active?: boolean;
  last_login_at?: string | null;
  // True for the oldest admin row. Used by the admin Users table to
  // grey out destructive actions (delete / reset password / role
  // change) on the bootstrap account so a sibling admin can't
  // accidentally lock the deploy owner out.
  is_primary?: boolean;
  // ISO timestamp of the temporary-access deadline. Null / undefined
  // = permanent access. Lazy enforcement on the server: Login +
  // middleware reject after this stamp, no background job needed.
  access_expires_at?: string | null;
  // Per-user avatar override (hex string `#RRGGBB`). Empty / absent
  // = use the deterministic FNV → palette fallback in
  // `avatarColorForUser`.
  avatar_color?: string;
  // URL pública del avatar subido por el usuario, ya con cache-buster
  // embebido (`?v=<filename>`). Cuando es null/undefined el frontend
  // cae a iniciales sobre color como antes.
  avatar_image_url?: string | null;

  // Admin permission flags (migración 055). Opcionales en el wire
  // para que responses pre-055 no rompan el parser. is_owner es
  // inmutable de por vida (el que instala la app) — la UI marca al
  // owner con badge y deshabilita TODAS sus celdas en la matriz.
  // Los demás flags son editables vía PUT /users/{id}/permissions
  // si el requester tiene can_manage_admins (y can_manage_admins
  // mismo, sólo si el requester es el owner).
  is_owner?: boolean;
  can_manage_admins?: boolean;
  can_manage_users?: boolean;
  can_manage_libraries?: boolean;
  can_manage_iptv?: boolean;
  can_edit_metadata?: boolean;
  can_change_artwork?: boolean;
  can_view_audit?: boolean;
  can_upload?: boolean;

  // Cuota de upload (migración 053). Sólo /me las devuelve; el
  // listado /users también las incluiría si fuera necesario, pero
  // el panel admin de uploads aún no está montado en el frontend.
  // bytes — números enteros, JS los maneja hasta 2^53 sin pérdida
  // de precisión, suficiente para librerías de cientos de TiB.
  upload_quota_bytes?: number;
  upload_used_bytes?: number;
}

export interface CreateUserResponse {
  id: string;
  username: string;
  display_name: string;
  role: string;
  password_change_required: boolean;
  // Returned exactly once when the admin creates a user without
  // typing a password — the server generated a readable temporary
  // password and the admin must hand it to the user. Absent when
  // the admin specified their own password.
  generated_password?: string;
}

export interface ResetPasswordResponse {
  user_id: string;
  generated_password: string;
}

/**
 * Admin-only payload of GET /users/{id}/library-access. Profile ids
 * server-side resolve to their parent (grants always target the
 * top-level account — ADR-014), so the response surfaces both the
 * requested user_id AND the owner_id whose grants actually apply,
 * plus a flag the UI uses to render a read-only "inherited" view
 * with a "edit on the parent account" hint.
 */
export interface UserLibraryAccess {
  user_id: string;
  owner_id: string;
  library_ids: string[];
  is_inherited: boolean;
}

/**
 * Flags granulares del admin (migración 055). is_owner es inmutable
 * de por vida (no hay endpoint para cambiarlo, ver
 * docs/architecture/admin-permissions.md cuando aterrice). Los
 * demás flags son editables vía PUT /users/{id}/permissions con las
 * reglas:
 *   - el owner es inmutable: pasarlo como target devuelve 403.
 *   - sólo el owner puede otorgar can_manage_admins a otros admins.
 *   - el target debe ser admin (role=admin); usuarios normales se
 *     rechazan con TARGET_NOT_ADMIN.
 */
export interface UserPermissions {
  id: string;
  is_owner: boolean;
  can_manage_admins: boolean;
  can_manage_users: boolean;
  can_manage_libraries: boolean;
  can_manage_iptv: boolean;
  can_edit_metadata: boolean;
  can_change_artwork: boolean;
  can_view_audit: boolean;
  can_upload: boolean;
}

/**
 * Una fila del audit log de uploads (tabla upload_audit, migración
 * 054). Se inserta UNA SOLA VEZ por upload, en su estado final.
 * Las fases intermedias (validating, probing, moving, indexing)
 * fluyen por SSE en /uploads/events, no quedan en la DB.
 *
 * outcome semantics:
 *   accepted  todas las fases pasaron, fichero en su librería
 *   rejected  fallo de validación binaria (extensión, magic bytes,
 *             ffprobe, cuota o librería no elegible)
 *   aborted   el cliente canceló o se desconectó sin terminar
 *   error     fallo no atribuible al cliente (disco, move, panic)
 */
export interface UploadAuditEntry {
  id: string;
  library_id: string;
  original_name: string;
  final_path: string;
  bytes: number;
  mime_detected: string;
  outcome: "accepted" | "rejected" | "aborted" | "error";
  error_message: string;
  started_at: string;
  finished_at: string;
  duration_ms: number;
}

/**
 * Las fases que el backend publica por SSE en /uploads/events tras
 * cerrar el último PATCH. Coinciden con upload.Service.Finish.
 * El cliente las consume para mostrar progreso post-bytes —
 * "100% subido" no significa "listo", el pipeline aún tiene
 * trabajo después (ffprobe, move atómico).
 */
export type UploadPhase = "validating" | "probing" | "moving" | "indexing";

/**
 * Una entrada de la lista de orígenes CORS dinámicos (PR4 feature).
 * Los `statics` del YAML no llevan metadata — son strings; los
 * `dynamics` añadidos via panel sí.
 */
export interface CorsOriginEntry {
  origin: string;
  created_by: string;
  created_at: string;
  note: string;
}

export interface CorsOriginsListResponse {
  statics: string[];
  dynamics: CorsOriginEntry[];
}

/**
 * Una fila del audit log unificado (PR5). El payload es JSON
 * schemaless en el backend; en el cliente lo dejamos como string
 * y cada renderer decide cómo formatearlo (algunos lo pintan tal
 * cual, otros parsean campos conocidos como changes o reason).
 */
export interface AuditLogEntry {
  id: string;
  actor_user_id: string;
  event_type: string;
  target_type: string;
  target_id: string;
  payload: string;
  ip_address: string;
  user_agent: string;
  created_at: string;
}

export interface AuditLogQueryResponse {
  rows: AuditLogEntry[];
  total: number;
  limit: number;
  offset: number;
}

/**
 * One row of the user-facing "Tus dispositivos" panel — every active
 * auth session (refresh token alive in DB) for the calling user.
 * Distinct from AdminStreamSession (the admin's "Now Playing" surface
 * over the streaming manager): these are LOGINS, not playbacks. The
 * `current` flag marks whichever row matches the caller's refresh
 * cookie so the UI can warn before the operator revokes themselves.
 */
export interface MySession {
  id: string;
  device_name: string;
  device_id: string;
  ip_address: string;
  created_at: string;
  last_active_at: string;
  expires_at: string;
  current: boolean;
  /**
   * How this session was minted. "device_link" rows came from the
   * RFC 8628 pairing flow (QR / user_code) so the UI can group them
   * separately from regular username/password logins. Derived
   * server-side from the device_id prefix the device-code service
   * stamps; callers must NOT compute it on the wire row.
   */
  auth_method: "device_link" | "password";
}

/**
 * Response shape of POST /auth/device/start. The display form of
 * user_code is `ABCD-EFGH`; verification_uri_complete already carries
 * it as a query parameter so a QR rendered against the URL lands the
 * operator on /link with the input pre-filled.
 */
export interface DeviceStartResponse {
  device_code: string;
  user_code: string;
  verification_url: string;
  verification_uri: string;
  verification_uri_complete: string;
  expires_in: number;
  interval: number;
}

/**
 * One entry in the "Who's watching?" picker. Slim wire payload —
 * just identity, the avatar attribution that the deterministic
 * colour helper consumes, and the PIN flag so the picker can render
 * a lock icon. Returned by /auth/login (alongside the token) and by
 * GET /me/profiles when the frontend lands via cookie refresh.
 */
export interface ProfileSummary {
  id: string;
  username: string;
  display_name: string;
  role: string;
  is_active: boolean;
  parent_user_id?: string;
  has_pin: boolean;
  max_content_rating?: string;
  avatar_color?: string;
}

export interface AuthResponse {
  access_token: string;
  refresh_token: string;
  expires_in: number;
  user: User;
  /** Profile tree under the current account. Returned by /auth/login
   *  and /auth/switch-profile so the frontend can decide whether to
   *  drop into the "Who's watching?" picker without an extra fetch.
   *  Absent on solo deploys (server omits the field). */
  profiles?: ProfileSummary[];
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
  // The article-stripped sort key the backend computes for SQL
  // ORDER BY ("the matrix" → "matrix"). Most endpoints ship it, but
  // a few list shapes (federation peer items, older lean responses)
  // omit it — keep this optional so callers handle absence rather
  // than trusting a non-null type that the wire doesn't honour.
  sort_title?: string;
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
  // 16:9 "miniatura" — the marketing still providers (TMDb / Fanart)
  // ship next to the cartel. The Continue Watching rail prefers it
  // over backdrop_url for movies so the landscape cards stay
  // rectangular like the per-episode screencaps next to them. Null
  // when the provider never shipped one for this title.
  thumb_url?: string | null;
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
  // Optional brand-mark image URL for the studio (TMDb's logo_path
  // resolved server-side to an absolute image URL). Present when the
  // production company has a logo on TMDb (Lucasfilm, HBO, Disney+,
  // …) and absent otherwise — frontend renders the image when set
  // and falls back to the `studio` text otherwise so older studios
  // and indie productions still get attribution.
  studio_logo_url?: string;
  // URL-safe slug for the click-through to /studios/{slug}. Backend
  // computes it from `studio` via the same recipe stored in the
  // `studios` table, so the link is valid for any studio that
  // produced an item in the catalogue. Absent when the item has no
  // studio attribution at all.
  studio_slug?: string;
  // Movie-saga link (Jellyfin-style "Movie Collection") — only the
  // {id, name} pair so the detail page can render "Part of: X" with
  // a click-through to /collections/{id}. Absent on TV items and on
  // movies without a TMDb collection match.
  collection?: CollectionRef;
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
  // Season-level artwork the Home hero uses for episode slides: the
  // poster the user sees when entering the season page on the left,
  // and the season's own backdrop (or, if absent, the series'
  // backdrop via the `series_backdrop_url` fallback). Surfaced by
  // `/me/continue-watching` for episode rows; absent on movies and
  // on episodes whose season has no scanned artwork.
  season_poster_url?: string;
  season_backdrop_url?: string;
  // Episode-activity hint emitted by /items/latest when the caller
  // scopes to a shows library with `type=series`. The number is the
  // count of episodes added under this series in the trailing 14-day
  // window; the home rail renders a "+N nuevos episodios" corner
  // badge on the PosterCard when present and > 0. Absent for non-
  // series rows and on the standard /items/latest call without the
  // type filter.
  new_episodes_count?: number;
  latest_activity_at?: string;
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
  // `/api/v1/images/file/{id}` — present only when the item has a
  // primary poster on disk. Absent for items scanned without a TMDb
  // match or where the poster download failed.
  poster_url?: string;
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

// Server-side response for GET /studios (browse) and
// GET /studios/{slug} (detail). The detail wire reuses the same
// {id, type, title, year, poster_url} shape the recommendations
// rail and filmography use, so the Tile component can render any
// of them with no special-casing.
export interface StudioListEntry {
  id: string;
  name: string;
  slug: string;
  logo_url?: string;
  item_count: number;
}

export interface StudioItem {
  id: string;
  type: "movie" | "series";
  title: string;
  year?: number;
  poster_url?: string;
}

export interface StudioDetail {
  id: string;
  name: string;
  slug: string;
  logo_url?: string;
  tmdb_id?: number;
  items: StudioItem[];
}

// Movie collections (Jellyfin-style sagas — X-Men, MCU, Toy Story).
// The detail endpoint reuses StudioItem's grid shape for `items` so
// the same Tile component can render either surface.
export interface CollectionListEntry {
  id: string;
  name: string;
  poster_url?: string;
  backdrop_url?: string;
  item_count: number;
}

export interface CollectionDetail {
  id: string;
  tmdb_id: number;
  name: string;
  overview?: string;
  poster_url?: string;
  backdrop_url?: string;
  items: StudioItem[];
}

// Slim {id, name} pair surfaced on a movie's detail wire so the
// frontend can render "Part of: X" with a click-through to
// /collections/{id}. Absent on movies without a TMDb collection match.
export interface CollectionRef {
  id: string;
  name: string;
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

// Skip-intro / skip-credits / skip-recap segment.
//
// Emitted by the backend's segment detector at the (item, kind)
// level — the server already collapsed multiple sources to the
// highest-confidence row, so the client can iterate without
// further resolution. `start_seconds` / `end_seconds` are floats
// because that's what `<video>.currentTime` speaks; the underlying
// DB stores 10M-tick integers but the API rounds at the boundary.
//
// `confidence` is 0..1. Anything ≥ 0.7 is treated as "show the skip
// button automatically"; lower values exist only because future
// fingerprint-derived rows may be uncertain — chapter-derived rows
// always sit at 0.95.
export type EpisodeSegmentKind = "intro" | "outro" | "recap";

export interface EpisodeSegment {
  kind: EpisodeSegmentKind;
  source: "chapter" | "fingerprint" | "manual";
  start_seconds: number;
  end_seconds: number;
  confidence: number;
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

// One "more like this" entry surfaced under the cast strip on the
// detail page. Server resolves the poster URL against TMDb's image
// CDN and tags `in_library` + `local_id` when the user already has
// the title stored — clicking such an entry opens the local detail
// page; clicking an external one opens TMDb in a new tab.
export interface Recommendation {
  tmdb_id: string;
  title: string;
  year: number;
  overview?: string;
  poster_url?: string;
  rating?: number;
  in_library: boolean;
  /** Set when in_library is true; deep-link target for /movies/{id}
   *  or /series/{id} depending on what the user actually has. */
  local_id?: string;
}

// IdentifyCandidate — un match TMDb propuesto para el flujo admin de
// "Identify". Distinto de Recommendation porque (a) no se cruza con la
// biblioteca local — siempre apunta al provider externo — y (b) lleva
// el external_id directo + nombre del provider, ya que el frontend lo
// vuelve a enviar al POST /identify para confirmar la elección.
export interface IdentifyCandidate {
  /** ID externo del provider (TMDb id hoy). */
  external_id: string;
  /** Nombre del provider; hoy siempre "tmdb". */
  provider: string;
  title: string;
  /** 0 cuando el provider no devuelve fecha (estrenos lejanos, contenido
   *  borrado). El diálogo lo oculta cuando vale 0. */
  year: number;
  overview?: string;
  /** Absoluta. Vacía cuando el candidato no tiene póster en el provider;
   *  el diálogo cae a un placeholder en ese caso. */
  poster_url?: string;
  /** Ranking de relevancia normalizado 0-1. Sólo informativo: la lista
   *  ya viene ordenada por el backend. */
  score: number;
}

export interface ItemDetail extends MediaItem {
  media_streams: MediaStream[];
  /** Admin-only: cuando true, el scanner se salta este item en
   *  enrichMetadata / RefreshMetadata. Sólo se incluye cuando el
   *  caller es admin y el identifier está cableado en el backend. */
  metadata_locked?: boolean;
  // Cast / crew. Server omits the key entirely when no rows are
  // stored, so the field is optional on the wire side too. Detail
  // page guards on `?.length > 0` already; absent === empty list.
  people?: Person[];
  // Optional: backend omits `chapters` entirely when the file has no
  // markers (most non-Blu-ray rips). Empty and absent are equivalent
  // for clients.
  chapters?: Chapter[];
  // Optional: skip-intro / skip-credits / recap markers. Backend
  // emits at most one row per kind (highest-confidence source wins
  // server-side), so the player can iterate without further
  // resolution. Absent === no segments detected.
  segments?: EpisodeSegment[];
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
  /** Nombre saneado para mostrar (sin `[geo-blocked]`, `[VIP]`, `(HD)`,
   *  `|ES|`, símbolos decorativos). Backend deriva esto en cada
   *  serialización; el nombre crudo del M3U queda en `raw_name`. */
  name: string;
  /** Nombre EXACTO del M3U cuando difiere del saneado. Útil como
   *  tooltip o para tests. Omitido cuando el M3U ya venía limpio. */
  raw_name?: string;
  /** Etiqueta de resolución detectada en el nombre crudo: "UHD"
   *  (4K/2160p), "FHD" (1080p), "HD" (720p), "SD" (480p) o undefined
   *  cuando no hay marca. El frontend la pinta como badge sutil. */
  quality?: "UHD" | "FHD" | "HD" | "SD";
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
  /** Populated only when the caller passed `?include_hidden=true`
   *  (personalisation panel). True when the user has hidden this
   *  channel from their own view. Absent / false everywhere else. */
  hidden?: boolean;
  /** Populated only when the caller passed `?include_hidden=true`.
   *  The user's override position when they have one — distinct from
   *  `number` (the admin default) so the panel can render both. */
  user_position?: number;
}

export interface ChannelOrderRequest {
  /** Full reordered list of channel IDs. The position each ID gets
   *  is its index + 1. Omitting a channel removes its override row
   *  → it falls back to the admin's default position. */
  ordered_channel_ids: string[];
  /** Set of channel IDs the user wants hidden. Can overlap with
   *  `ordered_channel_ids` (a channel that's reordered AND hidden)
   *  or be exclusive (a channel that's only hidden — keeps its
   *  admin position when un-hidden). */
  hidden_channel_ids: string[];
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
  host: SystemHostStats;
  database: SystemDatabaseStats;
  ffmpeg: SystemFFmpegStats;
  runtime: SystemRuntimeStats;
  streaming: SystemStreamingStats;
  storage: SystemStorageStats;
  libraries: SystemLibraryStats;
}

// Host-level introspection sampled by internal/sysmetrics. Distinct
// from SystemRuntimeStats (which is Go-process-specific: heap MB,
// goroutines) — answers "is my SERVER hot?" rather than "is my hubplay
// process hot?". Empty strings / zero values are valid: they mean the
// probe couldn't fingerprint that field on this host (e.g. no
// nvidia-smi for GPU; gopsutil failure for CPU model). The panel
// renders dashes for empty rows rather than hiding them.
export interface SystemHostStats {
  /** Human-readable CPU model (e.g. "AMD Ryzen 5 5600 6-Core Processor"). */
  cpu_model: string;
  /** Physical cores. Hyper-threaded CPUs have half the logical count. */
  cpu_cores_physical: number;
  /** Logical threads — matches runtime.NumCPU() on Linux. */
  cpu_cores_logical: number;
  /** Host-wide CPU utilisation 0-100. 0 on the very first sample. */
  cpu_percent: number;
  ram_total_bytes: number;
  /** "Used" defined as Total - Available, matching `free -h`'s used column. */
  ram_used_bytes: number;
  /** NVIDIA GPU model when nvidia-smi succeeded at boot. Empty otherwise. */
  gpu_model: string;
  gpu_memory_total_bytes: number;
  gpu_driver_version: string;
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

// ─── Database driver management (admin + setup wizard) ────────────────

export type DatabaseDriver = "sqlite" | "postgres";

// AdminDatabaseStatus is what GET /admin/system/db returns. Renders
// the "Database" card in the System panel: live driver, redacted
// DSN / path, and pool stats so the operator can spot when the
// pool is exhausted at a glance.
export interface AdminDatabaseStatus {
  driver: DatabaseDriver;
  path?: string;
  dsn_redacted?: string;
  pool: {
    max_open: number;
    open: number;
    in_use: number;
    idle: number;
    wait_count: number;
    wait_duration_ms: number;
  };
}

// AdminDatabaseTestRequest carries a candidate driver + DSN/path
// the panel wants to validate before persisting. Path is sqlite-only;
// dsn is postgres-only — the server ignores the irrelevant one.
export interface AdminDatabaseTestRequest {
  driver: DatabaseDriver;
  path?: string;
  dsn?: string;
  // When true with driver=postgres, the server swaps in the
  // docker-compose-bundled DSN. The UI never sees / sends the
  // password — the panel just renders a toggle.
  use_bundled?: boolean;
}

// Returned by GET /admin/system/db/profiles and /setup/db/profiles.
// Drives whether the UI offers the one-click "Switch to PostgreSQL"
// toggle (bundled docker-compose) or falls back to the custom DSN
// form.
export interface AdminDatabaseProfiles {
  bundled_postgres: boolean;
  bundled_label?: string;
}

export interface AdminDatabaseTestResponse {
  ok: boolean;
  driver_detected?: string;
  server_version?: string;
  duration_ms: number;
  error?: string;
}

export interface AdminDatabaseSaveRequest extends AdminDatabaseTestRequest {
  // When true the server triggers a graceful self-restart after
  // persisting the new YAML so the next boot uses the new driver.
  restart?: boolean;
}

export interface AdminDatabaseMigrateRequest {
  target_dsn?: string;
  use_bundled?: boolean;
  restart?: boolean;
}

export interface AdminDatabaseSaveResponse {
  status: "saved";
  restart_scheduled: boolean;
}

// AdminDatabaseMigrateEvent is one record from the NDJSON stream
// the migration endpoint emits. The panel parses each line as JSON
// and routes by `event`.
export type AdminDatabaseMigrateEvent =
  | { event: "start"; source: string; target: string }
  | { event: "progress"; table: string; copied: number; total: number; phase: string }
  | { event: "config_saved" }
  | { event: "restart_scheduled" }
  | { event: "warning"; message: string }
  | { event: "error"; message: string }
  | { event: "done"; tables_copied: number; rows_copied: number; duration_ms: number };

// AdminStreamSession is one row of the admin "Now Playing" table.
// Mirrors the wire shape of GET /admin/system/sessions; the username
// and item title are best-effort enrichments from the server (empty
// strings if the underlying user/item has been deleted but the
// manager still tracks the session — at which point the kill button
// is the only remaining way to clean up).
export interface AdminStreamSession {
  session_id: string;
  user_id: string;
  username?: string;
  item_id: string;
  item_title?: string;
  item_type?: string;
  profile?: string; // empty for non-transcode sessions
  method: "DirectPlay" | "DirectStream" | "Transcode";
  started_at: string; // RFC3339 UTC
  last_accessed: string; // RFC3339 UTC
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

// ─── Channel health summary (admin Bibliotecas panel) ─────────────────────

/**
 * Lightweight projection the LivetvAdminPanel reads on first paint.
 * Replaces the previous pattern of fetching the full unhealthy +
 * without-epg lists just to call `.length` on them — those responses
 * can be hundreds of KB on real catalogues, and the panel mounts
 * once per livetv library on the Bibliotecas page so the parallel
 * waterfall noticeably froze the browser. The full lists now load
 * lazily, only when their tab is active.
 */
export interface ChannelHealthSummary {
  total_channels: number;
  unhealthy_count: number;
  without_epg_count: number;
}

// ─── Home (configurable home page) ─────────────────────────────────────────

/**
 * One section/rail in the user's home layout. The same shape is used
 * over the wire and in storage. `library_id` is set only when the
 * section type is `latest_in_library`. `library_name` is server-resolved
 * (read-only) for rail titles — the backend ignores anything the client
 * sends in this field on PUT.
 */
export interface HomeSection {
  id: string;
  type:
    | "continue_watching"
    | "next_up"
    | "trending"
    | "live_now"
    | "latest_in_library";
  library_id?: string;
  library_name?: string;
  visible: boolean;
}

export interface HomeLayout {
  version: number;
  sections: HomeSection[];
}

/**
 * One entry in the "trending this week" rail. Smaller than MediaItem —
 * carries only what's needed to render a poster card. The frontend
 * renders these via the same PosterCard / LandscapeCard family by
 * mapping the fields onto a MediaItem-shaped subset.
 */
export interface HomeTrendingItem {
  id: string;
  type: string;
  title: string;
  library_id: string;
  play_count: number;
  year?: number;
  community_rating?: number;
  poster_url?: string;
  poster_blurhash?: string;
  poster_color?: string;
  poster_color_muted?: string;
  backdrop_url?: string;
  logo_url?: string;
  overview?: string;
  genres?: string[];
}

/**
 * One entry in the "Recomendado para ti" tier of the home hero. Same
 * basic media-card shape as a TrendingItem but enriched with
 * `recommended_because.genres` so the hero slide can render an
 * honest "Porque te gusta {{genre}}" subtitle. Only movies and
 * series — episodes are filtered server-side.
 */
export interface HomeRecommendedItem {
  id: string;
  type: "movie" | "series";
  title: string;
  library_id: string;
  year?: number;
  community_rating?: number;
  poster_url?: string;
  poster_blurhash?: string;
  poster_color?: string;
  poster_color_muted?: string;
  backdrop_url?: string;
  logo_url?: string;
  overview?: string;
  genres?: string[];
  recommended_because: {
    genres: string[];
  };
}

/**
 * Payload of /me/home/because-you-watched. The seed is the recently-
 * completed item that lit the rail; the items are recommendations
 * sharing genres with the seed. `seed` is null when the caller has
 * no completed watches yet (cold-start) — the rail hides itself in
 * that case.
 */
export interface HomeBecauseSeed {
  id: string;
  type: "movie" | "series";
  title: string;
  library_id: string;
  year?: number;
  poster_url?: string;
  poster_blurhash?: string;
  poster_color?: string;
}

export interface HomeBecauseResponse {
  seed: HomeBecauseSeed | null;
  items: HomeRecommendedItem[];
}

/**
 * One bucket in the admin Resumen's watch-activity sparkline. The
 * series is always contiguous (the backend zero-pads days that had
 * no plays) so the frontend can pass `buckets.map(b => b.watch_minutes)`
 * straight into `<Sparkline />` without reshaping.
 */
export interface AdminStreamActivityBucket {
  date: string; // YYYY-MM-DD UTC
  watch_minutes: number;
  session_count: number;
}

export interface AdminStreamActivityResponse {
  days: number;
  buckets: AdminStreamActivityBucket[];
}

/**
 * One row of the admin "most-watched" leaderboard. Slim payload —
 * episodes get rolled up to their parent series so the list stays
 * meaningful without per-episode noise.
 */
export interface AdminTopItem {
  id: string;
  type: "movie" | "series";
  title: string;
  play_count: number;
}

export interface AdminTopItemsResponse {
  days: number;
  items: AdminTopItem[];
}

/**
 * One channel in the "live now" rail. Always carries the channel
 * id/name/library; the EPG fields are populated only when the channel
 * has a program currently airing (the rail still shows the channel
 * with a "no programme info" hint when EPG is missing).
 */
export interface HomeLiveNowChannel {
  channel_id: string;
  channel_name: string;
  channel_logo?: string;
  // Deterministic placeholder avatar — same recipe as the LiveTV
  // browser's Channel.logo_initials/bg/fg, so a card on the home rail
  // and on /live-tv share the same letters and colour for the same
  // channel. Always populated by the server, even when channel_logo
  // is set, so the <ChannelLogo> onError fallback never has to guess.
  logo_initials: string;
  logo_bg: string;
  logo_fg: string;
  library_id: string;
  library_name: string;
  program_title?: string;
  program_start?: string;
  program_end?: string;
  program_icon?: string;
}

export interface ApiErrorBody {
  error: {
    code: string;
    message: string;
    details?: Record<string, unknown>;
  };
}

// ─── Storage (admin disk usage + per-library size) ─────────────────────────

// AdminLibraryDisk — una biblioteca dentro de un mount fisico, con
// su peso (sum de items.size) y file_count.
export interface AdminLibraryDisk {
  id: string;
  name: string;
  content_type: string;
  path: string;
  size_bytes: number;
  file_count: number;
}

// AdminDisk — un mount-point fisico con sus stats agregados +
// las bibliotecas que viven en el.
export interface AdminDisk {
  mount: string;
  filesystem?: string;
  total_bytes: number;
  used_bytes: number;
  free_bytes: number;
  used_percent: number;
  libraries: AdminLibraryDisk[];
}

export interface AdminStorageDisksResponse {
  disks: AdminDisk[];
}

// AdminRecentlyAddedItem — entrada del strip "Recientemente añadido"
// del dashboard. Es un MediaItem (con poster_url, backdrop_url,
// overview, etc.) MÁS dos campos opcionales:
//
//   - latest_activity_at: timestamp efectivo de "actividad" (added_at
//     para movies; max(added_at) de descendants para series).
//   - new_episodes_count: solo para series con actividad en los
//     últimos 14 días. La UI muestra "+N nuevos" en el card si > 0.
//
// El backend agrupa episodes a su serie padre - este endpoint nunca
// devuelve type=episode, solo movie + series.
export interface AdminRecentlyAddedItem extends MediaItem {
  latest_activity_at?: string;
  new_episodes_count?: number;
}

export interface AdminRecentlyAddedResponse {
  items: AdminRecentlyAddedItem[];
  total: number;
  limit: number;
}

// ─── Notifications ─────────────────────────────────────────────────────────

// NotificationKind: identificador del tipo de notificacion. El frontend
// mapea esto a icono + traduccion + link de fallback. Strings libres -
// añadir un kind nuevo no requiere update del tipo, solo el switch del
// renderizado.
export type NotificationKind =
  | "federation.pairing_request_received"
  | "federation.pairing_request_accepted"
  | "federation.pairing_request_declined"
  | string;

// AppNotification — entrada del inbox del usuario. Renombrada con
// prefijo "App" para no chocar con el global `Notification` del
// browser (que es la Notifications API nativa).
export interface AppNotification {
  id: string;
  kind: NotificationKind;
  title: string;
  body?: string;
  link?: string;
  payload?: unknown;
  created_at: string;
  read_at?: string;
}

// NotificationsResponse — el listing devuelve tanto las notifs como
// el contador de no-leidas en el mismo payload para hidratar el
// dropdown y el badge con un solo fetch.
export interface NotificationsResponse {
  data: AppNotification[];
  unread_count: number;
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
