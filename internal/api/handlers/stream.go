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
// `externalIDs` and `providers` are optional — when nil, the external
// subtitle endpoints return 503 instead of 500. Older test envs and
// installs without OpenSubtitles configured keep working without
// rewiring.
//
// `settings` is the runtime override layer (app_settings); when nil the
// handler falls back to the boot-time `baseURL` exclusively, which is
// what tests rely on. `baseURL` itself is the YAML / env default that
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
	decision := stream.Decide(item, mediaStreams, caps, "")
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
	playlist := stream.GenerateMasterPlaylist(itemID, h.effectiveBaseURL(r.Context()), profiles)

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = fmt.Fprint(w, playlist)
}

// QualityPlaylist returns the HLS playlist for a specific quality level.
// It starts a transcode session if one doesn't exist.
// segmentDurationSeconds is the value the transcoder hands to ffmpeg
// via -hls_time and the value the synthesized manifest declares per
// segment. Keep them in lockstep — if one moves, the other has to.
const segmentDurationSeconds float64 = 6

func (h *StreamHandler) QualityPlaylist(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemId")
	quality := chi.URLParam(r, "quality")

	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	startTime := parseFloat(r.URL.Query().Get("start"))
	// Client capabilities (codecs/containers) come in via the
	// X-Hubplay-Client-Capabilities header. nil means "no header sent",
	// in which case the stream manager + Decide() fall back to the
	// conservative web-browser defaults — no behaviour change for
	// today's web client which doesn't send the header yet.
	caps := stream.CapabilitiesFromRequest(r)

	ms, err := h.manager.StartSession(r.Context(), claims.UserID, itemID, quality, caps, startTime)
	if err != nil {
		// The manager returns typed AppErrors (e.g. TranscodeBusy) that
		// handleServiceError renders without leaking internal messages.
		h.logger.Error("failed to start session", "error", err)
		handleServiceError(w, r, err)
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
	// do without knowing how long the stream is.
	item, err := h.items.GetByID(r.Context(), itemID)
	if err == nil && item != nil && item.DurationTicks > 0 {
		duration := float64(item.DurationTicks) / 10_000_000
		segmentTpl := fmt.Sprintf("/api/v1/stream/%s/%s/segment%%05d.ts", itemID, quality)
		manifest := stream.SynthesizeVODManifest(duration, segmentDurationSeconds, segmentTpl)

		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-cache")
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
	w.Header().Set("Cache-Control", "no-cache")
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
		// Rate-limit hits are visibility-only: the client most
		// likely has a seek-loop bug and the right behaviour is to
		// stop encouraging it. 429 with Retry-After lets a healthy
		// client back off and resume cleanly; a buggy one keeps
		// retrying but at least no longer melts the transcoder.
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

// serveSegment writes the segment file with the right HLS headers.
// Pulled out so the fast and seek-restart paths can both use it
// without duplicating the headers.
func serveSegment(w http.ResponseWriter, r *http.Request, path string) {
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "max-age=3600")
	http.ServeFile(w, r, path)
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
	w.Header().Set("Cache-Control", "public, max-age=86400, stale-while-revalidate=604800")
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
