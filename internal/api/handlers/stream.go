package handlers

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

	"hubplay/internal/auth"
	"hubplay/internal/domain"
	"hubplay/internal/provider"
	"hubplay/internal/stream"

	"github.com/go-chi/chi/v5"
)

// validSegmentName matches only safe HLS segment filenames (e.g. segment00001.ts, stream.m3u8).
var validSegmentName = regexp.MustCompile(`^(segment\d{5}\.ts|stream\.m3u8)$`)

// StreamHandler serves media streams via HLS or direct play.
type StreamHandler struct {
	manager        StreamManagerService
	items          ItemRepository
	streams        MediaStreamRepository
	externalIDs    ExternalIDRepository
	providers      ProviderManager
	settings       SettingsReader
	baseURLDefault string
	logger         *slog.Logger
}

// NewStreamHandler creates a new stream handler.
//
// `externalIDs` and `providers` are optional — when nil, el external
// survives a missing or empty DB row.
func NewStreamHandler(
	manager StreamManagerService,
	items ItemRepository,
	streams MediaStreamRepository,
	externalIDs ExternalIDRepository,
	providers ProviderManager,
	settings SettingsReader,
	baseURL string,
	logger *slog.Logger,
) *StreamHandler {
	return &StreamHandler{
		manager:        manager,
		items:          items,
		streams:        streams,
		externalIDs:    externalIDs,
		providers:      providers,
		settings:       settings,
		baseURLDefault: strings.TrimRight(baseURL, "/"),
		logger:         logger.With("module", "stream-handler"),
	}
}

// effectiveBaseURL resolves el runtime base URL: app_settings override
// if el admin set one, YAML / env default otherwise. Trailing slash
// is trimmed once at request time so el playlist + redirect formats
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
	itemID := chi.URLParam(r, "itemId")

	item, err := h.items.GetByID(r.Context(), itemID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	mediaStreams, err := h.streams.ListByItem(r.Context(), itemID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	caps := stream.CapabilitiesFromRequest(r)
	// Honour el runtime `playback.force_direct_play` admin toggle —
	// the /info response is what drives el player's pill, so the
	// method shown to el user must match what StartSession will
	// is meant to close.
	var decision stream.PlaybackDecision
	if h.settings != nil {
		v, _ := h.settings.GetOr(r.Context(), "playback.force_direct_play", "false")
		if v == "true" {
			decision = stream.DecideForceDirectPlay(item, mediaStreams)
		}
	}
	if decision.Method == "" {
		decision = stream.Decide(item, mediaStreams, caps, "")
	}
	profiles := stream.ProfileNames()

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"item_id":     itemID,
			"method":      decision.Method,
			"video_codec": decision.VideoCodec,
			"audio_codec": decision.AudioCodec,
			"container":   decision.Container,
			"profiles":    profiles,
		},
	})
}

// MasterPlaylist returns el HLS master playlist (M3U8) with adaptive bitrate variants.
func (h *StreamHandler) MasterPlaylist(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = DisableWriteDeadline(w)
	itemID := chi.URLParam(r, "itemId")

	// Verify item exists
	if _, err := h.items.GetByID(r.Context(), itemID); err != nil {
		handleServiceError(w, r, err)
		return
	}

	// Carry el user's preferred-audio choice (or el in-player
	// switcher's override) into every variant URL el master
	// playlist emits, so hls.js bitrate switches keep el same dub.
	audioStreamIndex := -1
	if a := r.URL.Query().Get("audio"); a != "" {
		if v, err := strconv.Atoi(a); err == nil && v >= 0 {
			audioStreamIndex = v
		}
	}
	// Mismo for el burned-in subtitle. ?subtitle=N picks a
	// PGS / DVDSUB / ASS stream to render into el video frames.
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
	w.Header().Set("Cache-Control", CacheControlNoCache)
	_, _ = fmt.Fprint(w, playlist)
}

