package iptvhandler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"hubplay/internal/api/handlers"
	"hubplay/internal/auth"
	"hubplay/internal/iptv"
	iptvmodel "hubplay/internal/iptv/model"
)

// channelBrowseOps es el contrato mínimo para el surface de canales
// (listado, detalle, groups, schedule, streaming). 11 de ~50 métodos.
type channelBrowseOps interface {
	GetChannels(ctx context.Context, libraryID string, activeOnly bool) ([]*iptvmodel.Channel, error)
	GetChannelsForUser(ctx context.Context, libraryID, userID string, activeOnly bool) ([]*iptvmodel.Channel, error)
	GetChannelsForUserPersonalisation(ctx context.Context, libraryID, userID string) ([]*iptvmodel.Channel, error)
	GetChannel(ctx context.Context, id string) (*iptvmodel.Channel, error)
	GetGroups(ctx context.Context, libraryID string) ([]string, error)
	GetSchedule(ctx context.Context, channelID string, from, to time.Time) ([]*iptvmodel.EPGProgram, error)
	GetBulkSchedule(ctx context.Context, channelIDs []string, from, to time.Time) (map[string][]*iptvmodel.EPGProgram, error)
	NowPlaying(ctx context.Context, channelID string) (*iptvmodel.EPGProgram, error)
	ListChannelOverrides(ctx context.Context, userID string) ([]iptvmodel.UserChannelOrderEntry, error)
	GetChannelLogoOverride(ctx context.Context, channelID string) (*iptvmodel.ChannelLogoOverride, error)
	GetChannelEPGIcon(ctx context.Context, channelID string) (string, error)
}

type iptvChannelHandler struct {
	svc       channelBrowseOps
	proxy     handlers.IPTVStreamProxyService
	transmux  handlers.IPTVTransmuxer
	logoCache *iptv.LogoCache
	imageDir  string
	access    handlers.LibraryAccessService
	logger    *slog.Logger
}

// ListChannels returns the channels for a library, with the caller's
// per-user order + hidden overlay applied (admins included — they
// can personalise their view too without losing the global defaults).
//
// `?include_hidden=true` is an opt-in for the personalisation panel
// itself, which needs to show every channel (including the ones the
// user has hidden) so the toggle remains reachable.
func (h *iptvChannelHandler) ListChannels(w http.ResponseWriter, r *http.Request) {
	libraryID := handlers.RequireParam(w, r, "id")
	if libraryID == "" {
		return
	}
	if !canAccessLibrary(r, h.access, h.logger, libraryID) {
		iptvDenyForbidden(w, r)
		return
	}
	activeOnly := r.URL.Query().Get("active") != "false"
	includeHidden := r.URL.Query().Get("include_hidden") == "true"

	userID := ""
	if claims := auth.GetClaims(r.Context()); claims != nil {
		userID = claims.UserID
	}

	var (
		channels []*iptvmodel.Channel
		err      error
	)
	switch {
	case userID == "":
		// Admin/system caller sin auth: lista cruda sin overlays.
		channels, err = h.svc.GetChannels(r.Context(), libraryID, activeOnly)
	case includeHidden:
		// Panel /live-tv/customize: necesita TODAS las rows (incluso
		// las hidden por el usuario) ordenadas según SU overlay personal
		// — no según el admin. Antes este path caía al GetChannels
		// crudo y la página mostraba el orden del admin aunque el
		// usuario ya tuviera personalización, que era exactamente lo
		// contrario de lo que el panel debe enseñar.
		channels, err = h.svc.GetChannelsForUserPersonalisation(r.Context(), libraryID, userID)
	default:
		channels, err = h.svc.GetChannelsForUser(r.Context(), libraryID, userID, activeOnly)
	}
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}

	// When include_hidden=true, surface the user's overrides so the
	// panel can mark each row visually. Cheap one-query lookup keyed
	// by user_id; small N (only rows the user has touched).
	hiddenSet := map[string]bool{}
	positionSet := map[string]int{}
	if includeHidden && userID != "" {
		overrides, _ := h.svc.ListChannelOverrides(r.Context(), userID)
		for _, o := range overrides {
			if o.Hidden {
				hiddenSet[o.ChannelID] = true
			}
			positionSet[o.ChannelID] = o.Position
		}
	}

	result := make([]channelDTO, 0, len(channels))
	for _, ch := range channels {
		dto := toChannelDTO(ch, "/api/v1/channels/"+ch.ID+"/stream")
		if hiddenSet[ch.ID] {
			dto.Hidden = true
		}
		if pos, ok := positionSet[ch.ID]; ok {
			dto.UserPosition = pos
		}
		result = append(result, dto)
	}

	handlers.RespondData(w, http.StatusOK, result)
}

