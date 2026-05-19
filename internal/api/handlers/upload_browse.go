package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/auth"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/upload"
)

// UploadBrowseHandler implementa el explorador de carpetas estilo
// SFTP/Termius dentro de una librería destino (PR6 file explorer).
//
//   GET  /api/v1/libraries/{id}/upload-browse?path=Movies/Drama
//     lista subdirs del path indicado DENTRO de la librería.
//     - path vacío = raíz de la librería.
//     - Sólo subdirs (no ficheros) — el cliente sube, no inspecciona
//       contenido existente.
//     - Ordenados alfabéticamente para que la UI sea estable.
//
//   POST /api/v1/libraries/{id}/folders     body: {path: "Movies/New"}
//     crea una carpeta nueva dentro de la librería. Idempotente
//     (MkdirAll). Devuelve el path canónico tras sanitizar.
//
// Gate (router): can_upload — un user que no puede subir no necesita
// el explorador. El owner pasa automático.
//
// Defense in depth:
//   - El libraryID debe estar en las librerías a las que el user
//     tiene acceso. Devuelve 404 (no 403) para no filtrar existencia.
//   - El path se valida con upload.ResolveSubpath, que rechaza
//     traversal, paths absolutos, y normaliza separadores.
//   - Si la ruta resuelta no existe al hacer ReadDir, devolvemos []
//     en vez de 404 — el frontend acaba de crear la carpeta y aún
//     no se ha materializado en disco (caso happy path del "New
//     folder + browse"); preferimos no romper la UX por una race.
// LibraryLister es la mínima superficie que UploadBrowseHandler
// necesita del LibraryService — sólo ListForUser para resolver
// acceso. La interface ancha LibraryService también la cumple, así
// que el router pasa deps.Libraries sin cambios.
type LibraryLister interface {
	ListForUser(ctx context.Context, userID string) ([]*librarymodel.Library, error)
}

type UploadBrowseHandler struct {
	libraries LibraryLister
	logger    *slog.Logger
}

func NewUploadBrowseHandler(libraries LibraryLister, logger *slog.Logger) *UploadBrowseHandler {
	return &UploadBrowseHandler{
		libraries: libraries,
		logger:    logger.With("module", "upload-browse-handler"),
	}
}

// Browse lista subdirs del subpath indicado dentro de la librería.
func (h *UploadBrowseHandler) Browse(w http.ResponseWriter, r *http.Request) {
	lib, ok := h.resolveLibrary(w, r)
	if !ok {
		return
	}
	subpath := r.URL.Query().Get("path")

	abs, err := upload.ResolveSubpath(lib.Paths[0], subpath)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_PATH", err.Error())
		return
	}

	// Re-sanitize para construir el path canónico que devolvemos en
	// la respuesta (lo que el frontend usa como base para construir
	// breadcrumbs sin re-parsear).
	canonical, _ := upload.SanitizeSubpath(subpath)

	entries, err := os.ReadDir(abs)
	if err != nil {
		if os.IsNotExist(err) {
			// Carpeta aún no existente (recién creada via POST que
			// devolvió el path pero MkdirAll no había ocurrido si la
			// otra punta de la red la pidió antes) — devolver vacío
			// es la respuesta amable.
			respondJSON(w, http.StatusOK, map[string]any{
				"data": map[string]any{
					"library_id":   lib.ID,
					"library_name": lib.Name,
					"path":         canonical,
					"directories":  []any{},
				},
			})
			return
		}
		h.logger.Warn("upload browse read dir failed",
			"library", lib.ID, "path", abs, "error", err)
		respondError(w, r, http.StatusInternalServerError, "READ_DIR_FAILED", err.Error())
		return
	}

	type dirEntry struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	type fileEntry struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	}
	dirs := make([]dirEntry, 0)
	files := make([]fileEntry, 0)
	for _, e := range entries {
		name := e.Name()
		// Filtra dotfiles (cualquier dir/fichero que empieza por . es
		// de sistema: .DS_Store, .git, .stfolder, .nfo a veces...).
		if len(name) > 0 && name[0] == '.' {
			continue
		}
		if e.IsDir() {
			var childPath string
			if canonical == "" {
				childPath = name
			} else {
				childPath = canonical + "/" + name
			}
			dirs = append(dirs, dirEntry{Name: name, Path: childPath})
		} else {
			// Ficheros: incluimos size para que el cliente pinte algo
			// como "Pelicula.mkv · 2.4 GiB" y el operador pueda ver de
			// un vistazo qué hay ya en la carpeta sin tener que ir al
			// catálogo. Si stat falla, size=0 y seguimos — un fichero
			// que no podemos statear (race con un borrado) no debería
			// tirar el browse entero.
			var size int64
			if info, err := e.Info(); err == nil {
				size = info.Size()
			}
			files = append(files, fileEntry{Name: name, Size: size})
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })

	// Cache corta — el operador navega rápido entre carpetas y NO
	// queremos re-readdir por cada click si está en el mismo nivel.
	w.Header().Set("Cache-Control", "private, max-age=15")
	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"library_id":   lib.ID,
			"library_name": lib.Name,
			"path":         canonical,
			"directories":  dirs,
			"files":        files,
		},
	})
}

