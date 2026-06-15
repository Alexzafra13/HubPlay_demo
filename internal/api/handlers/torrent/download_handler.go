package torrenthandler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/api/handlers"
	"hubplay/internal/torrentstream"
)

// Downloader is the slice of *torrentstream.Manager the download handler
// needs.
type Downloader interface {
	StartDownload(src, destDir string, onDone func(error)) torrentstream.DownloadJob
	Downloads() []torrentstream.DownloadJob
	// RemoveDownload descarta un job terminal; (false,nil) si no existe,
	// ErrDownloadActive si sigue en vuelo.
	RemoveDownload(id string) (bool, error)
}

// LibraryTarget resolves where a downloaded title should land (a "Descargas"
// folder inside the matching library) and triggers a rescan once it's there.
// Implemented in the composition root over the library service.
type LibraryTarget interface {
	DownloadDir(ctx context.Context, mt torrentstream.MediaType) (libraryID, dir string, err error)
	Scan(ctx context.Context, libraryID string) error
}

// DownloadHandler serves "download to library": fetch a source fully into
// the library's Descargas folder and rescan so it appears as a normal item
// (and plays through HubPlay's regular pipeline, which handles MKV/HEVC).
type DownloadHandler struct {
	dl         Downloader
	lib        LibraryTarget
	adminCheck func(*http.Request) bool
	logger     *slog.Logger
}

func NewDownloadHandler(dl Downloader, lib LibraryTarget, adminCheck func(*http.Request) bool, logger *slog.Logger) *DownloadHandler {
	if logger == nil {
		logger = slog.Default()
	}
	if adminCheck == nil {
		adminCheck = claimsAdmin
	}
	return &DownloadHandler{dl: dl, lib: lib, adminCheck: adminCheck, logger: logger}
}

type downloadRequest struct {
	Src  string `json:"src"`
	Type string `json:"type"` // movie | series
}

// Create starts a download to the library. Admin-only (it spends bandwidth +
// disk).
//
// POST /torrent/download
func (h *DownloadHandler) Create(w http.ResponseWriter, r *http.Request) {
	if !h.adminCheck(r) {
		handlers.RespondError(w, r, http.StatusForbidden, "FORBIDDEN",
			"only an administrator can start a download")
		return
	}
	var req downloadRequest
	if err := handlers.DecodeJSON(w, r, &req); err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	src := strings.TrimSpace(req.Src)
	if !isAllowedSource(src) {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_SOURCE",
			"src must be a magnet link or an http(s) .torrent URL")
		return
	}
	mt := torrentstream.MediaTypeMovie
	if req.Type == "series" {
		mt = torrentstream.MediaTypeSeries
	}

	libraryID, dir, err := h.lib.DownloadDir(r.Context(), mt)
	if err != nil {
		h.logger.Warn("download: no target library", "type", mt, "error", err)
		handlers.RespondError(w, r, http.StatusUnprocessableEntity, "NO_LIBRARY",
			"no library configured for this content type")
		return
	}

	job := h.dl.StartDownload(src, dir, func(derr error) {
		if derr != nil {
			return
		}
		// Rescan on a fresh context — the request is long gone by now.
		if err := h.lib.Scan(context.Background(), libraryID); err != nil {
			h.logger.Warn("download: rescan failed", "library", libraryID, "error", err)
		}
	})
	handlers.RespondData(w, http.StatusAccepted, job)
}

// List returns the current download jobs. Admin-only — la lista revela qué
// se está bajando al servidor, así que se gatea igual que el POST.
//
// GET /torrent/downloads
func (h *DownloadHandler) List(w http.ResponseWriter, r *http.Request) {
	if !h.adminCheck(r) {
		handlers.RespondError(w, r, http.StatusForbidden, "FORBIDDEN",
			"only an administrator can view downloads")
		return
	}
	handlers.RespondData(w, http.StatusOK, h.dl.Downloads())
}

// Delete descarta un job terminal del listado (el "×" del panel). Admin-only.
//
// DELETE /torrent/downloads/{id}
func (h *DownloadHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if !h.adminCheck(r) {
		handlers.RespondError(w, r, http.StatusForbidden, "FORBIDDEN",
			"only an administrator can dismiss downloads")
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		handlers.RespondError(w, r, http.StatusBadRequest, "MISSING_ID", "download id required")
		return
	}
	ok, err := h.dl.RemoveDownload(id)
	switch {
	case errors.Is(err, torrentstream.ErrDownloadActive):
		handlers.RespondError(w, r, http.StatusConflict, "DOWNLOAD_ACTIVE",
			"the download is still in progress")
	case !ok:
		handlers.RespondError(w, r, http.StatusNotFound, "NOT_FOUND", "download not found")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// ErrNoLibrary is returned by a LibraryTarget when no matching library
// exists.
var ErrNoLibrary = errors.New("no matching library")