// GetChannel returns a single channel.
func (h *iptvChannelHandler) GetChannel(w http.ResponseWriter, r *http.Request) {
	channelID := handlers.RequireParam(w, r, "channelId")
	if channelID == "" {
		return
	}

	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	if !canAccessLibrary(r, h.access, h.logger, ch.LibraryID) {
		iptvDenyForbidden(w, r)
		return
	}

	// Get now playing
	nowPlaying, _ := h.svc.NowPlaying(r.Context(), channelID)

	// Detail endpoint omits stream_url; clients hit /channels/{id}/stream directly.
	dto := toChannelDTO(ch, "")

	// Use a wrapping map so the now_playing extension lives outside the
	// typed DTO — it's optional per-channel and specific to the detail view.
	resp := map[string]any{
		"id":            dto.ID,
		"name":          dto.Name,
		"number":        dto.Number,
		"group_name":    dto.GroupName,
		"category":      dto.Category,
		"logo_url":      dto.LogoURL,
		"logo_initials": dto.LogoInitials,
		"logo_bg":       dto.LogoBg,
		"logo_fg":       dto.LogoFg,
		"tvg_id":        dto.TvgID,
		"language":      dto.Language,
		"country":       dto.Country,
		"is_active":     dto.IsActive,
	}

	if nowPlaying != nil {
		resp["now_playing"] = map[string]any{
			"title":       nowPlaying.Title,
			"description": nowPlaying.Description,
			"category":    nowPlaying.Category,
			"start_time":  nowPlaying.StartTime,
			"end_time":    nowPlaying.EndTime,
		}
	}

	handlers.RespondData(w, http.StatusOK, resp)
}

// Groups returns channel group names for a library.
func (h *iptvChannelHandler) Groups(w http.ResponseWriter, r *http.Request) {
	libraryID := handlers.RequireParam(w, r, "id")
	if libraryID == "" {
		return
	}
	if !canAccessLibrary(r, h.access, h.logger, libraryID) {
		iptvDenyForbidden(w, r)
		return
	}

	groups, err := h.svc.GetGroups(r.Context(), libraryID)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}

	handlers.RespondData(w, http.StatusOK, groups)
}

// Stream proxies a live IPTV stream to the client.
//
// Format dispatch:
//   - HLS upstream (`*.m3u8`) → existing passthrough proxy. The browser
//     player consumes the manifest directly and we just rewrite segment
//     URLs through `/proxy?url=` so auth + the upstream rewriter still
//     apply.
//   - Anything else (typically Xtream Codes raw MPEG-TS over HTTP) →
//     302 redirect to the per-channel HLS transmux endpoint. ffmpeg
//     repackages the MPEG-TS into an HLS sliding window the browser can
//     play, with `-c copy` so CPU stays low.
//
// Falling back to the passthrough proxy when transmux is not configured
// preserves today's behaviour for deployments without ffmpeg — those
// users keep the broken-on-MPEG-TS state we shipped before transmux,
// but at least HLS providers continue to work.
func (h *iptvChannelHandler) Stream(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = handlers.DisableWriteDeadline(w)
	channelID := handlers.RequireParam(w, r, "channelId")
	if channelID == "" {
		return
	}

	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	if !canAccessLibrary(r, h.access, h.logger, ch.LibraryID) {
		iptvDenyForbidden(w, r)
		return
	}

	if !ch.IsActive {
		handlers.RespondError(w, r, http.StatusNotFound, "CHANNEL_INACTIVE", "channel is not active")
		return
	}

	// If the upstream URL doesn't look like HLS and a transmux
	// manager is wired, redirect the player to the transmux entry
	// point. The 302 is cheap (one round-trip) and keeps the format
	// decision out of the proxy code path, which means HLS upstreams
	// keep their existing zero-buffer behaviour.
	if h.transmux != nil && !iptv.IsHLSURL(ch.StreamURL) {
		http.Redirect(w, r, "/api/v1/channels/"+channelID+"/hls/index.m3u8", http.StatusFound)
		return
	}

	if err := h.proxy.ProxyStream(r.Context(), w, channelID, ch.StreamURL); err != nil {
		h.logger.Error("stream proxy error", "channel", channelID, "error", err)
		// Don't write error — response may already be partially written
	}
}

