package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	netUrl "net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/imaging"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/provider"
)

// CollectionRepository is the slice of db.CollectionRepository the
// handler needs.
type CollectionRepository interface {
	GetByID(ctx context.Context, id string) (*librarymodel.Collection, error)
	List(ctx context.Context) ([]*librarymodel.CollectionListEntry, error)
	ListItemsForCollection(ctx context.Context, collectionID string) ([]*librarymodel.CollectionItem, error)
}

// CollectionImageProvider expone el lookup de imágenes alternativas en
// el provider de metadatos (hoy TMDb). El handler lo usa en
// /collections/{id}/images/{type}/available para enseñar al admin
// todas las opciones que TMDb tiene de la saga, mismo flujo que
// Jellyfin con "Browse images".
type CollectionImageProvider interface {
	FetchCollectionImages(ctx context.Context, tmdbCollectionID string) ([]provider.ImageResult, error)
}

// CollectionImageOverrideRepo es el subset que el handler usa para
// gestionar overrides de carátula/fondo de las colecciones.
type CollectionImageOverrideRepo interface {
	UpsertURL(ctx context.Context, collectionID, imageType, url string) error
	UpsertFile(ctx context.Context, collectionID, imageType, basename string) error
	Delete(ctx context.Context, collectionID, imageType string) error
	Get(ctx context.Context, collectionID, imageType string) (*librarymodel.CollectionImageOverride, error)
	ListByCollection(ctx context.Context, collectionID string) ([]librarymodel.CollectionImageOverride, error)
}

// CollectionHandler serves /collections (browse) and /collections/{id}
// (detail). Powers the Jellyfin-style "Movie Collections" surface
// where saga members (X-Men, MCU, Toy Story) cluster under one page.
type CollectionHandler struct {
	collections CollectionRepository
	// overrides es opcional — nil-safe deja la página detail con las
	// imágenes TMDb originales y los endpoints de edit devuelven 503.
	overrides CollectionImageOverrideRepo
	// images consulta TMDb para listar pósters/backdrops alternativos.
	// Opcional: sin esto el endpoint /available devuelve 503 y el
	// admin sólo puede pegar URL o subir archivo (sin browse).
	images CollectionImageProvider
	// imageDir es donde se guardan los archivos subidos. Vacío
	// deshabilita el upload (las URLs sí funcionan sin esto).
	imageDir string
	audit    AuditEmitter
	logger   *slog.Logger
}

func NewCollectionHandler(collections CollectionRepository, overrides CollectionImageOverrideRepo, images CollectionImageProvider, imageDir string, audit AuditEmitter, logger *slog.Logger) *CollectionHandler {
	return &CollectionHandler{
		collections: collections,
		overrides:   overrides,
		images:      images,
		audit:       audit,
		imageDir:    imageDir,
		logger:      logger,
	}
}

func (h *CollectionHandler) auditEmit() AuditEmitter {
	if h.audit != nil {
		return h.audit
	}
	return noopAudit{}
}

const collectionImagesSubdir = "collection-images"

// List returns every collection with at least one member movie in the
// catalogue, sorted by member count desc.
//
//	GET /api/v1/collections
//	{ "data": { "collections": [ {id,name,poster_url,backdrop_url,item_count}, ... ] } }
func (h *CollectionHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.collections.List(r.Context())
	if err != nil {
		h.logger.Error("list collections", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "list collections failed")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		entry := map[string]any{
			"id":         row.ID,
			"name":       row.Name,
			"item_count": row.ItemCount,
		}
		if row.PosterURL != "" {
			entry["poster_url"] = row.PosterURL
		}
		if row.BackdropURL != "" {
			entry["backdrop_url"] = row.BackdropURL
		}
		out = append(out, entry)
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{"collections": out},
	})
}

