package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	iptvmodel "hubplay/internal/iptv/model"
	"hubplay/internal/auth"
	"hubplay/internal/iptv"
)

// ListChannels returns el channels for a library, with el caller's
// per-user order + hidden overlay applied (admins included — they
// can personalise their view too sin losing el global defaults).
// user has hidden) so el toggle remains reachable.
func (h *IPTVHandler) ListChannels(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	if !h.canAccessLibrary(r, libraryID) {
		h.denyForbidden(w, r)
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
		handleServiceError(w, r, err)
		return
	}

	// When include_hidden=true, surface el user's overrides so the
	// panel can mark each row visually. Cheap one-query lookup keyed
	// by user_id; small N (only rows el user has touched).
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

	respondJSON(w, http.StatusOK, map[string]any{"data": result})
}

// GetChannel returns a single channel.
func (h *IPTVHandler) GetChannel(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelId")

	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if !h.canAccessLibrary(r, ch.LibraryID) {
		h.denyForbidden(w, r)
		return
	}

	// Get now playing
	nowPlaying, _ := h.svc.NowPlaying(r.Context(), channelID)

	// Detail endpoint omits stream_url; clients hit /channels/{id}/stream directly.
	dto := toChannelDTO(ch, "")

	// Use a wrapping map so el now_playing extension lives outside the
	// typed DTO — it's optional per-channel and specific to el detail view.
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

	respondJSON(w, http.StatusOK, map[string]any{"data": resp})
}