// CreateFolderRequest mapea el body POST.
type CreateFolderRequest struct {
	Path string `json:"path"`
}

// CreateFolder crea una carpeta nueva dentro de la librería.
// Idempotente: si ya existe, devuelve OK sin tocar nada.
func (h *UploadBrowseHandler) CreateFolder(w http.ResponseWriter, r *http.Request) {
	lib, ok := h.resolveLibrary(w, r)
	if !ok {
		return
	}

	var req CreateFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "BAD_BODY", err.Error())
		return
	}

	canonical, err := upload.SanitizeSubpath(req.Path)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_PATH", err.Error())
		return
	}
	if canonical == "" {
		// "Crear la raíz de la librería" no tiene sentido — siempre
		// existe. Devolvemos 400 para que el frontend sepa que ese
		// click no hace nada.
		respondError(w, r, http.StatusBadRequest, "EMPTY_PATH",
			"path required (non-empty subpath within the library)")
		return
	}

	abs, err := upload.ResolveSubpath(lib.Paths[0], canonical)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_PATH", err.Error())
		return
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		respondError(w, r, http.StatusInternalServerError, "MKDIR_FAILED", err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"data": map[string]any{
			"library_id": lib.ID,
			"path":       canonical,
			"abs_path":   filepath.ToSlash(abs),
		},
	})
}

// resolveLibrary valida que la librería existe y el caller tiene
// acceso. Devuelve (lib, true) en éxito; en fallo escribe el response
// y retorna (nil, false). Sin filtrar existencia (always 404, never
// 403) — un user que no tiene acceso no debería poder enumerar.
func (h *UploadBrowseHandler) resolveLibrary(w http.ResponseWriter, r *http.Request) (*librarymodel.Library, bool) {
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "MISSING_ID", "library id required")
		return nil, false
	}

	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return nil, false
	}

	libs, err := h.libraries.ListForUser(r.Context(), claims.UserID)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, "LIST_FAILED", err.Error())
		return nil, false
	}
	for _, l := range libs {
		if l.ID == id {
			if len(l.Paths) == 0 {
				respondError(w, r, http.StatusConflict, "LIBRARY_NO_PATHS",
					"library has no paths configured")
				return nil, false
			}
			return l, true
		}
	}
	respondError(w, r, http.StatusNotFound, "NOT_FOUND", "library not found or not accessible")
	return nil, false
}

// Helper para tests: expone resolveLibrary con un nombre estable.
func (h *UploadBrowseHandler) ResolveLibraryForTests(ctx context.Context, userID, libID string) (*librarymodel.Library, error) {
	libs, err := h.libraries.ListForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, l := range libs {
		if l.ID == libID {
			return l, nil
		}
	}
	return nil, os.ErrNotExist
}
