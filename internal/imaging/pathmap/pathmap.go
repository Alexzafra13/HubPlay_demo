// Package pathmap guarda la correspondencia entre el ID de una imagen y
// la ruta donde está realmente en disco.
//
// Cada imagen subida o descargada vive en una carpeta por elemento, pero
// la URL pública usa el UUID de la imagen. Como el nombre del fichero
// depende del contenido y no se puede deducir desde el ID, escribimos un
// fichero diminuto por cada imagen dentro de `.mappings/` que contiene
// sólo la ruta absoluta. Para servir una imagen, primero se carga ese
// mapping y luego se lee el fichero que apunta.
//
// Aquí devolvemos errores en vez de tragarlos (como hacían los antiguos
// helpers en `handlers/image.go`) para que quien llame pueda loguear el
// problema sin esconder fallos reales como disco lleno o falta de permisos.
package pathmap

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// ErrInvalidID se devuelve cuando el ID no es un UUID válido. Como no
// llegamos a tocar el sistema de ficheros para IDs inválidos, sirve
// como protección contra intentos de salir del directorio raíz
// (por ejemplo, IDs como "../etc/passwd").
var ErrInvalidID = errors.New("pathmap: invalid id")

// ErrNotFound se devuelve cuando no existe mapping para ese ID. Envuelve
// el error estándar de "fichero no existe" para poder comprobarlo con
// `errors.Is(err, fs.ErrNotExist)`.
var ErrNotFound = fmt.Errorf("pathmap: mapping not found: %w", os.ErrNotExist)

// ErrCorruptMapping se devuelve cuando el fichero de mapping existe
// pero su contenido no es una ruta absoluta bien formada (está vacía,
// es relativa, o contiene `..`). Es una defensa extra: aunque el
// handler ya valida la ruta antes de servir, conviene que la lectura
// nunca devuelva una ruta peligrosa de entrada.
var ErrCorruptMapping = errors.New("pathmap: corrupt mapping")

// Store guarda los mappings ID → ruta en disco bajo un único
// directorio. Es seguro usarlo desde varias goroutines a la vez
// porque sólo hace llamadas al sistema de ficheros, sin estado en
// memoria.
type Store struct {
	dir string // un fichero por mapping
}

// New devuelve un Store que escribe los mappings dentro de
// `<parent>/.mappings/`. El directorio se crea la primera vez que se
// escribe.
func New(parent string) *Store {
	return &Store{dir: filepath.Join(parent, ".mappings")}
}

// Write registra que `imageID` apunta a `localPath`. El directorio se
// crea si no existe. IDs que no son UUID devuelven ErrInvalidID sin
// tocar el disco.
func (s *Store) Write(imageID, localPath string) error {
	if err := validID(imageID); err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("pathmap: create dir: %w", err)
	}
	target := filepath.Join(s.dir, imageID)
	if err := os.WriteFile(target, []byte(localPath), 0o644); err != nil {
		return fmt.Errorf("pathmap: write: %w", err)
	}
	return nil
}

// Read devuelve el path on-disk almacenado para imageID, o
// ErrNotFound si no existe mapping. IDs no-UUID devuelven
// ErrInvalidID sin tocar el filesystem.
//
// Defense-in-depth: si el contenido del fichero no es un path
// absoluto o contiene `..` literal en algún componente, devuelve
// ErrCorruptMapping. Sin esta validación, un mapping editado a
// mano o corrupto podría introducir un path relativo o con `..`
// que `filepath.Join` resolvería contra `cwd` o un padre
// arbitrario (audit olor HHH; complementa F16-1).
func (s *Store) Read(imageID string) (string, error) {
	if err := validID(imageID); err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(s.dir, imageID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("pathmap: read: %w", err)
	}
	p := strings.TrimSpace(string(data))
	if !isWellFormedAbsPath(p) {
		return "", ErrCorruptMapping
	}
	return p, nil
}

// isWellFormedAbsPath rechaza paths vacíos, relativos o con
// componentes `..`. El check de symlinks se hace en el handler con
// EvalSymlinks (ADR-021); aquí solo aseguramos que el path leído
// no es manifiestamente inseguro.
func isWellFormedAbsPath(p string) bool {
	if p == "" || !filepath.IsAbs(p) {
		return false
	}
	for _, seg := range strings.Split(filepath.ToSlash(p), "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

// Remove deletes the mapping for imageID. Missing mappings are not an error
// (idempotent). Invalid IDs return ErrInvalidID.
func (s *Store) Remove(imageID string) error {
	if err := validID(imageID); err != nil {
		return err
	}
	err := os.Remove(filepath.Join(s.dir, imageID))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("pathmap: remove: %w", err)
	}
	return nil
}

// validID rejects anything that doesn't parse as a UUID. This is the
// fundamental defense against path-traversal via the {imageId} URL parameter:
// "../foo" / "abs/olu/te" / "" all fail here before any os.* call.
func validID(id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return ErrInvalidID
	}
	return nil
}
