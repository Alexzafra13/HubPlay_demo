package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/imaging"
)

// Admin-only manual overrides del logo de un canal. La row vive en
// channel_logo_overlays indexada por (library_id, stream_url) — misma
// invariante que el resto de overrides de canal — para sobrevivir al
// próximo re-import del M3U.
//
// Endpoints:
//
//   PUT    /channels/{channelId}/logo            body: {logo_url: "..."}
//   POST   /channels/{channelId}/logo/upload     multipart: file=...
//   DELETE /channels/{channelId}/logo
//
// El handler GET /channels/{channelId}/logo (proxy) consulta el
// override antes de caer al tvg-logo del M3U; ver IPTVHandler.ChannelLogo.

// channelLogosSubdir es la carpeta bajo imageDir donde se guardan los
// archivos subidos. Coincide con el sentinel `iptv.LocalLogoSentinel`
// — el frontend nunca ve el path, sólo le interesa al proxy de logos.
const channelLogosSubdir = "channel-logos"

type setChannelLogoRequest struct {
	LogoURL string `json:"logo_url"`
}

// SetChannelLogo registra un override de URL externa para el logo del
// canal. Sustituye cualquier override previo (URL o archivo subido) —
// si había archivo lo borra del disco para no dejar huérfanos.
//
// La URL se valida superficialmente (esquema http/https + parseable);
// la validación profunda (que devuelva una imagen) la hace el cache
// remoto en la próxima petición de GET /channels/{id}/logo. URLs rotas
// caen al fallback de iniciales como cualquier otro fallo de fetch.
func (h *IPTVHandler) SetChannelLogo(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelId")

	var req setChannelLogoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}
	req.LogoURL = strings.TrimSpace(req.LogoURL)
	if req.LogoURL == "" {
		respondError(w, r, http.StatusBadRequest, "MISSING_URL", "logo_url required (use DELETE to clear)")
		return
	}
	if !isHTTPURL(req.LogoURL) {
		respondError(w, r, http.StatusBadRequest, "INVALID_URL", "logo_url must be http(s)")
		return
	}

	// Recuperamos el override previo ANTES de sobrescribir para saber
	// si había un archivo subido que ahora queda huérfano.
	prev, _ := h.svc.GetChannelLogoOverride(r.Context(), channelID)

	if err := h.svc.SetChannelLogoURL(r.Context(), channelID, req.LogoURL); err != nil {
		handleServiceError(w, r, err)
		return
	}

	if prev != nil && prev.LogoFile != "" {
		h.deleteChannelLogoFile(prev.LogoFile)
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{"channel_id": channelID, "logo_url": req.LogoURL},
	})
}

// UploadChannelLogo guarda un archivo de logo subido por el admin.
// Reutiliza las mismas validaciones que el upload de pósters:
// MaxUploadBytes (10MB), MIME sniffeado de los primeros 512 bytes
// (cliente no se cree para esto), guard de decompression-bomb, y el
// itemID/channel id se valida como path segment seguro antes de
// componer el nombre del archivo.
func (h *IPTVHandler) UploadChannelLogo(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelId")
	if !imaging.IsSafePathSegment(channelID) {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "invalid channel id")
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

	ext := extensionForContentType(sniffed)
	// Basename = "<channel_id>-<unix_nano>.<ext>". El timestamp evita
	// que un upload nuevo machaque la caché del browser para el viejo
	// (el query string del <img> es estable; los bytes detrás cambian).
	// Si el operador sube tres logos seguidos quedan ficheros viejos
	// hasta el siguiente upload — el flujo de SetChannelLogoFile devuelve
	// el previousFile que borramos abajo, así que sólo el "actual" persiste.
	basename := fmt.Sprintf("%s-%d%s", channelID, time.Now().UnixNano(), ext)

	dir := filepath.Join(h.imageDir, channelLogosSubdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		h.logger.Error("create channel-logos dir", "dir", dir, "error", err)
		respondError(w, r, http.StatusInternalServerError, "STORAGE_ERROR", "could not create storage dir")
		return
	}
	dest := filepath.Join(dir, basename)
	if err := os.WriteFile(dest, imgData, 0o644); err != nil {
		h.logger.Error("write channel-logo", "dest", dest, "error", err)
		respondError(w, r, http.StatusInternalServerError, "STORAGE_ERROR", "could not save logo")
		return
	}

	previousFile, err := h.svc.SetChannelLogoFile(r.Context(), channelID, basename)
	if err != nil {
		// Si el upsert falla limpiamos el archivo que acabamos de
		// escribir para no dejar basura en disco sin row asociada.
		_ = os.Remove(dest)
		handleServiceError(w, r, err)
		return
	}
	if previousFile != "" && previousFile != basename {
		h.deleteChannelLogoFile(previousFile)
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{"channel_id": channelID, "logo_file": basename},
	})
}

