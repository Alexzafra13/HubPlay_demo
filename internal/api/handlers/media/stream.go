package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"hubplay/internal/api/handlers"
	"hubplay/internal/auth"
	"hubplay/internal/domain"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/provider"
	"hubplay/internal/stream"
)

// validSegmentName matches only safe HLS segment filenames (e.g. segment00001.ts, stream.m3u8).
var validSegmentName = regexp.MustCompile(`^(segment\d{5}\.ts|stream\.m3u8)$`)

// StreamHandler serves media streams via HLS or direct play.
type StreamHandler struct {
	manager        handlers.StreamManagerService
	items          handlers.ItemRepository
	streams        handlers.MediaStreamRepository
	externalIDs    handlers.ExternalIDRepository
	providers      handlers.ProviderManager
	access         handlers.LibraryAccessService
	settings       settingsReader
	baseURLDefault string
	logger         *slog.Logger
}

// NewStreamHandler creates a new stream handler.
//
// `externalIDs` and `providers` are optional — when nil, the external
// subtitle endpoints return 503 instead of 500. Older test envs and
// installs without OpenSubtitles configured keep working without
// rewiring.
//
// `settings` is the runtime override layer (app_settings); when nil the
// handler falls back to the boot-time `baseURL` exclusively, which is
// what tests rely on. `baseURL` itself is the YAML / env default that
// survives a missing or empty DB row.
//
// `access` enforces the per-library ACL on every content-exposing
// endpoint (playback info, HLS playlists, direct play, subtitles).
// Production always wires it; when nil (minimal test builds) the gate
// passes through, mirroring the nil-guard idiom used for the optional
// collaborators above.
func NewStreamHandler(
	manager handlers.StreamManagerService,
	items handlers.ItemRepository,
	streams handlers.MediaStreamRepository,
	externalIDs handlers.ExternalIDRepository,
	providers handlers.ProviderManager,
	access handlers.LibraryAccessService,
	settings settingsReader,
	baseURL string,
	logger *slog.Logger,
) *StreamHandler {
	return &StreamHandler{
		manager:        manager,
		items:          items,
		streams:        streams,
		externalIDs:    externalIDs,
		providers:      providers,
		access:         access,
		settings:       settings,
		baseURLDefault: strings.TrimRight(baseURL, "/"),
		logger:         logger.With("module", "stream-handler"),
	}
}

// authorizeItem enforces the per-library ACL for the authenticated
// caller against the item's library. On denial it writes a 404
// (enumeration-safe — same as the federation stream surface, so a
// caller can't distinguish "no access" from "doesn't exist") and
// returns false; the caller must stop. When the access service isn't
// wired (nil) the gate passes through.
//
// Closes the broken-access-control gap where the library ACL was
// enforced for IPTV and federation streaming but not for local VOD /
// HLS playback, letting any authenticated user stream any item by ID.
func (h *StreamHandler) authorizeItem(w http.ResponseWriter, r *http.Request, item *librarymodel.Item) bool {
	if h.access == nil {
		return true
	}
	if handlers.CanAccessLibrary(r, h.access, h.logger, item.LibraryID) {
		return true
	}
	handlers.RespondError(w, r, http.StatusNotFound, "NOT_FOUND", "item not found")
	return false
}

// effectiveBaseURL resolves the runtime base URL: app_settings override
// if the admin set one, YAML / env default otherwise. Trailing slash
// is trimmed once at request time so the playlist + redirect formats
// below stay clean.
func (h *StreamHandler) effectiveBaseURL(ctx context.Context) string {
	if h.settings == nil {
		return h.baseURLDefault
	}
	value, err := h.settings.GetOr(ctx, "server.base_url", h.baseURLDefault)
	if err != nil {
		h.logger.Warn("read base_url override", "error", err)
		return h.baseURLDefault
	}
	return strings.TrimRight(value, "/")
}

