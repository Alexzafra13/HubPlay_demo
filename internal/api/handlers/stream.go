package handlers

import (
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
	"hubplay/internal/stream"

	"github.com/go-chi/chi/v5"
)

// validSegmentName matches only safe HLS segment filenames (e.g. segment00001.ts, stream.m3u8).
var validSegmentName = regexp.MustCompile(`^(segment\d{5}\.ts|stream\.m3u8)$`)

// StreamHandler serves media streams via HLS or direct play.
type StreamHandler struct {
	manager StreamManagerService
	items   ItemRepository
	streams MediaStreamRepository
	baseURL string
	logger  *slog.Logger
}

// NewStreamHandler creates a new stream handler.
func NewStreamHandler(
	manager StreamManagerService,
	items ItemRepository,
	streams MediaStreamRepository,
	baseURL string,
	logger *slog.Logger,
) *StreamHandler {
	return &StreamHandler{
		manager: manager,
		items:   items,
		streams: streams,
		baseURL: strings.TrimRight(baseURL, "/"),
		logger:  logger.With("module", "stream-handler"),
	}
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

	decision := stream.Decide(item, mediaStreams, "")
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

// MasterPlaylist returns the HLS master playlist (M3U8) with adaptive bitrate variants.
func (h *StreamHandler) MasterPlaylist(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")

	// Verify item exists
	if _, err := h.items.GetByID(r.Context(), itemID); err != nil {
		handleServiceError(w, r, err)
		return
	}

	profiles := []string{"1080p", "720p", "480p", "360p"}
	playlist := stream.GenerateMasterPlaylist(itemID, h.baseURL, profiles)

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = fmt.Fprint(w, playlist)
}

// QualityPlaylist returns the HLS playlist for a specific quality level.
// It starts a transcode session if one doesn't exist.
func (h *StreamHandler) QualityPlaylist(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	quality := chi.URLParam(r, "quality")

	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	startTime := parseFloat(r.URL.Query().Get("start"))

	ms, err := h.manager.StartSession(r.Context(), claims.UserID, itemID, quality, startTime)
	if err != nil {
		// The manager returns typed AppErrors (e.g. TranscodeBusy) that
		// handleServiceError renders without leaking internal messages.
		h.logger.Error("failed to start session", "error", err)
		handleServiceError(w, r, err)
		return
	}

	if ms.Decision.Method == stream.MethodDirectPlay {
		// Redirect to direct play
		http.Redirect(w, r, fmt.Sprintf("%s/api/v1/stream/%s/direct", h.baseURL, itemID), http.StatusTemporaryRedirect)
		return
	}

	// Wait for manifest to be generated (up to 10s)
	manifestPath := ms.ManifestPath()
	if err := waitForFile(manifestPath, 10*time.Second); err != nil {
		handleServiceError(w, r, domain.NewTranscodePending())
		return
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, manifestPath)
}

// Segment serves an HLS segment (.ts file) from a transcode session.
func (h *StreamHandler) Segment(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	quality := chi.URLParam(r, "quality")
	segmentFile := chi.URLParam(r, "segment")

	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	key := claims.UserID + ":" + itemID + ":" + quality
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

	// Wait for segment to appear (up to 30s, FFmpeg might still be encoding)
	if err := waitForFile(segmentPath, 30*time.Second); err != nil {
		respondError(w, r, http.StatusNotFound, "SEGMENT_NOT_FOUND", "segment not available")
		return
	}

	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "max-age=3600")
	http.ServeFile(w, r, segmentPath)
}

// DirectPlay serves the original media file via progressive download / range requests.
func (h *StreamHandler) DirectPlay(w http.ResponseWriter, r *http.Request) {
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

	quality := r.URL.Query().Get("quality")
	if quality == "" {
		quality = "720p"
	}

	key := claims.UserID + ":" + itemID + ":" + quality
	h.manager.StopSession(key)
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

// SubtitleTrack extracts and serves a subtitle track as WebVTT.
func (h *StreamHandler) SubtitleTrack(w http.ResponseWriter, r *http.Request) {
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
	w.Header().Set("Cache-Control", "max-age=86400")
	_, _ = io.Copy(w, vttData)
}
