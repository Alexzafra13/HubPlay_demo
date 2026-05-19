package upload

import (
	"context"
	"errors"
	"fmt"

	librarymodel "hubplay/internal/library/model"
)

// ErrNoLibraryDestination: el usuario no tiene ninguna librería en la
// que pueda aterrizar el upload (sin grants, o todas son livetv / sin
// paths configurados). El handler la traduce a 412 + mensaje al cliente.
var ErrNoLibraryDestination = errors.New("no library available as upload destination")

// ErrLibraryNotEligible: la librería especificada existe y el usuario
// tiene acceso, pero su ContentType no admite el kind detectado (p.ej.
// subir un video a una librería de música, o intentar usar una livetv).
var ErrLibraryNotEligible = errors.New("library is not eligible for this upload kind")

// LibraryStore es la mínima superficie del library repo que el picker
// necesita. Definida aquí (en vez de importar el repo) para testear
// con un fake sin spinning de DB.
type LibraryStore interface {
	GetByID(ctx context.Context, id string) (*librarymodel.Library, error)
	ListForUser(ctx context.Context, userID string) ([]*librarymodel.Library, error)
}

// LibraryPicker resuelve la pareja (libraryID, targetPath) donde ha de
// aterrizar un upload. Las reglas vienen del producto:
//
//   - Si el cliente pasa un `library_id` en la metadata de tus, se
//     valida y se usa (siempre que el usuario tenga acceso y el kind
//     case con ContentType).
//   - Si no, se busca la primera librería de tipo "movies" o "shows"
//     a la que el usuario tenga grant. Determinismo: orden por nombre
//     ASC para que dos uploads consecutivos sin hint aterricen en la
//     misma librería.
//
// Subtítulos requieren `library_id` explícito en v1: no podemos
// adivinar a qué item del catálogo pertenecen sin más contexto, así
// que el cliente decide. (Una iteración futura podría inferirlo del
// nombre `Movie.es.srt` → item llamado Movie.* — fuera de scope.)
type LibraryPicker struct {
	libraries LibraryStore
}

func NewLibraryPicker(libraries LibraryStore) *LibraryPicker {
	return &LibraryPicker{libraries: libraries}
}

// PickDestination devuelve la librería destino + el primer path
// configurado en ella. El caller compone la ruta final como
// `<path>/<sanitizedName>`.
//
// `hintLibraryID` es opcional (vacío = auto-pick). `userID` viene del
// JWT del request original.
func (p *LibraryPicker) PickDestination(ctx context.Context, userID, hintLibraryID string, kind MediaKind) (*librarymodel.Library, error) {
	if hintLibraryID != "" {
		return p.resolveHint(ctx, userID, hintLibraryID, kind)
	}
	return p.autoPick(ctx, userID, kind)
}

// resolveHint valida un library_id propuesto por el cliente.
func (p *LibraryPicker) resolveHint(ctx context.Context, userID, libraryID string, kind MediaKind) (*librarymodel.Library, error) {
	lib, err := p.libraries.GetByID(ctx, libraryID)
	if err != nil {
		return nil, fmt.Errorf("hinted library %s: %w", libraryID, err)
	}
	if !p.userHasAccess(ctx, userID, libraryID) {
		// No filtramos por "existe vs sin acceso" — desde la
		// perspectiva del cliente ambos son "no puedes ahí".
		return nil, fmt.Errorf("hinted library %s: %w", libraryID, ErrNoLibraryDestination)
	}
	if !isCompatible(lib.ContentType, kind) {
		return nil, fmt.Errorf("library content_type=%q vs kind=%s: %w",
			lib.ContentType, kind, ErrLibraryNotEligible)
	}
	if len(lib.Paths) == 0 {
		return nil, fmt.Errorf("library %s has no paths configured: %w",
			libraryID, ErrNoLibraryDestination)
	}
	return lib, nil
}

// autoPick recorre las librerías del usuario y devuelve la primera
// compatible. Determinismo: el repo ya entrega un orden estable; no
// re-ordenamos aquí para no añadir variabilidad.
func (p *LibraryPicker) autoPick(ctx context.Context, userID string, kind MediaKind) (*librarymodel.Library, error) {
	libs, err := p.libraries.ListForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list libraries for %s: %w", userID, err)
	}
	for _, lib := range libs {
		if !isCompatible(lib.ContentType, kind) {
			continue
		}
		if len(lib.Paths) == 0 {
			continue
		}
		return lib, nil
	}
	return nil, ErrNoLibraryDestination
}

// userHasAccess comprueba que el userID figura en library_access para
// libraryID. Lo decide vía ListForUser para reutilizar la lógica
// existente (incluye normalización profile→parent).
func (p *LibraryPicker) userHasAccess(ctx context.Context, userID, libraryID string) bool {
	libs, err := p.libraries.ListForUser(ctx, userID)
	if err != nil {
		return false
	}
	for _, l := range libs {
		if l.ID == libraryID {
			return true
		}
	}
	return false
}

// isCompatible decide qué content_type acepta cada kind. Mapa pequeño,
// lo dejamos explícito para que cambiar la política sea evidente.
//
// - movies: acepta video. (Subtítulos: el cliente apunta a una librería
//   de movies si el item al que pertenecen vive ahí — semánticamente
//   ok aunque la nomenclatura sea "movies".)
// - shows : igual que movies.
// - music : nada por ahora (no soportamos uploads de audio en v1).
// - livetv: nada — no es un destino de upload, los canales se ingieren
//   por M3U.
func isCompatible(contentType string, kind MediaKind) bool {
	switch contentType {
	case "movies", "shows":
		return kind == KindVideo || kind == KindSubtitle
	default:
		return false
	}
}