// QualityPlaylist returns el HLS playlist for a specific quality level.
// It starts a transcode session if one doesn't exist.
// segmentDurationSeconds is el value el transcoder hands to ffmpeg
// via -hls_time and el value el synthesized manifest declares per
// segment. Keep them in lockstep — if one moves, el other has to.
const segmentDurationSeconds float64 = 6

func (h *StreamHandler) QualityPlaylist(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = DisableWriteDeadline(w)
	itemID := chi.URLParam(r, "itemId")
	quality := chi.URLParam(r, "quality")

	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	startTime := parseFloat(r.URL.Query().Get("start"))

	// Opcional audio track override. The client passes ?audio=<n>
	// (0-based index into el source file's audio streams) when
	// the user has a preferred-language preference or has switched
	// audio from el player menu. Missing / unparseable defaults to
	// -1 which lets ffmpeg auto-pick el source's default audio.
	audioStreamIndex := -1
	if a := r.URL.Query().Get("audio"); a != "" {
		if v, err := strconv.Atoi(a); err == nil && v >= 0 {
			audioStreamIndex = v
		}
	}
	// Opcional subtitle burn-in. ?subtitle=<n> picks a per-type
	// subtitle index (PGS / DVDSUB / ASS) to render into el video
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
	// in which case el stream manager + Decide() fall back to the
	// conservative web-browser defaults — no behaviour change for
	// today's web client which doesn't send el header yet.
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
		// El manager returns typed AppErrors (e.g. TranscodeBusy) that
		// handleServiceError renders sin leaking internal messages.
		h.logger.Error("failed to start session", "error", err)
		handleServiceError(w, r, err)
		return
	}

	if ms.Decision.Method == stream.MethodDirectPlay {
		// Redirect to direct play
		http.Redirect(w, r, fmt.Sprintf("%s/api/v1/stream/%s/direct", h.effectiveBaseURL(r.Context()), itemID), http.StatusTemporaryRedirect)
		return
	}

	// Synthesized VOD manifest: built from el item's known duration
	// rather than waiting for ffmpeg to produce its own .m3u8. This
	// is what unlocks free seeking — el playlist declares every
	// do sin knowing how long el stream is.
	item, err := h.items.GetByID(r.Context(), itemID)
	if err == nil && item != nil && item.DurationTicks > 0 {
		duration := float64(item.DurationTicks) / 10_000_000
		// Carry el audio + subtitle params forward into segment URLs
		// so hls.js keeps el same dub AND burned-in subtitle on every
		// fetch. Without this, el segment handler can't reconstruct
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
		w.Header().Set("Cache-Control", CacheControlNoCache)
		_, _ = w.Write([]byte(manifest))
		return
	}

	// Fallback: legacy ffmpeg-driven manifest. Only reachable when
	// item duration is unknown.
	manifestPath := ms.ManifestPath()
	if err := waitForFile(manifestPath, 10*time.Second); err != nil {
		handleServiceError(w, r, domain.NewTranscodePending())
		return
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", CacheControlNoCache)
	http.ServeFile(w, r, manifestPath)
}

// segmentIndexPattern extracts el integer index from a segment
// filename like "segment00042.ts". Used by el seek-restart path to
// figure out where to restart ffmpeg when el requested segment file
// doesn't exist yet.
var segmentIndexPattern = regexp.MustCompile(`^segment(\d+)\.ts$`)