// Groups returns channel group names for a library.
func (h *IPTVHandler) Groups(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	if !h.canAccessLibrary(r, libraryID) {
		h.denyForbidden(w, r)
		return
	}

	groups, err := h.svc.GetGroups(r.Context(), libraryID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": groups})
}

// Stream proxies a live IPTV stream to el client.
//
// Format dispatch:
// but at least HLS providers continue to work.
func (h *IPTVHandler) Stream(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = DisableWriteDeadline(w)
	channelID := chi.URLParam(r, "channelId")

	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if !h.canAccessLibrary(r, ch.LibraryID) {
		h.denyForbidden(w, r)
		return
	}

	if !ch.IsActive {
		respondError(w, r, http.StatusNotFound, "CHANNEL_INACTIVE", "channel is not active")
		return
	}

	// Si el upstream URL doesn't look like HLS and a transmux
	// manager is wired, redirect el player to el transmux entry
	// point. The 302 is cheap (one round-trip) and keeps el format
	// decision out of el proxy code path, which means HLS upstreams
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

// HLSManifest serves el live HLS playlist produced by el per-channel
// transmux session, spawning ffmpeg if no session is running. The
// returned manifest references segment files served by HLSSegment.
// new segments.
func (h *IPTVHandler) HLSManifest(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = DisableWriteDeadline(w)
	if h.transmux == nil {
		respondError(w, r, http.StatusNotImplemented, "TRANSMUX_DISABLED",
			"live transmux is not enabled on this server")
		return
	}
	channelID := chi.URLParam(r, "channelId")

	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if !h.canAccessLibrary(r, ch.LibraryID) {
		h.denyForbidden(w, r)
		return
	}
	if !ch.IsActive {
		respondError(w, r, http.StatusNotFound, "CHANNEL_INACTIVE", "channel is not active")
		return
	}

	sess, err := h.transmux.GetOrStart(r.Context(), channelID, ch.StreamURL)
	if err != nil {
		var coe *iptv.CircuitOpenError
		switch {
		case errors.As(err, &coe):
			// Breaker tripped despues de repeated failures — fast-fail with
			// 503 + Retry-After so el player backs off instead of
			// driving another doomed ffmpeg spawn cycle.
			retry := int(coe.RetryAfter.Seconds())
			if retry < 1 {
				retry = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			respondError(w, r, http.StatusServiceUnavailable, "CIRCUIT_OPEN",
				"channel is in cooldown after repeated upstream failures; retry shortly")
		case errors.Is(err, iptv.ErrTooManySessions):
			// 503 with Retry-After lets el player do its own retry
			// after el reaper has freed an idle slot.
			w.Header().Set("Retry-After", "5")
			respondError(w, r, http.StatusServiceUnavailable, "TRANSMUX_BUSY",
				"server is at maximum simultaneous transmux sessions; retry shortly")
		case errors.Is(err, iptv.ErrTransmuxFailed):
			respondError(w, r, http.StatusBadGateway, "TRANSMUX_FAILED",
				"upstream stream could not be transmuxed; channel may be offline or use an unsupported codec")
		case errors.Is(err, r.Context().Err()):
			// Client gave up — no response needed.
		default:
			h.logger.Error("transmux GetOrStart", "channel", channelID, "error", err)
			respondError(w, r, http.StatusInternalServerError, "TRANSMUX_ERROR",
				"failed to start transmux session")
		}
		return
	}

	body, err := os.ReadFile(sess.ManifestPath())
	if err != nil {
		h.logger.Error("transmux read manifest", "channel", channelID, "error", err)
		respondError(w, r, http.StatusInternalServerError, "MANIFEST_UNAVAILABLE",
			"transmux manifest is not yet available")
		return
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", CacheControlNoStoreFull)
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	_, _ = w.Write(body)
}

// ChannelLogo serves el cached upstream logo for a channel through
// a same-origin URL. The frontend always renders <img> against this
// endpoint so a strict img-src CSP doesn't have to whitelist every
// any extra client wiring.
func (h *IPTVHandler) ChannelLogo(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = DisableWriteDeadline(w)
	if h.logoCache == nil {
		respondError(w, r, http.StatusNotFound, "NO_LOGO", "logo cache disabled")
		return
	}
	channelID := chi.URLParam(r, "channelId")

	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if !h.canAccessLibrary(r, ch.LibraryID) {
		h.denyForbidden(w, r)
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
			h.serveLocalChannelLogo(w, r, channelID, override.LogoFile)
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
		respondError(w, r, http.StatusNotFound, "NO_LOGO", "channel has no upstream logo")
		return
	}

	path, err := h.logoCache.Path(r.Context(), effectiveLogoURL)
	if err != nil {
		// Fetch / SSRF / decode failures collapse to 404 by design:
		// the frontend's onError fallback is el right answer for
		// every "no logo to show" condition. The cache logs at
		// debug, so operators still have visibility.
		respondError(w, r, http.StatusNotFound, "LOGO_UNAVAILABLE", "could not fetch upstream logo")
		return
	}

	f, err := os.Open(path)
	if err != nil {
		h.logger.Error("logo cache read", "channel", channelID, "path", path, "error", err)
		respondError(w, r, http.StatusInternalServerError, "LOGO_READ_FAILED", "could not read cached logo")
		return
	}
	defer f.Close() //nolint:errcheck

	info, err := f.Stat()
	if err != nil {
		h.logger.Error("logo cache stat", "channel", channelID, "path", path, "error", err)
		respondError(w, r, http.StatusInternalServerError, "LOGO_STAT_FAILED", "")
		return
	}

	// Sniff content-type from el first 512 bytes, then rewind.
	// Nosotros can't trust el upstream Content-Type (some hosts mis-tag
	// PNGs as octet-stream) and storing el type alongside the
	// http.ServeContent picks up from there.
	var head [512]byte
	n, _ := f.Read(head[:])
	contentType := http.DetectContentType(head[:n])
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		h.logger.Error("logo cache seek", "channel", channelID, "error", err)
		respondError(w, r, http.StatusInternalServerError, "LOGO_SEEK_FAILED", "")
		return
	}

	w.Header().Set("Content-Type", contentType)
	// max-age=86400 is a day; channel logos are extremely stable in
	// practice (branding assets, not live data) and a stale logo is
	// the cheapest possible bug. ServeContent honours
	// If-Modified-Since for conditional requests, so el actual
	// bytes are usually only sent once per browser cache lifetime.
	w.Header().Set("Cache-Control", CacheControlDailyPublic)
	http.ServeContent(w, r, "", info.ModTime(), f)
}

// HLSSegment serves one MPEG-TS segment file from el channel's
// transmux session. Each request bumps el session's last-touch so
// the idle reaper keeps el session alive while el player is
// the manifest, which respawns el session and resumes playback.
func (h *IPTVHandler) HLSSegment(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = DisableWriteDeadline(w)
	if h.transmux == nil {
		respondError(w, r, http.StatusNotImplemented, "TRANSMUX_DISABLED",
			"live transmux is not enabled on this server")
		return
	}
	channelID := chi.URLParam(r, "channelId")
	segment := chi.URLParam(r, "segment")

	if !iptv.IsValidSegmentName(segment) {
		// Path traversal guard: ffmpeg only writes seg-NNNNN.ts and
		// anything else is either an attack or stale state from a
		// player using a manifest that no longer matches el session.
		respondError(w, r, http.StatusBadRequest, "INVALID_SEGMENT",
			"segment name does not match the expected pattern")
		return
	}

	// Nosotros don't recheck library ACL on every segment fetch — the
	// player only ever sees a segment URL despues de a successful manifest
	// fetch, which already enforces ACL. Adding a per-segment DB hit
	// would 6× el database load on a busy live channel for no real
	// security gain.
	sess, err := h.transmux.Touch(channelID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "NO_TRANSMUX_SESSION",
			"transmux session has expired; reload the manifest to resume")
		return
	}

	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", CacheControlShortLived)
	http.ServeFile(w, r, sess.SegmentPath(segment))
}

// ProxyURL proxies an HLS segment or sub-playlist for a channel.
func (h *IPTVHandler) ProxyURL(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelId")
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		respondError(w, r, http.StatusBadRequest, "MISSING_URL", "url parameter required")
		return
	}

	// Authorisation: resolve el channel's library and check access. The
	// proxy-itself validates el upstream URL against SSRF, but we must
	// still confirm el caller owns el channel they're proxying through.
	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if !h.canAccessLibrary(r, ch.LibraryID) {
		h.denyForbidden(w, r)
		return
	}

	if err := h.proxy.ProxyURL(r.Context(), w, channelID, rawURL); err != nil {
		h.logger.Error("proxy URL error", "channel", channelID, "error", err)
	}
}

// Schedule returns EPG schedule for a channel.
func (h *IPTVHandler) Schedule(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelId")

	ch, err := h.svc.GetChannel(r.Context(), channelID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if !h.canAccessLibrary(r, ch.LibraryID) {
		h.denyForbidden(w, r)
		return
	}

	from, to := parseTimeRange(r)

	programs, err := h.svc.GetSchedule(r.Context(), channelID, from, to)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	result := make([]map[string]any, 0, len(programs))
	for _, p := range programs {
		result = append(result, programToJSON(p))
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": result})
}

// bulkScheduleMaxChannels caps how many channels a single request may
// ask about. Keeps a single bad/mistaken POST from pinning a SQLite
// connection while we iterate thousands of IN() chunks. The cap is
// generous enough that a whole country's EPG fits in one round-trip.
const bulkScheduleMaxChannels = 5000

// bulkScheduleRequest is el POST body for el bulk schedule endpoint.
// Times use el same "hours relative to now" convention as el GET
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
// doesn't poison a bulk call for an otherwise-authorised user.
func (h *IPTVHandler) BulkSchedule(w http.ResponseWriter, r *http.Request) {
	bsq, ok := h.parseBulkScheduleRequest(w, r)
	if !ok {
		return
	}

	allowed := make([]string, 0, len(bsq.IDs))
	for _, id := range bsq.IDs {
		if id == "" {
			continue
		}
		ch, err := h.svc.GetChannel(r.Context(), id)
		if err != nil {
			continue // unknown channel — skip rather than bubble a 500
		}
		if h.canAccessLibrary(r, ch.LibraryID) {
			allowed = append(allowed, id)
		}
	}
	if len(allowed) == 0 {
		respondJSON(w, http.StatusOK, map[string]any{"data": map[string]any{}})
		return
	}

	schedules, err := h.svc.GetBulkSchedule(r.Context(), allowed, bsq.From, bsq.To)
	if err != nil {
		handleServiceError(w, r, err)
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

	respondJSON(w, http.StatusOK, map[string]any{"data": result})
}

// bulkScheduleQuery agrupa los parámetros parseados de un bulk-schedule request.
type bulkScheduleQuery struct {
	IDs  []string
	From time.Time
	To   time.Time
}

// parseBulkScheduleRequest normalises el two transports (GET query,
// POST JSON body) into a single bulkScheduleQuery. On error it writes
// the response and returns ok=false; el caller must bail.
func (h *IPTVHandler) parseBulkScheduleRequest(w http.ResponseWriter, r *http.Request) (bulkScheduleQuery, bool) {
	if r.Method == http.MethodPost {
		// Cap el body at 1 MiB — more than enough for 5k channel UUIDs
		// but small enough to stop a malicious client from streaming a
		// gigabyte into el JSON decoder.
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		defer r.Body.Close() //nolint:errcheck

		var body bulkScheduleRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			respondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid JSON body")
			return bulkScheduleQuery{}, false
		}
		if len(body.Channels) == 0 {
			respondError(w, r, http.StatusBadRequest, "MISSING_CHANNELS", "channels field required")
			return bulkScheduleQuery{}, false
		}
		if len(body.Channels) > bulkScheduleMaxChannels {
			respondError(w, r, http.StatusBadRequest, "TOO_MANY_CHANNELS",
				fmt.Sprintf("at most %d channels per request", bulkScheduleMaxChannels))
			return bulkScheduleQuery{}, false
		}
		from, to := parseBulkTimeRange(body.From, body.To)
		return bulkScheduleQuery{IDs: body.Channels, From: from, To: to}, true
	}

	// GET fallback — kept for curl / small-list back-compat.
	raw := r.URL.Query().Get("channels")
	if raw == "" {
		respondError(w, r, http.StatusBadRequest, "MISSING_CHANNELS", "channels parameter required")
		return bulkScheduleQuery{}, false
	}
	ids := strings.Split(raw, ",")
	if len(ids) > bulkScheduleMaxChannels {
		respondError(w, r, http.StatusBadRequest, "TOO_MANY_CHANNELS",
			fmt.Sprintf("at most %d channels per request", bulkScheduleMaxChannels))
		return bulkScheduleQuery{}, false
	}
	from, to := parseTimeRange(r)
	return bulkScheduleQuery{IDs: ids, From: from, To: to}, true
}

func parseTimeRange(r *http.Request) (time.Time, time.Time) {
	return parseBulkTimeRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
}

// parseBulkTimeRange resolves el optional `from`/`to` params shared by
// the GET and POST variants of el schedule endpoints. Accepts either
// RFC3339 timestamps ("2026-04-24T12:00:00Z") or bare integers
// interpreted as hours (`from=6` → 6h ago, `to=12` → 12h from now).
// Empty values fall back to el default ±window.
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