// HLSManifest serves the live HLS playlist produced by the per-channel
// transmux session, spawning ffmpeg if no session is running. The
// returned manifest references segment files served by HLSSegment.
//
// Cache headers force every reload: the manifest is a live sliding
// window and any client-side cache breaks the player's ability to see
// new segments.
func (h *iptvChannelHandler) HLSManifest(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = handlers.DisableWriteDeadline(w)
	if h.transmux == nil {
		handlers.RespondError(w, r, http.StatusNotImplemented, "TRANSMUX_DISABLED",
			"live transmux is not enabled on this server")
		return
	}
	channelID := handlers.RequireParam(w, r, "channelId")
	if channelID == "" {
		return
	}

	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	if !canAccessLibrary(r, h.access, h.logger, ch.LibraryID) {
		iptvDenyForbidden(w, r)
		return
	}
	if !ch.IsActive {
		handlers.RespondError(w, r, http.StatusNotFound, "CHANNEL_INACTIVE", "channel is not active")
		return
	}

	sess, err := h.transmux.GetOrStart(r.Context(), channelID, ch.StreamURL)
	if err != nil {
		var coe *iptv.CircuitOpenError
		switch {
		case errors.As(err, &coe):
			// Breaker tripped after repeated failures — fast-fail with
			// 503 + Retry-After so the player backs off instead of
			// driving another doomed ffmpeg spawn cycle.
			retry := int(coe.RetryAfter.Seconds())
			if retry < 1 {
				retry = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			handlers.RespondError(w, r, http.StatusServiceUnavailable, "CIRCUIT_OPEN",
				"channel is in cooldown after repeated upstream failures; retry shortly")
		case errors.Is(err, iptv.ErrTooManySessions):
			// 503 with Retry-After lets the player do its own retry
			// after the reaper has freed an idle slot.
			w.Header().Set("Retry-After", "5")
			handlers.RespondError(w, r, http.StatusServiceUnavailable, "TRANSMUX_BUSY",
				"server is at maximum simultaneous transmux sessions; retry shortly")
		case errors.Is(err, iptv.ErrTransmuxFailed):
			handlers.RespondError(w, r, http.StatusBadGateway, "TRANSMUX_FAILED",
				"upstream stream could not be transmuxed; channel may be offline or use an unsupported codec")
		case errors.Is(err, r.Context().Err()):
			// Client gave up — no response needed.
		default:
			h.logger.Error("transmux GetOrStart", "channel", channelID, "error", err)
			handlers.RespondError(w, r, http.StatusInternalServerError, "TRANSMUX_ERROR",
				"failed to start transmux session")
		}
		return
	}

	body, err := os.ReadFile(sess.ManifestPath())
	if err != nil {
		// Warn y no Error: hls.js pide el manifest cada 2-6s; si la sesión
		// está arrancando o muerta, generaríamos Error por request → spam.
		// El status 500 al cliente sí queda — el operador ve el patrón si
		// persiste sin saturar logs.
		h.logger.Warn("transmux read manifest", "channel", channelID, "error", err)
		handlers.RespondError(w, r, http.StatusInternalServerError, "MANIFEST_UNAVAILABLE",
			"transmux manifest is not yet available")
		return
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", handlers.CacheControlNoStoreFull)
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	_, _ = w.Write(body)
}

// ChannelLogo serves the cached upstream logo for a channel through
// a same-origin URL. The frontend always renders <img> against this
// endpoint so a strict img-src CSP doesn't have to whitelist every
// random image-host the M3U happens to use, and no upstream host
// gets to track the user.
//
// 404 is the right status for "no logo to show" — empty upstream
// URL, fetch failure, SSRF rejection, non-image response. The React
// `<ChannelCard>` has an onError handler that swaps to the
// initials/colour avatar, so the UI degrades gracefully without
// any extra client wiring.
func (h *iptvChannelHandler) ChannelLogo(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = handlers.DisableWriteDeadline(w)
	if h.logoCache == nil {
		handlers.RespondError(w, r, http.StatusNotFound, "NO_LOGO", "logo cache disabled")
		return
	}
	channelID := handlers.RequireParam(w, r, "channelId")
	if channelID == "" {
		return
	}

	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	if !canAccessLibrary(r, h.access, h.logger, ch.LibraryID) {
		iptvDenyForbidden(w, r)
		return
	}

	// Cascada de resolución del logo:
	//   1. override.logo_file → fichero local subido por el admin
	//   2. override.logo_url  → URL externa pegada por el admin
	//   3. channels.logo_url  → tvg-logo del M3U
	// El override desempata frente al M3U porque la intención del admin
	// (rematch manual) siempre gana.
	effectiveLogoURL := ch.LogoURL
	if override, oErr := h.svc.GetChannelLogoOverride(r.Context(), channelID); oErr == nil && override != nil {
		if override.LogoFile != "" && h.imageDir != "" {
			// Local file route: bypass el cache remoto, sirve directo
			// desde disco. El basename ya fue validado en el upload
			// (IsSafePathSegment) así que no puede ser path traversal.
			serveLocalChannelLogo(w, r, h.imageDir, h.logger, channelID, override.LogoFile)
			return
		}
		if override.LogoURL != "" {
			effectiveLogoURL = override.LogoURL
		}
	}

	// Último fallback antes del 404: el icono que el EPG haya recolectado
	// para programas de este canal (XMLTV `<icon>`). Cubre el caso muy
	// común de feeds M3U sin tvg-logo cuyo XMLTV asociado sí trae
	// iconos por canal. Mismo cache remoto + proxy CSP que el resto.
	if effectiveLogoURL == "" {
		if epgIcon, eErr := h.svc.GetChannelEPGIcon(r.Context(), channelID); eErr == nil && epgIcon != "" {
			effectiveLogoURL = epgIcon
		}
	}

	if effectiveLogoURL == "" {
		handlers.RespondError(w, r, http.StatusNotFound, "NO_LOGO", "channel has no upstream logo")
		return
	}

	path, err := h.logoCache.Path(r.Context(), effectiveLogoURL)
	if err != nil {
		// Fetch / SSRF / decode failures collapse to 404 by design:
		// the frontend's onError fallback is the right answer for
		// every "no logo to show" condition. The cache logs at
		// debug, so operators still have visibility.
		handlers.RespondError(w, r, http.StatusNotFound, "LOGO_UNAVAILABLE", "could not fetch upstream logo")
		return
	}

	f, err := os.Open(path)
	if err != nil {
		h.logger.Error("logo cache read", "channel", channelID, "path", path, "error", err)
		handlers.RespondError(w, r, http.StatusInternalServerError, "LOGO_READ_FAILED", "could not read cached logo")
		return
	}
	defer f.Close() //nolint:errcheck

	info, err := f.Stat()
	if err != nil {
		h.logger.Error("logo cache stat", "channel", channelID, "path", path, "error", err)
		handlers.RespondError(w, r, http.StatusInternalServerError, "LOGO_STAT_FAILED", "")
		return
	}

	// Sniff content-type from the first 512 bytes, then rewind.
	// We can't trust the upstream Content-Type (some hosts mis-tag
	// PNGs as octet-stream) and storing the type alongside the
	// cache file would mean a sidecar metadata format we'd have to
	// maintain. Reading 512 bytes is cheaper than that, and
	// http.ServeContent picks up from there.
	var head [512]byte
	n, _ := f.Read(head[:])
	contentType := http.DetectContentType(head[:n])
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		h.logger.Error("logo cache seek", "channel", channelID, "error", err)
		handlers.RespondError(w, r, http.StatusInternalServerError, "LOGO_SEEK_FAILED", "")
		return
	}

	w.Header().Set("Content-Type", contentType)
	// max-age=86400 is a day; channel logos are extremely stable in
	// practice (branding assets, not live data) and a stale logo is
	// the cheapest possible bug. ServeContent honours
	// If-Modified-Since for conditional requests, so the actual
	// bytes are usually only sent once per browser cache lifetime.
	w.Header().Set("Cache-Control", handlers.CacheControlDailyPublic)
	http.ServeContent(w, r, "", info.ModTime(), f)
}

// HLSSegment serves one MPEG-TS segment file from the channel's
// transmux session. Each request bumps the session's last-touch so
// the idle reaper keeps the session alive while the player is
// consuming segments at the live edge.
//
// 404 is the right answer when the session has expired (e.g. user
// paused for 60+ s and we reaped it): hls.js handles it by reloading
// the manifest, which respawns the session and resumes playback.
func (h *iptvChannelHandler) HLSSegment(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = handlers.DisableWriteDeadline(w)
	if h.transmux == nil {
		handlers.RespondError(w, r, http.StatusNotImplemented, "TRANSMUX_DISABLED",
			"live transmux is not enabled on this server")
		return
	}
	channelID := handlers.RequireParam(w, r, "channelId")
	if channelID == "" {
		return
	}
	segment := handlers.RequireParam(w, r, "segment")
	if segment == "" {
		return
	}

	if !iptv.IsValidSegmentName(segment) {
		// Path traversal guard: ffmpeg only writes seg-NNNNN.ts and
		// anything else is either an attack or stale state from a
		// player using a manifest that no longer matches the session.
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_SEGMENT",
			"segment name does not match the expected pattern")
		return
	}

	// We don't recheck library ACL on every segment fetch — the
	// player only ever sees a segment URL after a successful manifest
	// fetch, which already enforces ACL. Adding a per-segment DB hit
	// would 6× the database load on a busy live channel for no real
	// security gain.
	sess, err := h.transmux.Touch(channelID)
	if err != nil {
		handlers.RespondError(w, r, http.StatusNotFound, "NO_TRANSMUX_SESSION",
			"transmux session has expired; reload the manifest to resume")
		return
	}

	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", handlers.CacheControlShortLived)
	http.ServeFile(w, r, sess.SegmentPath(segment))
}

