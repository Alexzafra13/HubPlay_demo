package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/imaging"
	"hubplay/internal/imaging/pathmap"
	"hubplay/internal/provider"
)

type ImageHandler struct {
	images      ImageRepository
	externalIDs ExternalIDRepository
	items       ItemRepository
	providers   ProviderManager
	refresher   ImageRefreshService
	imageDir    string
	pathmap     *pathmap.Store
	logger      *slog.Logger
}

func NewImageHandler(
	images ImageRepository,
	externalIDs ExternalIDRepository,
	items ItemRepository,
	providers ProviderManager,
	refresher ImageRefreshService,
	imageDir string,
	logger *slog.Logger,
) *ImageHandler {
	return &ImageHandler{
		images:      images,
		externalIDs: externalIDs,
		items:       items,
		providers:   providers,
		refresher:   refresher,
		imageDir:    imageDir,
		pathmap:     pathmap.New(imageDir),
		logger:      logger.With("handler", "images"),
	}
}

// List returns all images stored for an item.
func (h *ImageHandler) List(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "id")

	images, err := h.images.ListByItem(r.Context(), itemID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	data := make([]map[string]any, len(images))
	for i, img := range images {
		data[i] = imageResponse(img)
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": data})
}

// Available queries all registered image providers for available images.
func (h *ImageHandler) Available(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "id")

	// Get external IDs for this item
	extIDs, err := h.externalIDs.ListByItem(r.Context(), itemID)
	if err != nil {
		h.logger.Error("failed to get external IDs", "item", itemID, "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get external IDs")
		return
	}

	if len(extIDs) == 0 {
		respondJSON(w, http.StatusOK, map[string]any{"data": []any{}})
		return
	}

	// Build external ID map
	idMap := make(map[string]string, len(extIDs))
	for _, e := range extIDs {
		idMap[e.Provider] = e.ExternalID
	}

	// Determine item type
	item, err := h.items.GetByID(r.Context(), itemID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	itemType := provider.ItemMovie
	switch item.Type {
	case "series":
		itemType = provider.ItemSeries
	case "season":
		itemType = provider.ItemSeason
	case "episode":
		itemType = provider.ItemEpisode
	}

	// Fetch available images from providers
	results, err := h.providers.FetchImages(r.Context(), idMap, itemType)
	if err != nil {
		h.logger.Error("failed to fetch images from providers", "item", itemID, "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to fetch images from providers")
		return
	}

	// Filter by type if query param provided
	filterType := r.URL.Query().Get("type")

	data := make([]map[string]any, 0, len(results))
	for _, img := range results {
		if filterType != "" && img.Type != filterType {
			continue
		}
		data = append(data, map[string]any{
			"url":      img.URL,
			"type":     img.Type,
			"language": img.Language,
			"width":    img.Width,
			"height":   img.Height,
			"score":    img.Score,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": data})
}

// Select downloads an image from a URL and saves it locally, setting it as primary.
func (h *ImageHandler) Select(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "id")
	imgType := chi.URLParam(r, "type")

	if !imaging.IsSafePathSegment(itemID) {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "invalid item id")
		return
	}
	if !imaging.IsValidKind(imgType) {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "invalid image type")
		return
	}

	var body struct {
		URL    string `json:"url"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
	if err := decodeJSON(r, &body); err != nil {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "invalid request body")
		return
	}

	if body.URL == "" {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "url is required")
		return
	}

	// Download the image through the SSRF-safe client (blocks loopback/private
	// addresses, non-http(s) schemes, oversized bodies).
	imgData, contentType, err := imaging.SafeGet(body.URL, imaging.MaxUploadBytes, 30*time.Second)
	if err != nil {
		h.logger.Error("failed to download image", "url", body.URL, "error", err)
		respondError(w, r, http.StatusBadGateway, "DOWNLOAD_FAILED", "failed to download image")
		return
	}
	if err := imaging.EnforceMaxPixels(imgData); err != nil {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "image dimensions too large")
		return
	}

	img, err := h.persistManualImage(r, itemID, imgType, imgData, contentType, "local", body.Width, body.Height)
	if err != nil {
		h.logger.Error("failed to persist selected image", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to save image")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": imageResponse(img)})
}

// Upload handles multipart file upload for custom images.
//
// Security:
//   - itemID is validated as a safe path segment (no traversal/separators).
//   - The real MIME type is sniffed from the body bytes; the multipart
//     Content-Type header is ignored for validation (clients can spoof it).
//   - Image dimensions are bounded via imaging.EnforceMaxPixels to block
//     decompression bombs before the blurhash/resize stages.
func (h *ImageHandler) Upload(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "id")
	imgType := chi.URLParam(r, "type")

	if !imaging.IsSafePathSegment(itemID) {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "invalid item id")
		return
	}
	if !imaging.IsValidKind(imgType) {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "invalid image type")
		return
	}

	if err := r.ParseMultipartForm(imaging.MaxUploadBytes); err != nil {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "file too large (max 10MB)")
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "file field is required")
		return
	}
	defer file.Close() //nolint:errcheck

	// Cap the total read at MaxUploadBytes — ParseMultipartForm already limits
	// the on-disk spill but the in-memory copy is unbounded by default.
	imgData, err := io.ReadAll(io.LimitReader(file, imaging.MaxUploadBytes+1))
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to read file")
		return
	}
	if int64(len(imgData)) > imaging.MaxUploadBytes {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "file too large (max 10MB)")
		return
	}

	// Sniff the real content type from the bytes, never trust the client header.
	sniffed, _, _ := imaging.SniffContentType(bytes.NewReader(imgData))
	if !imaging.IsValidContentType(sniffed) {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "invalid file type (must be JPEG, PNG, or WebP)")
		return
	}

	// Reject oversized dimensions (decompression-bomb guard).
	if err := imaging.EnforceMaxPixels(imgData); err != nil {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "image dimensions too large")
		return
	}

	// Width/height are 0 — the imaging pipeline doesn't need them and the
	// upload endpoint doesn't ask the client for dimensions (the file
	// IS the source of truth, decoding it would be redundant work).
	img, err := h.persistManualImage(r, itemID, imgType, imgData, sniffed, "upload", 0, 0)
	if err != nil {
		h.logger.Error("failed to persist uploaded image", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to save image")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": imageResponse(img)})
}

// SetLocked toggles the manual-override lock on an image. The flag is
// honoured by the ImageRefresher (skips kinds with any locked image)
// so admins can pin curated artwork without the next refresh
// silently overwriting it. Body shape: `{"locked": true|false}`.
func (h *ImageHandler) SetLocked(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "id")
	imageID := chi.URLParam(r, "imageId")

	img, err := h.images.GetByID(r.Context(), imageID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if img.ItemID != itemID {
		respondAppError(w, r.Context(), domain.NewNotFound("image"))
		return
	}

	var body struct {
		Locked bool `json:"locked"`
	}
	if err := decodeJSON(r, &body); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}

	if err := h.images.SetLocked(r.Context(), imageID, body.Locked); err != nil {
		h.logger.Error("failed to set lock", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to set lock")
		return
	}

	img.IsLocked = body.Locked
	respondJSON(w, http.StatusOK, map[string]any{"data": imageResponse(img)})
}

// SetPrimary sets an existing image as the primary for its type.
func (h *ImageHandler) SetPrimary(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "id")
	imageID := chi.URLParam(r, "imageId")

	img, err := h.images.GetByID(r.Context(), imageID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	if img.ItemID != itemID {
		respondAppError(w, r.Context(), domain.NewNotFound("image"))
		return
	}

	if err := h.images.SetPrimary(r.Context(), itemID, img.Type, imageID); err != nil {
		h.logger.Error("failed to set primary", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to set primary image")
		return
	}

	img.IsPrimary = true
	respondJSON(w, http.StatusOK, map[string]any{"data": imageResponse(img)})
}

// Delete removes an image record and its local file.
func (h *ImageHandler) Delete(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "id")
	imageID := chi.URLParam(r, "imageId")

	img, err := h.images.GetByID(r.Context(), imageID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	if img.ItemID != itemID {
		respondAppError(w, r.Context(), domain.NewNotFound("image"))
		return
	}

	// Delete local file if it exists, plus any cached thumbnails the
	// width-resizer generated on demand (`<imageDir>/.thumbnails/<id>_wN.<ext>`).
	// Without this the resizer leaks ~1-N files per resolution that were
	// asked for and never cleaned — bounded growth in practice but real
	// disk waste on long-lived installs that delete & re-upload artwork.
	if localPath := h.readPathMapping(imageID); localPath != "" {
		_ = os.Remove(localPath)
		h.removePathMapping(imageID)
	}
	thumbPattern := filepath.Join(h.imageDir, ".thumbnails", imageID+"_w*")
	if matches, err := filepath.Glob(thumbPattern); err == nil {
		for _, m := range matches {
			if h.isUnderImageDir(m) {
				_ = os.Remove(m)
			}
		}
	}

	if err := h.images.DeleteByID(r.Context(), imageID); err != nil {
		h.logger.Error("failed to delete image", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to delete image")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RefreshLibraryImages delegates to the library.ImageRefresher service; the
// loop that used to live here (provider fetch, best-by-kind selection,
// download, save, persist) is now a single service call so this handler
// stays focused on HTTP-shaped concerns.
func (h *ImageHandler) RefreshLibraryImages(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")

	updated, err := h.refresher.RefreshForLibrary(r.Context(), libraryID)
	if err != nil {
		h.logger.Error("image refresh failed", "library", libraryID, "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to refresh images")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"updated": updated}})
}

// ServeFile serves a locally stored image by its ID.
// Supports an optional "w" query parameter for thumbnail generation (e.g. ?w=300).
//
// Security:
//   - readPathMapping enforces UUID-shaped imageIDs at the pathmap layer.
//   - Before passing any path to http.ServeFile we verify that it resolves
//     inside h.imageDir. Defense in depth against a poisoned mapping file.
func (h *ImageHandler) ServeFile(w http.ResponseWriter, r *http.Request) {
	imageID := chi.URLParam(r, "id")

	localPath := h.readPathMapping(imageID)
	if localPath == "" {
		// No path-mapping entry → no on-disk file. Every image since
		// the scanner-downloads-artwork commit lives at a local
		// path; an empty pathmap result means the caller asked for
		// something that doesn't exist (or never finished writing).
		respondError(w, r, http.StatusNotFound, "NOT_FOUND", "image file not found")
		return
	}

	if !h.isUnderImageDir(localPath) {
		h.logger.Warn("pathmap points outside imageDir — ignoring", "id", imageID, "path", localPath)
		respondError(w, r, http.StatusNotFound, "NOT_FOUND", "image file not found")
		return
	}

	// Images are content-addressed by ID and rarely change.
	// Cache aggressively with stale-while-revalidate for seamless background refresh.
	w.Header().Set("Cache-Control", "public, max-age=86400, stale-while-revalidate=604800")

	// Check for thumbnail width request.
	if wStr := r.URL.Query().Get("w"); wStr != "" {
		maxWidth, err := strconv.Atoi(wStr)
		if err == nil && maxWidth > 0 && maxWidth < 4096 {
			thumbDir := filepath.Join(h.imageDir, ".thumbnails")
			thumbPath := filepath.Join(thumbDir, fmt.Sprintf("%s_w%d%s", imageID, maxWidth, filepath.Ext(localPath)))
			if !h.isUnderImageDir(thumbPath) {
				// imageID was UUID-valid so this should not happen, but be safe.
				h.logger.Warn("thumbnail path escaped imageDir", "id", imageID)
				http.ServeFile(w, r, localPath)
				return
			}
			// Serve cached thumbnail if it exists.
			if _, err := os.Stat(thumbPath); err != nil {
				// Generate the thumbnail.
				if genErr := imaging.GenerateThumbnail(localPath, thumbPath, maxWidth); genErr != nil {
					h.logger.Warn("failed to generate thumbnail, serving original", "error", genErr)
					http.ServeFile(w, r, localPath)
					return
				}
			}
			http.ServeFile(w, r, thumbPath)
			return
		}
	}

	http.ServeFile(w, r, localPath)
}

// isUnderImageDir reports whether p, after cleaning, has h.imageDir as an
// ancestor. Compares absolute paths so relative/absolute mismatches don't
// fool the check.
func (h *ImageHandler) isUnderImageDir(p string) bool {
	rootAbs, err := filepath.Abs(h.imageDir)
	if err != nil {
		return false
	}
	pAbs, err := filepath.Abs(filepath.Clean(p))
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, pAbs)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..") && rel != ".."
}

// ── Helpers ──

func (h *ImageHandler) saveImageFile(itemID, filename string, data []byte) (string, error) {
	dir := filepath.Join(h.imageDir, itemID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create dir: %w", err)
	}

	fullPath := filepath.Join(dir, filename)
	// Atomic write — if the request is interrupted mid-flight (server
	// crash, disk-full), the destination is either absent or fully
	// written. Without this, an aborted upload could leave a truncated
	// JPEG that ServeFile would happily hand back to the next caller.
	if err := imaging.AtomicWriteFile(fullPath, data, 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	return fullPath, nil
}

// persistManualImage is the shared tail of `Select` and `Upload`: once
// the bytes have been validated (size + content-type + dimensions),
// both flows do the exact same nine steps to put the image on disk
// and into the DB. This helper owns those steps so the two callers
// can't drift.
//
// Steps:
//   1. Compose the on-disk filename from {kind}_{8-byte sha256}{ext}.
//   2. Atomic write to {imageDir}/{itemID}/{filename}.
//   3. Compute blurhash.
//   4. Compute dominant colour pair.
//   5. Insert the DB row with IsLocked = true (manual selection).
//   6. If insert fails, remove the file (rollback).
//   7. Promote the row to primary for its kind.
//   8. Write the pathmap entry so /images/file/<id> can serve it.
//   9. Return the populated `*db.Image` so the caller can build the
//      JSON response.
//
// The (width, height) pair is optional (0 = unknown) — Select gets it
// from the request body, Upload leaves it unset and the imaging
// pipeline (blurhash / colour extract) doesn't need it.
func (h *ImageHandler) persistManualImage(
	r *http.Request,
	itemID, kind string,
	data []byte,
	contentType, providerTag string,
	width, height int,
) (*db.Image, error) {
	ext := imaging.ExtensionForContentType(contentType)
	hash := sha256.Sum256(data)
	filename := fmt.Sprintf("%s_%s%s", kind, hex.EncodeToString(hash[:8]), ext)

	localPath, err := h.saveImageFile(itemID, filename, data)
	if err != nil {
		return nil, fmt.Errorf("save file: %w", err)
	}

	bhash := imaging.ComputeBlurhash(data, h.logger)
	vibrant, muted := imaging.ExtractDominantColors(data, h.logger)

	imgID := uuid.NewString()
	img := &db.Image{
		ID:        imgID,
		ItemID:    itemID,
		Type:      kind,
		Path:      "/api/v1/images/file/" + imgID,
		Width:     width,
		Height:    height,
		Blurhash:  bhash,
		Provider:  providerTag,
		IsPrimary: false,
		// Manual pick (Select from candidates / Upload from disk):
		// the admin's choice is authoritative, so future refreshes
		// must skip this kind until the admin explicitly unlocks.
		// Plex/Jellyfin both auto-lock on any manual selection for
		// the same reason.
		IsLocked:           true,
		AddedAt:            time.Now(),
		DominantColor:      vibrant,
		DominantColorMuted: muted,
	}

	if err := h.images.Create(r.Context(), img); err != nil {
		// Rollback the on-disk artefact so we don't leave an orphan
		// file the admin can't see in the UI.
		_ = os.Remove(localPath)
		return nil, fmt.Errorf("create image record: %w", err)
	}

	if err := h.images.SetPrimary(r.Context(), itemID, kind, imgID); err != nil {
		// SetPrimary failure is logged but not fatal: the row exists
		// and the admin can re-promote manually. Returning an error
		// here would force a rollback of an already-valid DB record.
		h.logger.Error("failed to set primary", "image_id", imgID, "error", err)
	} else {
		img.IsPrimary = true
	}

	h.writePathMapping(imgID, localPath)
	return img, nil
}

// writePathMapping logs at WARN on failure — the DB record is authoritative,
// so a missing mapping only costs a fallback DB lookup on serve.
func (h *ImageHandler) writePathMapping(imageID, localPath string) {
	if err := h.pathmap.Write(imageID, localPath); err != nil {
		h.logger.Warn("pathmap write failed", "id", imageID, "error", err)
	}
}

// readPathMapping returns the mapped path or "" when the mapping is missing
// / invalid / unreadable. Callers fall back to the DB record in that case.
func (h *ImageHandler) readPathMapping(imageID string) string {
	p, err := h.pathmap.Read(imageID)
	if err != nil {
		return ""
	}
	return p
}

func (h *ImageHandler) removePathMapping(imageID string) {
	if err := h.pathmap.Remove(imageID); err != nil {
		h.logger.Warn("pathmap remove failed", "id", imageID, "error", err)
	}
}