// Get returns a collection's metadata + member movies in release
// order. 404 when the id doesn't match — the handler accepts the
// stable "collection:<tmdb_id>" id directly so the frontend never
// has to slug-encode it.
//
//	GET /api/v1/collections/{id}
//	{ "data": {
//	    "id": "collection:86311", "tmdb_id": 86311,
//	    "name": "Marvel Cinematic Universe",
//	    "overview": "...", "poster_url": "...", "backdrop_url": "...",
//	    "items": [ {id,type,title,year,poster_url}, ... ]
//	} }
func (h *CollectionHandler) Get(w http.ResponseWriter, r *http.Request) {
	// chi v5 returns URL parameters in their raw, percent-encoded form
	// (it matches against r.URL.RawPath when set). Collection IDs are
	// "collection:<tmdb_id>" so the frontend's encodeURIComponent
	// turns the colon into "%3A" before navigation, and that escaped
	// form is what lands here. Decode it before the DB lookup or the
	// query searches for the literal "%3A" string and 404s every saga
	// the home rail just listed. PathUnescape returning an error is
	// theoretically impossible for a value that already came out of a
	// validly-routed request, but we fall back to the raw value so a
	// future malformed input surfaces as 404 from the lookup rather
	// than crashing the handler.
	rawID := chi.URLParam(r, "id")
	id := rawID
	if decoded, err := netUrl.PathUnescape(rawID); err == nil {
		id = decoded
	}
	col, err := h.collections.GetByID(r.Context(), id)
	if err != nil {
		h.logger.Error("get collection", "id", id, "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "lookup failed")
		return
	}
	if col == nil {
		respondError(w, r, http.StatusNotFound, "NOT_FOUND", "collection not found")
		return
	}
	items, err := h.collections.ListItemsForCollection(r.Context(), col.ID)
	if err != nil {
		h.logger.Error("list collection items", "id", id, "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "list items failed")
		return
	}

	resp := map[string]any{
		"id":      col.ID,
		"tmdb_id": col.TMDBID,
		"name":    col.Name,
	}
	if col.Overview != "" {
		resp["overview"] = col.Overview
	}

	// Overlay de imágenes — si el admin pegó URL o subió archivo,
	// gana sobre la de TMDb. Una llamada al repo, dos rows como
	// mucho. Si el archivo está subido, devolvemos un endpoint local
	// que sirve el binario directamente. Si es URL externa, va tal
	// cual y el browser la fetchea (TMDb URLs ya las usamos así, el
	// CSP del proyecto las permite vía img-src).
	overrideByType := map[string]librarymodel.CollectionImageOverride{}
	if h.overrides != nil {
		rows, oErr := h.overrides.ListByCollection(r.Context(), col.ID)
		if oErr == nil {
			for _, o := range rows {
				overrideByType[o.ImageType] = o
			}
		}
	}
	if ov, has := overrideByType["poster"]; has {
		resp["poster_url"] = resolveOverrideURL(ov, col.ID, "poster")
	} else if col.PosterURL != "" {
		resp["poster_url"] = col.PosterURL
	}
	if ov, has := overrideByType["backdrop"]; has {
		resp["backdrop_url"] = resolveOverrideURL(ov, col.ID, "backdrop")
	} else if col.BackdropURL != "" {
		resp["backdrop_url"] = col.BackdropURL
	}

	entries := make([]map[string]any, 0, len(items))
	for _, it := range items {
		entry := map[string]any{
			"id":    it.ID,
			"type":  it.Type,
			"title": it.Title,
		}
		if it.Year > 0 {
			entry["year"] = it.Year
		}
		if it.PrimaryImageID != "" {
			entry["poster_url"] = "/api/v1/images/file/" + it.PrimaryImageID
		}
		entries = append(entries, entry)
	}
	resp["items"] = entries

	respondData(w, http.StatusOK, resp)
}

// resolveOverrideURL produce la URL absoluta que el frontend pondrá en
// el <img> según el tipo de override. Para overrides de archivo
// devolvemos un endpoint local que sirve el binario; para URL externa
// la devolvemos tal cual (TMDb / CDN del admin).
func resolveOverrideURL(ov librarymodel.CollectionImageOverride, collectionID, imageType string) string {
	if ov.File != "" {
		return fmt.Sprintf("/api/v1/collections/%s/images/%s/file?v=%d",
			netUrl.PathEscape(collectionID), imageType, ov.UpdatedAt.UnixNano())
	}
	return ov.URL
}

type setCollectionImageRequest struct {
	URL string `json:"url"`
}

// SetCollectionImage registra un override de URL externa para el poster
// o el backdrop de una colección. {type} = "poster" | "backdrop".
//
// PUT /collections/{id}/images/{type}     body: {url}
// Admin-only.
func (h *CollectionHandler) SetCollectionImage(w http.ResponseWriter, r *http.Request) {
	collectionID, imageType, ok := h.parseCollectionImageRoute(w, r)
	if !ok {
		return
	}
	if h.overrides == nil {
		respondError(w, r, http.StatusServiceUnavailable, "NO_OVERRIDES", "collection image overrides not configured")
		return
	}

	var req setCollectionImageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		respondError(w, r, http.StatusBadRequest, "MISSING_URL", "url required (use DELETE to clear)")
		return
	}
	if !isHTTPURL(req.URL) {
		respondError(w, r, http.StatusBadRequest, "INVALID_URL", "url must be http(s)")
		return
	}

	prev, _ := h.overrides.Get(r.Context(), collectionID, imageType)
	if err := h.overrides.UpsertURL(r.Context(), collectionID, imageType, req.URL); err != nil {
		h.logger.Error("upsert collection image override", "id", collectionID, "type", imageType, "error", err)
		respondError(w, r, http.StatusInternalServerError, "STORAGE_ERROR", "could not save override")
		return
	}
	if prev != nil && prev.File != "" {
		h.deleteCollectionImageFile(prev.File)
	}
	h.auditEmit().LogArtworkChanged(r.Context(), r, "collection", collectionID, imageType)

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"collection_id": collectionID,
			"image_type":    imageType,
			"url":           req.URL,
		},
	})
}