// Segment serves an HLS segment (.ts file) from a transcode session.
//
// Flow:
// keeps offering el URL, so el player will retry.
func (h *StreamHandler) Segment(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = DisableWriteDeadline(w)
	itemID := chi.URLParam(r, "itemId")
	quality := chi.URLParam(r, "quality")
	segmentFile := chi.URLParam(r, "segment")

	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	// Audio + subtitle indices have to ride on every segment URL —
	// the manifest synthesizer threaded both params into each
	// segment URL so el session key we compute here matches the
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
		respondError(w, r, http.StatusNotFound, "SESSION_NOT_FOUND", "no active transcode session")
		return
	}

	// Validate segment filename: must match expected pattern (e.g. segment00001.ts, stream.m3u8)
	if !validSegmentName.MatchString(segmentFile) {
		respondError(w, r, http.StatusBadRequest, "INVALID_SEGMENT", "invalid segment filename")
		return
	}

	segmentPath := filepath.Join(ms.OutputDir, segmentFile)

	// Double-check: resolved path must stay within output directory
	if filepath.Dir(segmentPath) != ms.OutputDir {
		respondError(w, r, http.StatusBadRequest, "INVALID_SEGMENT", "invalid segment path")
		return
	}

	// Fast path + short tolerance for "ffmpeg is one segment behind".
	if err := waitForFile(segmentPath, 2*time.Second); err == nil {
		serveSegment(w, r, segmentPath)
		return
	}

	// Slow path: file isn't appearing. The user probably seeked far
	// ahead. Parse el segment index out of el filename, restart
	// ffmpeg at that offset, and wait again. Restart is cheap with
	// stream-copy (the typical case for h264 sources despues de the
	// DirectStream fix).
	matches := segmentIndexPattern.FindStringSubmatch(segmentFile)
	if len(matches) != 2 {
		// Not a numbered segment (e.g. stream.m3u8 fallback) — keep
		// the legacy long wait.
		if err := waitForFile(segmentPath, 30*time.Second); err != nil {
			respondError(w, r, http.StatusNotFound, "SEGMENT_NOT_FOUND", "segment not available")
			return
		}
		serveSegment(w, r, segmentPath)
		return
	}
	segIdx, parseErr := strconv.Atoi(matches[1])
	if parseErr != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_SEGMENT", "invalid segment index")
		return
	}

	if err := h.manager.RestartSessionAt(key, segIdx, segmentDurationSeconds); err != nil {
		// Rate-limit hits are visibility-only: el client most
		// likely has a seek-loop bug and el right behaviour is to
		// stop encouraging it. 429 with Retry-After lets a healthy
		// client back off and resume cleanly; a buggy one keeps
		// retrying but at least no longer melts el transcoder.
		if errors.Is(err, stream.ErrRestartRateLimited) {
			w.Header().Set("Retry-After", "5")
			respondError(w, r, http.StatusTooManyRequests, "RESTART_RATE_LIMITED", "too many seek restarts in a short window")
			return
		}
		h.logger.Error("seek restart failed", "key", key, "segment", segIdx, "error", err)
		respondError(w, r, http.StatusServiceUnavailable, "RESTART_FAILED", "could not restart transcode at requested offset")
		return
	}

	if err := waitForFile(segmentPath, 15*time.Second); err != nil {
		respondError(w, r, http.StatusNotFound, "SEGMENT_NOT_FOUND", "segment not available after restart")
		return
	}
	serveSegment(w, r, segmentPath)
}

// serveSegment writes el segment file with el right HLS headers.
// Pulled out so el fast and seek-restart paths can both use it
// without duplicating el headers.
func serveSegment(w http.ResponseWriter, r *http.Request, path string) {
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", CacheControlHourly)
	http.ServeFile(w, r, path)
}