// ClearChannelLogo borra el override (URL o archivo) del canal. La
// próxima petición al proxy cae al tvg-logo del M3U.
func (h *IPTVHandler) ClearChannelLogo(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelId")
	previousFile, err := h.svc.ClearChannelLogo(r.Context(), channelID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if previousFile != "" {
		h.deleteChannelLogoFile(previousFile)
	}
	w.WriteHeader(http.StatusNoContent)
}

// serveLocalChannelLogo sirve un archivo de logo subido directamente
// desde <imageDir>/channel-logos/<basename>. Validamos otra vez el
// basename como path segment seguro defensivamente — la upsert ya lo
// validó pero un attacker que pueda escribir directo en la tabla no
// debería conseguir leer ficheros fuera del directorio designado.
func (h *IPTVHandler) serveLocalChannelLogo(w http.ResponseWriter, r *http.Request, channelID, basename string) {
	if h.imageDir == "" {
		respondError(w, r, http.StatusNotFound, "NO_LOGO", "image storage not configured")
		return
	}
	if !imaging.IsSafePathSegment(basename) {
		h.logger.Warn("rejected unsafe channel-logo basename", "channel", channelID, "basename", basename)
		respondError(w, r, http.StatusNotFound, "LOGO_UNAVAILABLE", "")
		return
	}
	path := filepath.Join(h.imageDir, channelLogosSubdir, basename)
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			respondError(w, r, http.StatusNotFound, "LOGO_UNAVAILABLE", "uploaded logo file is missing")
			return
		}
		h.logger.Error("read channel-logo file", "channel", channelID, "path", path, "error", err)
		respondError(w, r, http.StatusInternalServerError, "LOGO_READ_FAILED", "")
		return
	}
	defer f.Close() //nolint:errcheck

	info, err := f.Stat()
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, "LOGO_STAT_FAILED", "")
		return
	}

	var head [512]byte
	n, _ := f.Read(head[:])
	contentType := http.DetectContentType(head[:n])
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		respondError(w, r, http.StatusInternalServerError, "LOGO_SEEK_FAILED", "")
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", CacheControlDailyPublic)
	http.ServeContent(w, r, "", info.ModTime(), f)
}

// deleteChannelLogoFile borra un archivo de logo huérfano del disco.
// No es bloqueante para la respuesta — los errores se logan y ya está;
// un archivo huérfano es un coste menor frente a un 5xx por una
// limpieza de housekeeping.
func (h *IPTVHandler) deleteChannelLogoFile(basename string) {
	if h.imageDir == "" || basename == "" {
		return
	}
	if !imaging.IsSafePathSegment(basename) {
		return
	}
	path := filepath.Join(h.imageDir, channelLogosSubdir, basename)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		h.logger.Warn("delete orphan channel-logo", "path", path, "error", err)
	}
}

func isHTTPURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func extensionForContentType(ct string) string {
	switch ct {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	default:
		return ".jpg"
	}
}

// RefreshLogosFromIPTVOrg dispara el lookup masivo contra iptv-org
// (https://iptv-org.github.io/api/channels.json) para todos los canales
// de una biblioteca que aún no tengan logo. Cada match se persiste
// como override de URL en channel_logo_overrides — el operador puede
// borrarlos individualmente con DELETE /channels/{id}/logo.
//
// Idempotente: una segunda llamada no reescribe los que ya tienen
// override (incluso si vinieron de un run anterior de este endpoint).
//
// POST /libraries/{libraryId}/iptv/refresh-logos-from-iptv-org
// Admin-only. Retorna el count de canales actualizados.
func (h *IPTVHandler) RefreshLogosFromIPTVOrg(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	sum, err := h.svc.RefreshLogosFromIPTVOrg(r.Context(), libraryID)
	if err != nil {
		// "not configured" colapsa a 503 igual que el resto del flujo
		// (el operador puede no haber montado el lookup en tests).
		if strings.Contains(err.Error(), "not configured") {
			respondError(w, r, http.StatusServiceUnavailable, "NO_IPTV_ORG", err.Error())
			return
		}
		h.logger.Error("iptv-org refresh failed", "library", libraryID, "error", err)
		respondError(w, r, http.StatusBadGateway, "IPTV_ORG_ERROR", "could not refresh logos from iptv-org")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"library_id":           libraryID,
			"total":                sum.Total,
			"already_have_logo":    sum.AlreadyHaveLogo,
			"without_tvg_id":       sum.WithoutTvgID,
			"skipped_has_override": sum.SkippedHasOverride,
			"not_in_database":      sum.NotInDatabase,
			"updated":              sum.Updated,
		},
	})
}