// UploadCollectionImage guarda un archivo subido como override. Mismo
// patrón que el upload de logos de canal: validamos size, sniff MIME,
// decompression-bomb guard.
//
// POST /collections/{id}/images/{type}/upload    multipart: file=...
// Admin-only.
func (h *CollectionHandler) UploadCollectionImage(w http.ResponseWriter, r *http.Request) {
	collectionID, imageType, ok := h.parseCollectionImageRoute(w, r)
	if !ok {
		return
	}
	if h.overrides == nil {
		respondError(w, r, http.StatusServiceUnavailable, "NO_OVERRIDES", "collection image overrides not configured")
		return
	}
	if h.imageDir == "" {
		respondError(w, r, http.StatusServiceUnavailable, "NO_STORAGE", "image storage not configured")
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

	imgData, err := io.ReadAll(io.LimitReader(file, imaging.MaxUploadBytes+1))
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to read file")
		return
	}
	if int64(len(imgData)) > imaging.MaxUploadBytes {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "file too large (max 10MB)")
		return
	}
	sniffed, _, _ := imaging.SniffContentType(bytes.NewReader(imgData))
	if !imaging.IsValidContentType(sniffed) {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "invalid file type (must be JPEG, PNG, or WebP)")
		return
	}
	if err := imaging.EnforceMaxPixels(imgData); err != nil {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "image dimensions too large")
		return
	}

	// Basename = "<collection_id_safe>-<type>-<unix_nano>.<ext>". El
	// collection_id contiene ":" (formato "collection:550"); lo
	// sustituimos por "_" para que el filename sea seguro en todos
	// los filesystems.
	safeID := strings.ReplaceAll(collectionID, ":", "_")
	basename := fmt.Sprintf("%s-%s-%d%s", safeID, imageType, time.Now().UnixNano(), extensionForContentType(sniffed))

	dir := filepath.Join(h.imageDir, collectionImagesSubdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		h.logger.Error("create collection-images dir", "dir", dir, "error", err)
		respondError(w, r, http.StatusInternalServerError, "STORAGE_ERROR", "could not create storage dir")
		return
	}
	dest := filepath.Join(dir, basename)
	if err := os.WriteFile(dest, imgData, 0o644); err != nil {
		h.logger.Error("write collection-image", "dest", dest, "error", err)
		respondError(w, r, http.StatusInternalServerError, "STORAGE_ERROR", "could not save image")
		return
	}

	prev, _ := h.overrides.Get(r.Context(), collectionID, imageType)
	if err := h.overrides.UpsertFile(r.Context(), collectionID, imageType, basename); err != nil {
		_ = os.Remove(dest)
		respondError(w, r, http.StatusInternalServerError, "STORAGE_ERROR", "could not save override")
		return
	}
	if prev != nil && prev.File != "" && prev.File != basename {
		h.deleteCollectionImageFile(prev.File)
	}
	h.auditEmit().LogArtworkChanged(r.Context(), r, "collection", collectionID, imageType)

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"collection_id": collectionID,
			"image_type":    imageType,
			"file":          basename,
		},
	})
}