// Info returns playback info for an item (what method will be used, available profiles).
func (h *StreamHandler) Info(w http.ResponseWriter, r *http.Request) {
	itemID := handlers.RequireParam(w, r, "itemId")
	if itemID == "" {
		return
	}

	item, err := h.items.GetByID(r.Context(), itemID)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	if !h.authorizeItem(w, r, item) {
		return
	}

	mediaStreams, err := h.streams.ListByItem(r.Context(), itemID)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}

	caps := stream.CapabilitiesFromRequest(r)
	// ?audio=N: la decisión depende de la pista que va a sonar (una
	// pista DTS no-default fuerza transcode de audio aunque la default
	// sea AAC). Mismo parseo que MasterPlaylist; ausente → -1 (default).
	audioStreamIndex := -1
	if a := r.URL.Query().Get("audio"); a != "" {
		if v, err := strconv.Atoi(a); err == nil && v >= 0 {
			audioStreamIndex = v
		}
	}
	// Honour the runtime `playback.force_direct_play` admin toggle —
	// the /info response is what drives the player's pill, so the
	// method shown to the user must match what StartSession will
	// actually pick. Without this the panel would read "Transcode"
	// for every item even when the manager was wired to short-circuit
	// to DirectPlay, which is exactly the visibility gap the toggle
	// is meant to close.
	var decision stream.PlaybackDecision
	if h.settings != nil {
		v, _ := h.settings.GetOr(r.Context(), "playback.force_direct_play", "false")
		if v == "true" {
			decision = stream.DecideForceDirectPlay(item, mediaStreams)
		}
	}
	if decision.Method == "" {
		decision = stream.Decide(item, mediaStreams, caps, "", audioStreamIndex)
	}
	profiles := stream.ProfileNames()

	handlers.RespondData(w, http.StatusOK, map[string]any{
		"item_id":     itemID,
		"method":      decision.Method,
		"video_codec": decision.VideoCodec,
		"audio_codec": decision.AudioCodec,
		"container":   decision.Container,
		"profiles":    profiles,
	})
}

// MasterPlaylist returns the HLS master playlist (M3U8) with adaptive bitrate variants.
func (h *StreamHandler) MasterPlaylist(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = handlers.DisableWriteDeadline(w)
	itemID := handlers.RequireParam(w, r, "itemId")
	if itemID == "" {
		return
	}

	// Verify item exists and the caller may access its library.
	item, err := h.items.GetByID(r.Context(), itemID)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	if !h.authorizeItem(w, r, item) {
		return
	}

	// Carry the user's preferred-audio choice (or the in-player
	// switcher's override) into every variant URL the master
	// playlist emits, so hls.js bitrate switches keep the same dub.
	audioStreamIndex := -1
	if a := r.URL.Query().Get("audio"); a != "" {
		if v, err := strconv.Atoi(a); err == nil && v >= 0 {
			audioStreamIndex = v
		}
	}
	// Same for the burned-in subtitle. ?subtitle=N picks a
	// PGS / DVDSUB / ASS stream to render into the video frames.
	// Missing / unparseable → no burn-in (the player will fall back
	// to whatever native sub track it advertises, if any).
	burnSubIndex := -1
	if s := r.URL.Query().Get("subtitle"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v >= 0 {
			burnSubIndex = v
		}
	}
	profiles := []string{"1080p", "720p", "480p", "360p"}
	playlist := stream.GenerateMasterPlaylist(itemID, h.effectiveBaseURL(r.Context()), profiles, audioStreamIndex, burnSubIndex)

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", handlers.CacheControlNoCache)
	_, _ = fmt.Fprint(w, playlist)
}

// QualityPlaylist returns the HLS playlist for a specific quality level.
// It starts a transcode session if one doesn't exist.
// segmentDurationSeconds is the value the transcoder hands to ffmpeg
// via -hls_time and the value the synthesized manifest declares per
// segment. Keep them in lockstep — if one moves, the other has to.
const segmentDurationSeconds float64 = 6

