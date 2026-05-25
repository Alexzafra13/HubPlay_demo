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
	w.Header().Set("Cache-Control", CacheControlListingShort)
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

// DeleteEntry borra un fichero o una carpeta dentro de la librería.
//
//   DELETE /libraries/{id}/files?path=Movies/Drama/old.mkv
//   DELETE /libraries/{id}/files?path=Movies/Drama&recursive=true
//
// Reglas:
//   - path REQUERIDO y no puede ser "" (no borramos la librería entera).
//   - Si el path es una carpeta NO VACÍA, requiere ?recursive=true.
//     Defensa contra borrar accidentalmente cientos de GB.
//   - Idempotente: borrar algo inexistente devuelve 204 igual (mismo
//     contrato que el repo de cors_origins).
//
// Permiso: can_upload (el operador que sube también puede limpiar).
// Para borrados masivos / library-level CRUD el flag correcto es
// can_manage_libraries, pero borrar un fichero individual cae en
// "gestionar mi propia subida".
func (h *UploadBrowseHandler) DeleteEntry(w http.ResponseWriter, r *http.Request) {
	lib, ok := h.resolveLibrary(w, r)
	if !ok {
		return
	}

	rawPath := r.URL.Query().Get("path")
	recursive := r.URL.Query().Get("recursive") == "true"

	canonical, err := upload.SanitizeSubpath(rawPath)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_PATH", err.Error())
		return
	}
	if canonical == "" {
		respondError(w, r, http.StatusBadRequest, "EMPTY_PATH",
			"path required (cannot delete the library root)")
		return
	}

	abs, err := upload.ResolveSubpath(lib.Paths[0], canonical)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_PATH", err.Error())
		return
	}

	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			// Idempotente: borrar lo que ya no está es éxito.
			w.WriteHeader(http.StatusNoContent)
			return
		}
		respondError(w, r, http.StatusInternalServerError, "STAT_FAILED", err.Error())
		return
	}

	if info.IsDir() && !recursive {
		// Comprueba si está vacío — un dir vacío SÍ se borra sin
		// recursive. Defensa solo para "tiene contenido dentro".
		entries, err := os.ReadDir(abs)
		if err != nil {
			respondError(w, r, http.StatusInternalServerError, "READ_DIR_FAILED", err.Error())
			return
		}
		if len(entries) > 0 {
			respondError(w, r, http.StatusConflict, "DIR_NOT_EMPTY",
				"directory not empty; pass ?recursive=true to confirm")
			return
		}
	}

	if err := os.RemoveAll(abs); err != nil {
		respondError(w, r, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
		return
	}

	h.logger.Info("upload entry deleted",
		"library", lib.ID, "path", canonical, "was_dir", info.IsDir())
	w.WriteHeader(http.StatusNoContent)
}

// RenameRequest mapea el body POST.
type RenameRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// RenameEntry renombra o mueve un fichero/carpeta dentro de la
// librería.
//
//   POST /libraries/{id}/files/rename body: {from: "old.mkv", to: "new.mkv"}
//
// Reglas:
//   - Ambos paths se sanitizan + se validan que viven dentro de la
//     librería.
//   - From debe existir; To no debe existir (no permitimos pisar
//     ficheros con rename — eso es delete + rename).
//   - Idempotente NO: si from==to devuelve 400 BAD_REQUEST. Si el
//     usuario quería confirmar "no cambies" no debería llamar al
//     endpoint.
//   - Funciona para ficheros y carpetas — os.Rename los acepta
//     ambos en el mismo filesystem.  Cross-fs rename falla; lo
//     señalamos como CROSS_DEVICE — el operador típicamente NO
//     puede mover entre librerías con paths en discos distintos
//     desde la UI (otra librería = otra API call).
func (h *UploadBrowseHandler) RenameEntry(w http.ResponseWriter, r *http.Request) {
	lib, ok := h.resolveLibrary(w, r)
	if !ok {
		return
	}

	var req RenameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "BAD_BODY", err.Error())
		return
	}

	fromCanon, err := upload.SanitizeSubpath(req.From)
	if err != nil || fromCanon == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_FROM",
			"from path is invalid or empty")
		return
	}
	toCanon, err := upload.SanitizeSubpath(req.To)
	if err != nil || toCanon == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_TO",
			"to path is invalid or empty")
		return
	}
	if fromCanon == toCanon {
		respondError(w, r, http.StatusBadRequest, "SAME_PATH",
			"from and to resolve to the same path")
		return
	}

	fromAbs, err := upload.ResolveSubpath(lib.Paths[0], fromCanon)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_FROM", err.Error())
		return
	}
	toAbs, err := upload.ResolveSubpath(lib.Paths[0], toCanon)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_TO", err.Error())
		return
	}

	if _, err := os.Stat(fromAbs); err != nil {
		if os.IsNotExist(err) {
			respondError(w, r, http.StatusNotFound, "FROM_NOT_FOUND",
				"source path does not exist")
			return
		}
		respondError(w, r, http.StatusInternalServerError, "STAT_FAILED", err.Error())
		return
	}
	if _, err := os.Stat(toAbs); err == nil {
		respondError(w, r, http.StatusConflict, "TO_EXISTS",
			"destination already exists; delete it first")
		return
	}

	// Asegúrate de que el dir destino existe (caso típico: rename
	// movie.mkv → 2024/Movie/movie.mkv requiere mkdir intermedio).
	if err := os.MkdirAll(filepath.Dir(toAbs), 0o755); err != nil {
		respondError(w, r, http.StatusInternalServerError, "MKDIR_FAILED", err.Error())
		return
	}

	if err := os.Rename(fromAbs, toAbs); err != nil {
		// Cross-device errors se identifican por mensaje (mismo
		// patrón que upload.staging.isCrossDevice). Para v1 lo
		// señalamos como UNSUPPORTED — mover entre filesystems
		// requiere copy+remove que no haremos en este endpoint.
		respondError(w, r, http.StatusInternalServerError, "RENAME_FAILED", err.Error())
		return
	}

	h.logger.Info("upload entry renamed",
		"library", lib.ID, "from", fromCanon, "to", toCanon)
	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"library_id": lib.ID,
			"from":       fromCanon,
			"to":         toCanon,
		},
	})
}

// resolveLibrary valida que la librería existe y el caller tiene
// acceso. Devuelve (lib, true) en éxito; en fallo escribe el response
// y retorna (nil, false). Sin filtrar existencia (always 404, never
// 403) — un user que no tiene acceso no debería poder enumerar.
func (h *UploadBrowseHandler) resolveLibrary(w http.ResponseWriter, r *http.Request) (*librarymodel.Library, bool) {
	id := requireParam(w, r, "id")
	if id == "" {
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