// ProxyURL proxies an HLS segment or sub-playlist for a channel.
func (h *iptvChannelHandler) ProxyURL(w http.ResponseWriter, r *http.Request) {
	channelID := handlers.RequireParam(w, r, "channelId")
	if channelID == "" {
		return
	}
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		handlers.RespondError(w, r, http.StatusBadRequest, "MISSING_URL", "url parameter required")
		return
	}

	// Authorisation: resolve the channel's library and check access. The
	// proxy-itself validates the upstream URL against SSRF, but we must
	// still confirm the caller owns the channel they're proxying through.
	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	if !canAccessLibrary(r, h.access, h.logger, ch.LibraryID) {
		iptvDenyForbidden(w, r)
		return
	}

	if err := h.proxy.ProxyURL(r.Context(), w, channelID, rawURL); err != nil {
		h.logger.Error("proxy URL error", "channel", channelID, "error", err)
	}
}

// Schedule returns EPG schedule for a channel.
func (h *iptvChannelHandler) Schedule(w http.ResponseWriter, r *http.Request) {
	channelID := handlers.RequireParam(w, r, "channelId")
	if channelID == "" {
		return
	}

	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	if !canAccessLibrary(r, h.access, h.logger, ch.LibraryID) {
		iptvDenyForbidden(w, r)
		return
	}

	from, to := parseTimeRange(r)

	programs, err := h.svc.GetSchedule(r.Context(), channelID, from, to)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}

	result := make([]map[string]any, 0, len(programs))
	for _, p := range programs {
		result = append(result, programToJSON(p))
	}

	handlers.RespondData(w, http.StatusOK, result)
}