func (h *StreamHandler) QualityPlaylist(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = handlers.DisableWriteDeadline(w)
	itemID := handlers.RequireParam(w, r, "itemId")
	if itemID == "" {
		return
	}
	quality := handlers.RequireParam(w, r, "quality")
	if quality == "" {
		return
	}

	claims := auth.GetClaims(r.Context())
	if claims == nil {
		handlers.RespondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	// Resolve + authorize the item BEFORE starting a session: otherwise
	// an unauthorised caller would spawn a transcode session (resource
	// burn) that the segment handler — which only checks the session
	// exists — would then happily serve, defeating the ACL.
	item, err := h.items.GetByID(r.Context(), itemID)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	if !h.authorizeItem(w, r, item) {
		return
	}

	startTime := parseFloat(r.URL.Query().Get("start"))

	// Optional audio track override. The client passes ?audio=<n>
	// (0-based index into the source file's audio streams) when
	// the user has a preferred-language preference or has switched
	// audio from the player menu. Missing / unparseable defaults to
	// -1 which lets ffmpeg auto-pick the source's default audio.
	audioStreamIndex := -1
	if a := r.URL.Query().Get("audio"); a != "" {
		if v, err := strconv.Atoi(a); err == nil && v >= 0 {
			audioStreamIndex = v
		}
	}
	// Optional subtitle burn-in. ?subtitle=<n> picks a per-type
	// subtitle index (PGS / DVDSUB / ASS) to render into the video
	// frames. -1 = no burn-in. The manager only honours indices
	// pointing at burnable codecs; pointing at an SRT track is a
	// no-op (those ride as native HLS sub tracks elsewhere).
	burnSubIndex := -1
	if s := r.URL.Query().Get("subtitle"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v >= 0 {
			burnSubIndex = v
		}
	}
	// Client capabilities (codecs/containers) come in via the
	// X-Hubplay-Client-Capabilities header. nil means "no header sent",
	// in which case the stream manager + Decide() fall back to the
	// conservative web-browser defaults — no behaviour change for
	// today's web client which doesn't send the header yet.
	caps := stream.CapabilitiesFromRequest(r)

	ms, err := h.manager.StartSession(r.Context(), stream.StartSessionRequest{
		UserID:           claims.UserID,
		ItemID:           itemID,
		ProfileName:      quality,
		Caps:             caps,
		StartTime:        startTime,
		AudioStreamIndex: audioStreamIndex,
		BurnSubIndex:     burnSubIndex,
	})
	if err != nil {
		// The manager returns typed AppErrors (e.g. TranscodeBusy) that
		// handleServiceError renders without leaking internal messages.
		h.logger.Error("failed to start session", "error", err)
		handlers.HandleServiceError(w, r, err)
		return
	}

	if ms.Decision.Method == stream.MethodDirectPlay {
		// Redirect to direct play
		http.Redirect(w, r, fmt.Sprintf("%s/api/v1/stream/%s/direct", h.effectiveBaseURL(r.Context()), itemID), http.StatusTemporaryRedirect)
		return
	}

	// Synthesized VOD manifest: built from the item's known duration
	// rather than waiting for ffmpeg to produce its own .m3u8. This
	// is what unlocks free seeking — the playlist declares every
	// segment up-front with `EXT-X-PLAYLIST-TYPE:VOD` + `EXT-X-ENDLIST`,
	// so hls.js stops treating the stream as live and lets the user
	// scrub anywhere on the timeline. Segments that ffmpeg hasn't
	// reached yet are produced on-demand by the segment handler
	// (which restarts ffmpeg at the right offset on miss).
	//
	// We still need item.DurationTicks to compute the segment count.
	// For sources where the duration is unknown (very rare — usually
	// a scan-in-progress item), fall back to the legacy behaviour of
	// serving ffmpeg's own progressively-grown manifest. The user
	// loses free seeking on those items, which is the best we can
	// do without knowing how long the stream is. `item` was already
	// resolved + authorized above.
	if item.DurationTicks > 0 {
		duration := float64(item.DurationTicks) / 10_000_000
		// Carry the audio + subtitle params forward into segment URLs
		// so hls.js keeps the same dub AND burned-in subtitle on every
		// fetch. Without this, the segment handler can't reconstruct
		// the session key (which embeds both indices) and every .ts
		// request 404s with SESSION_NOT_FOUND.
		params := make([]string, 0, 2)
		if audioStreamIndex >= 0 {
			params = append(params, fmt.Sprintf("audio=%d", audioStreamIndex))
		}
		if burnSubIndex >= 0 {
			params = append(params, fmt.Sprintf("subtitle=%d", burnSubIndex))
		}
		segSuffix := ""
		if len(params) > 0 {
			segSuffix = "?" + strings.Join(params, "&")
		}
		segmentTpl := fmt.Sprintf("/api/v1/stream/%s/%s/segment%%05d.ts%s", itemID, quality, segSuffix)
		manifest := stream.SynthesizeVODManifest(duration, segmentDurationSeconds, segmentTpl)

		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", handlers.CacheControlNoCache)
		_, _ = w.Write([]byte(manifest))
		return
	}

	// Fallback: legacy ffmpeg-driven manifest. Only reachable when
	// item duration is unknown.
	manifestPath := ms.ManifestPath()
	if err := waitForFile(manifestPath, 10*time.Second); err != nil {
		handlers.HandleServiceError(w, r, domain.NewTranscodePending())
		return
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", handlers.CacheControlNoCache)
	http.ServeFile(w, r, manifestPath)
}

// segmentIndexPattern extracts the integer index from a segment
// filename like "segment00042.ts". Used by the seek-restart path to
// figure out where to restart ffmpeg when the requested segment file
// doesn't exist yet.
var segmentIndexPattern = regexp.MustCompile(`^segment(\d+)\.ts$`)

// Segment serves an HLS segment (.ts file) from a transcode session.
//
// Flow:
//
//  1. If the file is already on disk, serve it (fast path — happens
//     for sequential playback once ffmpeg is producing).
//  2. If not, wait briefly (~2 s) in case ffmpeg is one segment
//     behind — sequential playback can momentarily race.
//  3. If still missing, the client is asking for a segment ahead of
//     where ffmpeg is encoding — the user just clicked the seek bar
//     somewhere far. Restart ffmpeg at that segment's offset and
//     wait up to 15 s for the new ffmpeg to produce the file (with
//     `-c:v copy` this is typically <2 s).
//  4. If still nothing, give up with 404. The synthesized manifest
//     keeps offering the URL, so the player will retry.
func (h *StreamHandler) Segment(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = handlers.DisableWriteDeadline(w)
	itemID := handlers.RequireParam(w, r, "itemId")
	if itemID == "" {
		return
	}
	quality := handlers.RequireParam(w, r, "quality")
	if quality == "" {
		return
	}
	segmentFile := handlers.RequireParam(w, r, "segment")
	if segmentFile == "" {
		return
	}

	claims := auth.GetClaims(r.Context())
	if claims == nil {
		handlers.RespondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	// Audio + subtitle indices have to ride on every segment URL —
	// the manifest synthesizer threaded both params into each
	// segment URL so the session key we compute here matches the
	// one Manager.StartSession registered. Missing/unparseable
	// defaults to -1, which matches the master.m3u8 path that omits
	// the param entirely.
	audioStreamIndex := -1
	if a := r.URL.Query().Get("audio"); a != "" {
		if v, err := strconv.Atoi(a); err == nil && v >= 0 {
			audioStreamIndex = v
		}
	}
	burnSubIndex := -1
	if s := r.URL.Query().Get("subtitle"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v >= 0 {
			burnSubIndex = v
		}
	}
	key := stream.SessionKey(claims.UserID, itemID, quality, audioStreamIndex, burnSubIndex)
	ms, ok := h.manager.GetSession(key)
	if !ok {
		handlers.RespondError(w, r, http.StatusNotFound, "SESSION_NOT_FOUND", "no active transcode session")
		return
	}

	// Validate segment filename: must match expected pattern (e.g. segment00001.ts, stream.m3u8)
	if !validSegmentName.MatchString(segmentFile) {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_SEGMENT", "invalid segment filename")
		return
	}

	segmentPath := filepath.Join(ms.OutputDir, segmentFile)

	// Double-check: resolved path must stay within output directory
	if filepath.Dir(segmentPath) != ms.OutputDir {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_SEGMENT", "invalid segment path")
		return
	}

	// Fast path + short tolerance for "ffmpeg is one segment behind".
	if err := waitForFile(segmentPath, 2*time.Second); err == nil {
		serveSegment(w, r, segmentPath)
		return
	}

	// Slow path: file isn't appearing. The user probably seeked far
	// ahead. Parse the segment index out of the filename, restart
	// ffmpeg at that offset, and wait again. Restart is cheap with
	// stream-copy (the typical case for h264 sources after the
	// DirectStream fix).
	matches := segmentIndexPattern.FindStringSubmatch(segmentFile)
	if len(matches) != 2 {
		// Not a numbered segment (e.g. stream.m3u8 fallback) — keep
		// the legacy long wait.
		if err := waitForFile(segmentPath, 30*time.Second); err != nil {
			handlers.RespondError(w, r, http.StatusNotFound, "SEGMENT_NOT_FOUND", "segment not available")
			return
		}
		serveSegment(w, r, segmentPath)
		return
	}
	segIdx, parseErr := strconv.Atoi(matches[1])
	if parseErr != nil {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_SEGMENT", "invalid segment index")
		return
	}

	if err := h.manager.RestartSessionAt(key, segIdx, segmentDurationSeconds); err != nil {
		// Rate-limit hits are visibility-only: the client most
		// likely has a seek-loop bug and the right behaviour is to
		// stop encouraging it. 429 with Retry-After lets a healthy
		// client back off and resume cleanly; a buggy one keeps
		// retrying but at least no longer melts the transcoder.
		if errors.Is(err, stream.ErrRestartRateLimited) {
			w.Header().Set("Retry-After", "5")
			handlers.RespondError(w, r, http.StatusTooManyRequests, "RESTART_RATE_LIMITED", "too many seek restarts in a short window")
			return
		}
		h.logger.Error("seek restart failed", "key", key, "segment", segIdx, "error", err)
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "RESTART_FAILED", "could not restart transcode at requested offset")
		return
	}

	if err := waitForFile(segmentPath, 15*time.Second); err != nil {
		handlers.RespondError(w, r, http.StatusNotFound, "SEGMENT_NOT_FOUND", "segment not available after restart")
		return
	}
	serveSegment(w, r, segmentPath)
}

// serveSegment writes the segment file with the right HLS headers.
// Pulled out so the fast and seek-restart paths can both use it
// without duplicating the headers.
func serveSegment(w http.ResponseWriter, r *http.Request, path string) {
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", handlers.CacheControlHourly)
	http.ServeFile(w, r, path)
}

// DirectPlay serves the original media file via progressive download / range requests.
func (h *StreamHandler) DirectPlay(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = handlers.DisableWriteDeadline(w)
	itemID := handlers.RequireParam(w, r, "itemId")
	if itemID == "" {
		return
	}

	item, err := h.items.GetByID(r.Context(), itemID)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	if !h.authorizeItem(w, r, item) {
		return
	}

	if item.Path == "" || !item.IsAvailable {
		handlers.RespondError(w, r, http.StatusNotFound, "FILE_NOT_FOUND", "media file not available")
		return
	}

	// IsAvailable solo se refresca al escanear: si el fichero se borró
	// hace 5 minutos, ServeFile devolvería un 404 text/plain con el
	// Content-Type de vídeo ya puesto — rompiendo el contrato JSON de
	// error. Stat explícito primero. PB-25 (audit 2026-06-10).
	if _, err := os.Stat(item.Path); err != nil {
		handlers.RespondError(w, r, http.StatusNotFound, "FILE_NOT_FOUND", "media file not available")
		return
	}

	// Detect content type from container
	contentType := containerToMIME(item.Container)
	w.Header().Set("Content-Type", contentType)

	http.ServeFile(w, r, item.Path)
}

// StopSession stops a streaming session.
func (h *StreamHandler) StopSession(w http.ResponseWriter, r *http.Request) {
	itemID := handlers.RequireParam(w, r, "itemId")
	if itemID == "" {
		return
	}

	claims := auth.GetClaims(r.Context())
	if claims == nil {
		handlers.RespondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	// Stop EVERY session for this (user, item) — the player accreted
	// one per (quality, audioStreamIndex) tuple during ABR + audio
	// switches and the close button doesn't know which ones survived.
	// Without the bulk-stop the per-user cap kept hoarding zombies
	// and 503'd new playbacks. The legacy ?quality= param is ignored
	// for the same reason: the client doesn't track audio config so
	// a precise per-key delete would still leak the others.
	h.manager.StopSessionsByItem(claims.UserID, itemID)
	w.WriteHeader(http.StatusNoContent)
}

// waitForFile polls for a file to exist on disk, used to wait for FFmpeg output.
func waitForFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s", filepath.Base(path))
}

func parseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func containerToMIME(container string) string {
	parts := strings.Split(container, ",")
	for _, p := range parts {
		switch strings.TrimSpace(p) {
		case "mp4", "mov":
			return "video/mp4"
		case "webm":
			return "video/webm"
		case "matroska", "mkv":
			return "video/x-matroska"
		case "avi":
			return "video/x-msvideo"
		case "mpegts", "ts":
			return "video/mp2t"
		}
	}
	return "video/mp4"
}

// Subtitles returns available subtitle tracks for an item.
func (h *StreamHandler) Subtitles(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = handlers.DisableWriteDeadline(w)
	itemID := handlers.RequireParam(w, r, "itemId")
	if itemID == "" {
		return
	}

	// Authorize against the item's library before listing its tracks.
	item, err := h.items.GetByID(r.Context(), itemID)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	if !h.authorizeItem(w, r, item) {
		return
	}

	mediaStreams, err := h.streams.ListByItem(r.Context(), itemID)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}

	var subs []map[string]any
	for _, s := range mediaStreams {
		if s.StreamType != "subtitle" {
			continue
		}
		subs = append(subs, map[string]any{
			"index":    s.StreamIndex,
			"codec":    s.Codec,
			"language": s.Language,
			"title":    s.Title,
			"forced":   s.IsForced,
			"default":  s.IsDefault,
		})
	}

	handlers.RespondData(w, http.StatusOK, subs)
}

// SearchExternalSubtitles queries every registered subtitle provider
// for matches against the given item. Languages can be filtered via
// the `lang` query param (comma-separated ISO codes, e.g. `en,es`);
// no filter returns whatever the provider considers default.
//
// 503 when providers / external IDs aren't wired, 404 when the item
// doesn't exist. Returns whatever the providers return — empty list
// is a valid response (no matches), not an error.
func (h *StreamHandler) SearchExternalSubtitles(w http.ResponseWriter, r *http.Request) {
	if h.providers == nil || h.externalIDs == nil {
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "PROVIDERS_UNAVAILABLE",
			"external subtitle providers are not configured")
		return
	}
	itemID := handlers.RequireParam(w, r, "itemId")
	if itemID == "" {
		return
	}

	item, err := h.items.GetByID(r.Context(), itemID)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	if !h.authorizeItem(w, r, item) {
		return
	}

	extIDs, err := h.externalIDs.ListByItem(r.Context(), itemID)
	if err != nil {
		h.logger.Warn("list external ids", "item_id", itemID, "error", err)
	}
	idMap := make(map[string]string, len(extIDs))
	for _, e := range extIDs {
		idMap[e.Provider] = e.ExternalID
	}

	var langs []string
	if l := r.URL.Query().Get("lang"); l != "" {
		langs = strings.Split(l, ",")
	}

	query := provider.SubtitleQuery{
		Title:       item.Title,
		Year:        item.Year,
		ExternalIDs: idMap,
		Languages:   langs,
		ItemType:    subtitleItemType(item.Type),
	}
	if item.SeasonNumber != nil {
		query.SeasonNumber = item.SeasonNumber
	}
	if item.EpisodeNumber != nil {
		query.EpisodeNumber = item.EpisodeNumber
	}

	results, err := h.providers.SearchSubtitles(r.Context(), query)
	if err != nil {
		h.logger.Warn("search subtitles", "item_id", itemID, "error", err)
		handlers.RespondError(w, r, http.StatusBadGateway, "PROVIDER_ERROR",
			"subtitle provider lookup failed")
		return
	}

	out := make([]map[string]any, len(results))
	for i, r := range results {
		out[i] = map[string]any{
			"source":   r.Source,
			"file_id":  r.URL, // OpenSubtitles uses the URL slot for fileID
			"language": r.Language,
			"format":   r.Format,
			"score":    r.Score,
		}
	}
	handlers.RespondData(w, http.StatusOK, out)
}

