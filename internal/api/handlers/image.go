package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"hubplay/internal/db"
	"hubplay/internal/provider"
)

type ImageHandler struct {
	images      *db.ImageRepository
	externalIDs *db.ExternalIDRepository
	items       *db.ItemRepository
	providers   *provider.Manager
	imageDir    string
	logger      *slog.Logger
}

func NewImageHandler(
	images *db.ImageRepository,
	externalIDs *db.ExternalIDRepository,
	items *db.ItemRepository,
	providers *provider.Manager,
	imageDir string,
	logger *slog.Logger,
) *ImageHandler {
	return &ImageHandler{
		images:      images,
		externalIDs: externalIDs,
		items:       items,
		providers:   providers,
		imageDir:    imageDir,
		logger:      logger.With("handler", "images"),
	}
}

// List returns all images stored for an item.
func (h *ImageHandler) List(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "id")

	images, err := h.images.ListByItem(r.Context(), itemID)
	if err != nil {
		handleServiceError(w, err)
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
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get external IDs")
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
		handleServiceError(w, err)
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
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to fetch images from providers")
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

	if !isValidImageType(imgType) {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid image type")
		return
	}

	var body struct {
		URL    string `json:"url"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
	if err := decodeJSON(r, &body); err != nil {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid request body")
		return
	}

	if body.URL == "" {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "url is required")
		return
	}

	// Download the image
	imgData, contentType, err := h.downloadImage(body.URL)
	if err != nil {
		h.logger.Error("failed to download image", "url", body.URL, "error", err)
		respondError(w, http.StatusBadGateway, "DOWNLOAD_FAILED", "failed to download image")
		return
	}

	// Save locally
	ext := extensionForContentType(contentType)
	hash := sha256.Sum256(imgData)
	filename := fmt.Sprintf("%s_%s%s", imgType, hex.EncodeToString(hash[:8]), ext)
	localPath, err := h.saveImageFile(itemID, filename, imgData)
	if err != nil {
		h.logger.Error("failed to save image", "error", err)
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to save image")
		return
	}

	// Create DB record
	imgID := uuid.NewString()
	img := &db.Image{
		ID:        imgID,
		ItemID:    itemID,
		Type:      imgType,
		Path:      "/api/v1/images/file/" + imgID,
		Width:     body.Width,
		Height:    body.Height,
		Provider:  "local",
		IsPrimary: false,
		AddedAt:   time.Now(),
	}

	if err := h.images.Create(r.Context(), img); err != nil {
		os.Remove(localPath)
		h.logger.Error("failed to create image record", "error", err)
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to save image record")
		return
	}

	// Set as primary
	if err := h.images.SetPrimary(r.Context(), itemID, imgType, imgID); err != nil {
		h.logger.Error("failed to set primary", "error", err)
	}
	img.IsPrimary = true

	// Store the local file path mapping
	h.writePathMapping(imgID, localPath)

	respondJSON(w, http.StatusOK, map[string]any{"data": imageResponse(img)})
}

// Upload handles multipart file upload for custom images.
func (h *ImageHandler) Upload(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "id")
	imgType := chi.URLParam(r, "type")

	if !isValidImageType(imgType) {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid image type")
		return
	}

	// 10 MB max
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "file too large (max 10MB)")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "file field is required")
		return
	}
	defer file.Close()

	// Validate content type
	contentType := header.Header.Get("Content-Type")
	if !isValidImageContentType(contentType) {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid file type (must be JPEG, PNG, or WebP)")
		return
	}

	imgData, err := io.ReadAll(file)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to read file")
		return
	}

	// Save locally
	ext := extensionForContentType(contentType)
	hash := sha256.Sum256(imgData)
	filename := fmt.Sprintf("%s_%s%s", imgType, hex.EncodeToString(hash[:8]), ext)
	localPath, err := h.saveImageFile(itemID, filename, imgData)
	if err != nil {
		h.logger.Error("failed to save uploaded image", "error", err)
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to save image")
		return
	}

	imgID := uuid.NewString()
	img := &db.Image{
		ID:        imgID,
		ItemID:    itemID,
		Type:      imgType,
		Path:      "/api/v1/images/file/" + imgID,
		Provider:  "upload",
		IsPrimary: false,
		AddedAt:   time.Now(),
	}

	if err := h.images.Create(r.Context(), img); err != nil {
		os.Remove(localPath)
		h.logger.Error("failed to create image record", "error", err)
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to save image record")
		return
	}

	// Set as primary
	if err := h.images.SetPrimary(r.Context(), itemID, imgType, imgID); err != nil {
		h.logger.Error("failed to set primary", "error", err)
	}
	img.IsPrimary = true

	h.writePathMapping(imgID, localPath)

	respondJSON(w, http.StatusOK, map[string]any{"data": imageResponse(img)})
}

// SetPrimary sets an existing image as the primary for its type.
func (h *ImageHandler) SetPrimary(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "id")
	imageID := chi.URLParam(r, "imageId")

	img, err := h.images.GetByID(r.Context(), imageID)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	if img.ItemID != itemID {
		respondError(w, http.StatusNotFound, "NOT_FOUND", "image not found for this item")
		return
	}

	if err := h.images.SetPrimary(r.Context(), itemID, img.Type, imageID); err != nil {
		h.logger.Error("failed to set primary", "error", err)
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to set primary image")
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
		handleServiceError(w, err)
		return
	}

	if img.ItemID != itemID {
		respondError(w, http.StatusNotFound, "NOT_FOUND", "image not found for this item")
		return
	}

	// Delete local file if it exists
	if localPath := h.readPathMapping(imageID); localPath != "" {
		os.Remove(localPath)
		h.removePathMapping(imageID)
	}

	if err := h.images.DeleteByID(r.Context(), imageID); err != nil {
		h.logger.Error("failed to delete image", "error", err)
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to delete image")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ServeFile serves a locally stored image by its ID.
func (h *ImageHandler) ServeFile(w http.ResponseWriter, r *http.Request) {
	imageID := chi.URLParam(r, "id")

	localPath := h.readPathMapping(imageID)
	if localPath == "" {
		// Fallback: try to get the image from DB and check if path is a remote URL
		img, err := h.images.GetByID(r.Context(), imageID)
		if err != nil {
			respondError(w, http.StatusNotFound, "NOT_FOUND", "image not found")
			return
		}
		// If path starts with http, redirect to the remote URL
		if strings.HasPrefix(img.Path, "http") {
			http.Redirect(w, r, img.Path, http.StatusTemporaryRedirect)
			return
		}
		respondError(w, http.StatusNotFound, "NOT_FOUND", "image file not found")
		return
	}

	http.ServeFile(w, r, localPath)
}

// ── Helpers ──

func (h *ImageHandler) downloadImage(url string) ([]byte, string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url) //nolint:gosec
	if err != nil {
		return nil, "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20)) // 20MB max
	if err != nil {
		return nil, "", fmt.Errorf("read body: %w", err)
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = http.DetectContentType(data)
	}

	return data, ct, nil
}

func (h *ImageHandler) saveImageFile(itemID, filename string, data []byte) (string, error) {
	dir := filepath.Join(h.imageDir, itemID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create dir: %w", err)
	}

	fullPath := filepath.Join(dir, filename)
	if err := os.WriteFile(fullPath, data, 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	return fullPath, nil
}

// Path mapping: store imageID -> local file path in a simple file.
func (h *ImageHandler) writePathMapping(imageID, localPath string) {
	dir := filepath.Join(h.imageDir, ".mappings")
	os.MkdirAll(dir, 0o755)             //nolint:errcheck
	os.WriteFile(filepath.Join(dir, imageID), []byte(localPath), 0o644) //nolint:errcheck
}

func (h *ImageHandler) readPathMapping(imageID string) string {
	data, err := os.ReadFile(filepath.Join(h.imageDir, ".mappings", imageID))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (h *ImageHandler) removePathMapping(imageID string) {
	os.Remove(filepath.Join(h.imageDir, ".mappings", imageID)) //nolint:errcheck
}

func isValidImageType(t string) bool {
	switch t {
	case "primary", "backdrop", "logo", "thumb", "banner":
		return true
	}
	return false
}

func isValidImageContentType(ct string) bool {
	switch {
	case strings.HasPrefix(ct, "image/jpeg"),
		strings.HasPrefix(ct, "image/png"),
		strings.HasPrefix(ct, "image/webp"):
		return true
	}
	return false
}

func extensionForContentType(ct string) string {
	switch {
	case strings.Contains(ct, "png"):
		return ".png"
	case strings.Contains(ct, "webp"):
		return ".webp"
	default:
		return ".jpg"
	}
}