// bulkScheduleMaxChannels caps how many channels a single request may
// ask about. Keeps a single bad/mistaken POST from pinning a SQLite
// connection while we iterate thousands of IN() chunks. The cap is
// generous enough that a whole country's EPG fits in one round-trip.
const bulkScheduleMaxChannels = 5000

// bulkScheduleRequest is the POST body for the bulk schedule endpoint.
// Times use the same "hours relative to now" convention as the GET
// variant (handled by parseBulkTimeRange) so both shapes are
// interchangeable.
type bulkScheduleRequest struct {
	Channels []string `json:"channels"`
	From     string   `json:"from,omitempty"`
	To       string   `json:"to,omitempty"`
}

// BulkSchedule returns EPG for multiple channels at once.
//
// Accepts both GET (?channels=a,b,c) and POST (JSON body). POST is the
// preferred transport: a library with ~250 channels already produces a
// query string big enough to trip a 414 at common nginx defaults, so
// the React client always POSTs. GET stays supported for curl/ad-hoc
// debugging on small libraries.
//
// Each channel is filtered individually through the ACL — inaccessible
// channels are dropped silently (no error) so a single restricted channel
// doesn't poison a bulk call for an otherwise-authorised user.
func (h *iptvChannelHandler) BulkSchedule(w http.ResponseWriter, r *http.Request) {
	bq, ok := h.parseBulkScheduleRequest(w, r)
	if !ok {
		return
	}

	allowed := make([]string, 0, len(bq.ChannelIDs))
	for _, id := range bq.ChannelIDs {
		if id == "" {
			continue
		}
		ch, err := h.svc.GetChannel(r.Context(), id)
		if err != nil {
			continue // canal desconocido — skip sin 500
		}
		if canAccessLibrary(r, h.access, h.logger, ch.LibraryID) {
			allowed = append(allowed, id)
		}
	}
	if len(allowed) == 0 {
		handlers.RespondData(w, http.StatusOK, map[string]any{})
		return
	}

	schedules, err := h.svc.GetBulkSchedule(r.Context(), allowed, bq.From, bq.To)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}

	result := make(map[string]any)
	for chID, programs := range schedules {
		progs := make([]map[string]any, 0, len(programs))
		for _, p := range programs {
			progs = append(progs, programToJSON(p))
		}
		result[chID] = progs
	}

	handlers.RespondData(w, http.StatusOK, result)
}