// DownloadExternalSubtitle pulls bytes from the named provider, runs
// them through ffmpeg to coerce the output to WebVTT (the only format
// every browser's `<track>` reliably handles), and serves the result.
// The query param `source` selects the provider (`opensubtitles`,
// `subscene`, …); `file_id` is whatever opaque handle that provider
// uses — for OpenSubtitles it's the integer file ID returned by the
// search endpoint above.
//
// Currently no on-disk cache: the player picks one external sub per
// session and the browser caches the VTT after the first hit. Once
// repeat-hit traffic is observed in the wild, a per-(item, file_id)
// cache under `<dataDir>/subtitles/` is the obvious next step.
func (h *StreamHandler) DownloadExternalSubtitle(w http.ResponseWriter, r *http.Request) {
	if h.providers == nil {
		handlers.RespondError(w, r, http.StatusServiceUnavailable, "PROVIDERS_UNAVAILABLE",
			"external subtitle providers are not configured")
		return
	}
	source := r.URL.Query().Get("source")
	fileID := handlers.RequireParam(w, r, "fileId")
	if fileID == "" {
		return
	}
	if source == "" || fileID == "" {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_REQUEST",
			"source query param and file_id path param are required")
		return
	}

	raw, err := h.providers.DownloadSubtitle(r.Context(), source, fileID)
	if err != nil {
		h.logger.Warn("download subtitle", "source", source, "file_id", fileID, "error", err)
		handlers.RespondError(w, r, http.StatusBadGateway, "PROVIDER_ERROR", "subtitle download failed")
		return
	}

	vtt, err := stream.ConvertSubtitleToVTT(r.Context(), raw)
	if err != nil {
		h.logger.Warn("convert subtitle", "source", source, "file_id", fileID, "error", err)
		handlers.RespondError(w, r, http.StatusBadGateway, "CONVERSION_FAILED", "failed to convert subtitle to vtt")
		return
	}

	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", handlers.CacheControlImage)
	_, _ = w.Write(vtt)
}