// ClearCollectionImage borra el override del tipo dado. Idempotente.
//
// DELETE /collections/{id}/images/{type}
// Admin-only.
func (h *CollectionHandler) ClearCollectionImage(w http.ResponseWriter, r *http.Request) {
	collectionID, imageType, ok := h.parseCollectionImageRoute(w, r)
	if !ok {
		return
	}
	if h.overrides == nil {
		respondError(w, r, http.StatusServiceUnavailable, "NO_OVERRIDES", "collection image overrides not configured")
		return
	}
	prev, _ := h.overrides.Get(r.Context(), collectionID, imageType)
	if err := h.overrides.Delete(r.Context(), collectionID, imageType); err != nil {
		respondError(w, r, http.StatusInternalServerError, "STORAGE_ERROR", "could not clear override")
		return
	}
	if prev != nil && prev.File != "" {
		h.deleteCollectionImageFile(prev.File)
	}
	h.auditEmit().LogArtworkChanged(r.Context(), r, "collection", collectionID, imageType+"_cleared")
	w.WriteHeader(http.StatusNoContent)
}

// ServeCollectionImage sirve el archivo de un override de archivo,
// directamente desde imageDir. Cualquier usuario autenticado puede
// leerlo (mismo modelo que /api/v1/images/file/{id}). Validamos el
// basename como path segment seguro defensivamente.
//
// GET /collections/{id}/images/{type}/file
func (h *CollectionHandler) ServeCollectionImage(w http.ResponseWriter, r *http.Request) {
	collectionID, imageType, ok := h.parseCollectionImageRoute(w, r)
	if !ok {
		return
	}
	if h.overrides == nil || h.imageDir == "" {
		respondError(w, r, http.StatusNotFound, "NO_OVERRIDE", "")
		return
	}
	ov, err := h.overrides.Get(r.Context(), collectionID, imageType)
	if err != nil || ov == nil || ov.File == "" {
		respondError(w, r, http.StatusNotFound, "NO_OVERRIDE", "no file override for this collection image")
		return
	}
	if !imaging.IsSafePathSegment(ov.File) {
		respondError(w, r, http.StatusNotFound, "NO_OVERRIDE", "")
		return
	}
	path := filepath.Join(h.imageDir, collectionImagesSubdir, ov.File)
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			respondError(w, r, http.StatusNotFound, "FILE_MISSING", "uploaded file is missing on disk")
			return
		}
		respondError(w, r, http.StatusInternalServerError, "READ_FAILED", "")
		return
	}
	defer f.Close() //nolint:errcheck
	info, err := f.Stat()
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, "STAT_FAILED", "")
		return
	}
	var head [512]byte
	n, _ := f.Read(head[:])
	contentType := http.DetectContentType(head[:n])
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		respondError(w, r, http.StatusInternalServerError, "SEEK_FAILED", "")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", CacheControlDailyPublic)
	http.ServeContent(w, r, "", info.ModTime(), f)
}