// bulkScheduleQuery agrupa los parámetros parseados de una petición
// de schedule bulk (GET query o POST JSON body).
type bulkScheduleQuery struct {
	ChannelIDs []string
	From       time.Time
	To         time.Time
}

// parseBulkScheduleRequest normaliza los dos transportes (GET query,
// POST JSON body) en un bulkScheduleQuery. En error escribe la
// respuesta y devuelve ok=false; el caller debe salir.
func (h *iptvChannelHandler) parseBulkScheduleRequest(w http.ResponseWriter, r *http.Request) (bulkScheduleQuery, bool) {
	if r.Method == http.MethodPost {
		// Body cap a 1 MiB — suficiente para 5k UUIDs de canal.
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		defer r.Body.Close() //nolint:errcheck

		var body bulkScheduleRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid JSON body")
			return bulkScheduleQuery{}, false
		}
		if len(body.Channels) == 0 {
			handlers.RespondError(w, r, http.StatusBadRequest, "MISSING_CHANNELS", "channels field required")
			return bulkScheduleQuery{}, false
		}
		if len(body.Channels) > bulkScheduleMaxChannels {
			handlers.RespondError(w, r, http.StatusBadRequest, "TOO_MANY_CHANNELS",
				fmt.Sprintf("at most %d channels per request", bulkScheduleMaxChannels))
			return bulkScheduleQuery{}, false
		}
		from, to := parseBulkTimeRange(body.From, body.To)
		return bulkScheduleQuery{ChannelIDs: body.Channels, From: from, To: to}, true
	}

	// GET fallback para curl / listas pequeñas.
	raw := r.URL.Query().Get("channels")
	if raw == "" {
		handlers.RespondError(w, r, http.StatusBadRequest, "MISSING_CHANNELS", "channels parameter required")
		return bulkScheduleQuery{}, false
	}
	ids := strings.Split(raw, ",")
	if len(ids) > bulkScheduleMaxChannels {
		handlers.RespondError(w, r, http.StatusBadRequest, "TOO_MANY_CHANNELS",
			fmt.Sprintf("at most %d channels per request", bulkScheduleMaxChannels))
		return bulkScheduleQuery{}, false
	}
	from, to := parseTimeRange(r)
	return bulkScheduleQuery{ChannelIDs: ids, From: from, To: to}, true
}

func parseTimeRange(r *http.Request) (time.Time, time.Time) {
	return parseBulkTimeRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
}

// parseBulkTimeRange resolves the optional `from`/`to` params shared by
// the GET and POST variants of the schedule endpoints. Accepts either
// RFC3339 timestamps ("2026-04-24T12:00:00Z") or bare integers
// interpreted as hours (`from=6` → 6h ago, `to=12` → 12h from now).
// Empty values fall back to the default ±window.
func parseBulkTimeRange(fromRaw, toRaw string) (time.Time, time.Time) {
	now := time.Now()
	from := now.Add(-2 * time.Hour) // default: 2h ago
	to := now.Add(24 * time.Hour)   // default: 24h from now

	if fromRaw != "" {
		if t, err := time.Parse(time.RFC3339, fromRaw); err == nil {
			from = t
		} else if hours, err := strconv.Atoi(fromRaw); err == nil {
			from = now.Add(-time.Duration(hours) * time.Hour)
		}
	}
	if toRaw != "" {
		if t, err := time.Parse(time.RFC3339, toRaw); err == nil {
			to = t
		} else if hours, err := strconv.Atoi(toRaw); err == nil {
			to = now.Add(time.Duration(hours) * time.Hour)
		}
	}

	return from, to
}

func programToJSON(p *iptvmodel.EPGProgram) map[string]any {
	return map[string]any{
		"id":          p.ID,
		"title":       p.Title,
		"description": p.Description,
		"category":    p.Category,
		"icon_url":    p.IconURL,
		"start_time":  p.StartTime,
		"end_time":    p.EndTime,
	}
}