// subtitleItemType maps the DB-level item type onto the provider-level
// enum the SubtitleQuery expects. Unknown values fall through to
// Movie, matching the metadata fetch behaviour upstream.
func subtitleItemType(t string) provider.ItemType {
	switch t {
	case "series", "season":
		return provider.ItemSeries
	case "episode":
		return provider.ItemEpisode
	default:
		return provider.ItemMovie
	}
}

// SubtitleTrack extracts and serves a subtitle track as WebVTT.
func (h *StreamHandler) SubtitleTrack(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = handlers.DisableWriteDeadline(w)
	itemID := handlers.RequireParam(w, r, "itemId")
	if itemID == "" {
		return
	}
	trackRaw := handlers.RequireParam(w, r, "trackIndex")
	if trackRaw == "" {
		return
	}
	trackIndex, _ := strconv.Atoi(trackRaw)

	item, err := h.items.GetByID(r.Context(), itemID)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	if !h.authorizeItem(w, r, item) {
		return
	}

	if item.Path == "" {
		handlers.RespondError(w, r, http.StatusNotFound, "FILE_NOT_FOUND", "media file not available")
		return
	}

	vttData, err := stream.ExtractSubtitleVTT(r.Context(), item.Path, trackIndex)
	if err != nil {
		h.logger.Error("subtitle extraction failed", "error", err)
		handlers.RespondError(w, r, http.StatusInternalServerError, "SUBTITLE_ERROR", "failed to extract subtitle")
		return
	}

	w.Header().Set("Content-Type", "text/vtt")
	w.Header().Set("Cache-Control", handlers.CacheControlDailyOpaque)
	_, _ = io.Copy(w, vttData)
}