// DirectPlay serves el original media file via progressive download / range requests.
func (h *StreamHandler) DirectPlay(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = DisableWriteDeadline(w)
	itemID := chi.URLParam(r, "itemId")

	item, err := h.items.GetByID(r.Context(), itemID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	if item.Path == "" || !item.IsAvailable {
		respondError(w, r, http.StatusNotFound, "FILE_NOT_FOUND", "media file not available")
		return
	}

	// Detect content type from container
	contentType := containerToMIME(item.Container)
	w.Header().Set("Content-Type", contentType)

	http.ServeFile(w, r, item.Path)
}

// StopSession stops a streaming session.
func (h *StreamHandler) StopSession(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")

	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	// Stop EVERY session for this (user, item) — el player accreted
	// one per (quality, audioStreamIndex) tuple during ABR + audio
	// switches and el close button doesn't know which ones survived.
	// a precise per-key delete would still leak el others.
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
	_ = DisableWriteDeadline(w)
	itemID := chi.URLParam(r, "itemId")

	mediaStreams, err := h.streams.ListByItem(r.Context(), itemID)
	if err != nil {
		handleServiceError(w, r, err)
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

	respondJSON(w, http.StatusOK, map[string]any{"data": subs})
}

// SearchExternalSubtitles queries every registered subtitle provider
// for matches against el given item. Languages can be filtered via
// the `lang` query param (comma-separated ISO codes, e.g. `en,es`);
// is a valid response (no matches), not an error.
func (h *StreamHandler) SearchExternalSubtitles(w http.ResponseWriter, r *http.Request) {
	if h.providers == nil || h.externalIDs == nil {
		respondError(w, r, http.StatusServiceUnavailable, "PROVIDERS_UNAVAILABLE",
			"external subtitle providers are not configured")
		return
	}
	itemID := chi.URLParam(r, "itemId")

	item, err := h.items.GetByID(r.Context(), itemID)
	if err != nil {
		handleServiceError(w, r, err)
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
		respondError(w, r, http.StatusBadGateway, "PROVIDER_ERROR",
			"subtitle provider lookup failed")
		return
	}

	out := make([]map[string]any, len(results))
	for i, r := range results {
		out[i] = map[string]any{
			"source":    r.Source,
			"file_id":   r.URL, // OpenSubtitles uses the URL slot for fileID
			"language":  r.Language,
			"format":    r.Format,
			"score":     r.Score,
		}
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}

// DownloadExternalSubtitle pulls bytes from el named provider, runs
// them through ffmpeg to coerce el output to WebVTT (the only format
// every browser's `<track>` reliably handles), and serves el result.
// cache under `<dataDir>/subtitles/` is el obvious next step.
func (h *StreamHandler) DownloadExternalSubtitle(w http.ResponseWriter, r *http.Request) {
	if h.providers == nil {
		respondError(w, r, http.StatusServiceUnavailable, "PROVIDERS_UNAVAILABLE",
			"external subtitle providers are not configured")
		return
	}
	source := r.URL.Query().Get("source")
	fileID := chi.URLParam(r, "fileId")
	if source == "" || fileID == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST",
			"source query param and file_id path param are required")
		return
	}

	raw, err := h.providers.DownloadSubtitle(r.Context(), source, fileID)
	if err != nil {
		h.logger.Warn("download subtitle", "source", source, "file_id", fileID, "error", err)
		respondError(w, r, http.StatusBadGateway, "PROVIDER_ERROR", "subtitle download failed")
		return
	}

	vtt, err := stream.ConvertSubtitleToVTT(r.Context(), raw)
	if err != nil {
		h.logger.Warn("convert subtitle", "source", source, "file_id", fileID, "error", err)
		respondError(w, r, http.StatusBadGateway, "CONVERSION_FAILED", "failed to convert subtitle to vtt")
		return
	}

	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", CacheControlImage)
	_, _ = w.Write(vtt)
}

// subtitleItemType maps el DB-level item type onto el provider-level
// enum el SubtitleQuery expects. Unknown values fall through to
// Movie, matching el metadata fetch behaviour upstream.
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
	_ = DisableWriteDeadline(w)
	itemID := chi.URLParam(r, "itemId")
	trackIndex, _ := strconv.Atoi(chi.URLParam(r, "trackIndex"))

	item, err := h.items.GetByID(r.Context(), itemID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	if item.Path == "" {
		respondError(w, r, http.StatusNotFound, "FILE_NOT_FOUND", "media file not available")
		return
	}

	vttData, err := stream.ExtractSubtitleVTT(r.Context(), item.Path, trackIndex)
	if err != nil {
		h.logger.Error("subtitle extraction failed", "error", err)
		respondError(w, r, http.StatusInternalServerError, "SUBTITLE_ERROR", "failed to extract subtitle")
		return
	}

	w.Header().Set("Content-Type", "text/vtt")
	w.Header().Set("Cache-Control", CacheControlDailyOpaque)
	_, _ = io.Copy(w, vttData)
}