// parseCollectionImageRoute extrae y valida {id} y {type} de la ruta.
// Devuelve (collectionID, imageType, true) en caso ok, o escribe el
// error y devuelve (_, _, false) — el caller debe abortar.
func (h *CollectionHandler) parseCollectionImageRoute(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	rawID := chi.URLParam(r, "id")
	id := rawID
	if decoded, err := netUrl.PathUnescape(rawID); err == nil {
		id = decoded
	}
	imageType := chi.URLParam(r, "type")
	if imageType != "poster" && imageType != "backdrop" {
		respondError(w, r, http.StatusBadRequest, "INVALID_TYPE", "image type must be poster or backdrop")
		return "", "", false
	}
	return id, imageType, true
}

func (h *CollectionHandler) deleteCollectionImageFile(basename string) {
	if h.imageDir == "" || basename == "" {
		return
	}
	if !imaging.IsSafePathSegment(basename) {
		return
	}
	path := filepath.Join(h.imageDir, collectionImagesSubdir, basename)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		h.logger.Warn("delete orphan collection-image", "path", path, "error", err)
	}
}

// AvailableCollectionImages devuelve las imágenes que TMDb tiene para
// la saga, filtradas por tipo. El admin las ve como cuadrícula en el
// modal del editor y elige una con un click — patrón Jellyfin "Browse
// images". Se guardan como override URL (no se descargan), así un
// futuro cambio de imagen es un PUT trivial y mantenemos cero coste
// de almacenamiento por estas elecciones.
//
// GET /collections/{id}/images/{type}/available
// Admin-only.
func (h *CollectionHandler) AvailableCollectionImages(w http.ResponseWriter, r *http.Request) {
	collectionID, imageType, ok := h.parseCollectionImageRoute(w, r)
	if !ok {
		return
	}
	if h.images == nil {
		respondError(w, r, http.StatusServiceUnavailable, "NO_PROVIDER", "image provider not configured")
		return
	}
	col, err := h.collections.GetByID(r.Context(), collectionID)
	if err != nil || col == nil {
		respondError(w, r, http.StatusNotFound, "NOT_FOUND", "collection not found")
		return
	}
	if col.TMDBID == 0 {
		// Colección sin tmdb_id (caso raro: row legacy o creada sin
		// match). Sin id no hay forma de pedirle imágenes a TMDb.
		respondData(w, http.StatusOK, []any{})
		return
	}

	images, err := h.images.FetchCollectionImages(r.Context(), fmt.Sprintf("%d", col.TMDBID))
	if err != nil {
		h.logger.Warn("fetch collection images", "id", collectionID, "error", err)
		respondError(w, r, http.StatusBadGateway, "PROVIDER_ERROR", "could not list images from provider")
		return
	}

	// Filtra por tipo (poster ↔ primary, backdrop ↔ backdrop). El
	// provider habla "primary" para pósters por consistencia con
	// items; el frontend habla "poster" porque es más natural para
	// colecciones. Mapa local sin más.
	target := "primary"
	if imageType == "backdrop" {
		target = "backdrop"
	}
	data := make([]map[string]any, 0, len(images))
	for _, img := range images {
		if img.Type != target {
			continue
		}
		data = append(data, map[string]any{
			"url":      img.URL,
			"width":    img.Width,
			"height":   img.Height,
			"language": img.Language,
			"score":    img.Score,
			"source":   img.Source,
		})
	}
	respondData(w, http.StatusOK, data)
}

// Compile-time check: el Manager del provider package es lo que se
// inyecta en producción, así que verificamos la conformidad aquí
// (los tests inyectan un mock que también satisface la interfaz).
var _ CollectionImageProvider = (*provider.Manager)(nil)
