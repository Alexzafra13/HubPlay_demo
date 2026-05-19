package upload

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrUnsafePath: una resolución de path acabó fuera del directorio
// confinado. Sólo debería ocurrir si SanitizeFilename tiene un bug —
// el assert vive aquí para que el upload service no escriba fuera del
// staging dir aunque algo upstream falle.
var ErrUnsafePath = errors.New("resolved path escapes its confinement directory")

// StagingDir gestiona el directorio donde se materializan los bytes
// recibidos por tus antes de ser validados y movidos a la librería.
// Layout en disco:
//
//	<dataDir>/uploads/staging/<userID>/<uploadID>/<sanitizedName>
//
// Razones para el sub-directorio por upload:
//   - Aísla nombres entre subidas concurrentes del mismo usuario (el
//     uploadID es único). Sin esto, dos uploads que se llamen
//     "movie.mkv" colisionarían.
//   - tusd ya crea su propio fichero `<uploadID>` + `.info` JSON en el
//     mismo path; el sub-dir por upload deja el final-name como un
//     vecino ordenado en vez de mezclar todo en un dir plano.
//   - Cleanup tras una cancelación borra el dir entero — una operación,
//     atómica para el usuario.
type StagingDir struct {
	root string // <dataDir>/uploads/staging
}

// NewStagingDir crea (si hace falta) la raíz de staging y devuelve el
// gestor. La ruta es absoluta antes de devolverla para que checks
// posteriores no sean engañados por un `..` en la config.
func NewStagingDir(root string) (*StagingDir, error) {
	if root == "" {
		return nil, errors.New("staging dir cannot be empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve staging root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, fmt.Errorf("mkdir staging root: %w", err)
	}
	return &StagingDir{root: abs}, nil
}

// Root devuelve la raíz absoluta. Lo expone para que el tusd FileStore
// pueda configurarse contra el mismo path — tusd escribe su `.info`
// + el blob en este directorio.
func (s *StagingDir) Root() string { return s.root }

// UploadDir devuelve la ruta (creándola si hace falta) para un upload
// en particular. uploadID viene del cliente vía tus; nosotros lo
// validamos que sea un identificador seguro antes de llamar.
func (s *StagingDir) UploadDir(userID, uploadID string) (string, error) {
	if !safeIDSegment(userID) {
		return "", fmt.Errorf("%w: user id", ErrUnsafePath)
	}
	if !safeIDSegment(uploadID) {
		return "", fmt.Errorf("%w: upload id", ErrUnsafePath)
	}
	dir := filepath.Join(s.root, userID, uploadID)
	if err := s.ensureInside(dir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("mkdir upload dir: %w", err)
	}
	return dir, nil
}

// ResolveFinalPath compone la ruta del fichero ya validado dentro del
// upload-dir. sanitizedName tiene que venir de SanitizeFilename. La
// función vuelve a chequear que la ruta resultante sigue dentro del
// dir confinado — barrera de profundidad por si la sanitización tiene
// un bug futuro.
func (s *StagingDir) ResolveFinalPath(userID, uploadID, sanitizedName string) (string, error) {
	dir, err := s.UploadDir(userID, uploadID)
	if err != nil {
		return "", err
	}
	if sanitizedName == "" {
		return "", fmt.Errorf("empty sanitized name")
	}
	full := filepath.Join(dir, sanitizedName)
	if err := s.ensureInside(full); err != nil {
		return "", err
	}
	return full, nil
}

// RemoveUpload borra el directorio entero del upload (incluido el
// blob de tusd, su `.info`, y cualquier fichero intermedio). Es
// best-effort: si falla, el caller recibe el error pero el upload
// ya está cerrado lógicamente — un orphan en disco no rompe nada
// hasta el siguiente GC manual.
func (s *StagingDir) RemoveUpload(userID, uploadID string) error {
	if !safeIDSegment(userID) || !safeIDSegment(uploadID) {
		return fmt.Errorf("%w: id segment", ErrUnsafePath)
	}
	dir := filepath.Join(s.root, userID, uploadID)
	if err := s.ensureInside(dir); err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// MoveTo mueve un fichero del staging a `targetPath` de forma atómica
// si los dos paths están en el mismo filesystem (rename), o copia +
// borra si están en filesystems distintos (volumen separado para la
// librería frente al de uploads, caso común en NAS).
//
// targetPath se valida que (a) sea absoluto y (b) NO esté dentro del
// staging dir — un upload no puede aterrizar dentro de su propio
// staging. La existencia previa del fichero destino se respeta:
// devuelve ErrTargetExists para que el caller añada un sufijo y
// reintente sin pisar nada.
func (s *StagingDir) MoveTo(sourcePath, targetPath string) error {
	if !filepath.IsAbs(sourcePath) || !filepath.IsAbs(targetPath) {
		return errors.New("both paths must be absolute")
	}
	// El source DEBE vivir dentro del staging — barrera contra que el
	// caller pase otra cosa por error.
	if err := s.ensureInside(sourcePath); err != nil {
		return fmt.Errorf("source %w", err)
	}
	// El target NO debe vivir dentro del staging — sería absurdo pero
	// el assert es barato y atrapa bugs de cableado.
	if rel, err := filepath.Rel(s.root, targetPath); err == nil && !strings.HasPrefix(rel, "..") {
		return errors.New("target lives inside staging dir")
	}

	if _, err := os.Stat(targetPath); err == nil {
		return ErrTargetExists
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("mkdir target dir: %w", err)
	}

	// Atomic rename first; cross-device falls back to copy+delete.
	if err := os.Rename(sourcePath, targetPath); err == nil {
		return nil
	} else if !isCrossDevice(err) {
		return fmt.Errorf("rename: %w", err)
	}

	if err := copyFile(sourcePath, targetPath); err != nil {
		return fmt.Errorf("cross-device copy: %w", err)
	}
	if err := os.Remove(sourcePath); err != nil {
		// El destino está bien; el source quedó huérfano pero no es
		// blocking — devolvemos el error para que el caller lo logue.
		return fmt.Errorf("copy ok, source remove failed: %w", err)
	}
	return nil
}

// ErrTargetExists: el destino del move ya existe. El caller decide si
// añadir sufijo y reintentar, o abortar.
var ErrTargetExists = errors.New("target file already exists")

// ─── helpers ────────────────────────────────────────────────────────

// safeIDSegment acepta caracteres que tusd o nuestro generator usan
// para uploadID + el formato de userID (UUID-ish). Rechaza separadores
// de path, dot-relative, vacíos.
func safeIDSegment(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// ensureInside verifica que `path` resuelve dentro de s.root. Defensa
// en profundidad — los callers ya construyen rutas seguras, pero un
// símlink o un bug futuro podrían sacar la ruta del dir confinado.
func (s *StagingDir) ensureInside(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("abs: %w", err)
	}
	rel, err := filepath.Rel(s.root, abs)
	if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
		return ErrUnsafePath
	}
	return nil
}

// isCrossDevice detecta el error EXDEV (rename between filesystems).
// Es el único caso de fallback razonable — el resto de errores de
// rename son fatales y los propagamos.
func isCrossDevice(err error) bool {
	if err == nil {
		return false
	}
	// LinkError envuelve syscall.Errno; comparamos por mensaje porque
	// importar syscall.EXDEV nos ataría a una platform-list explícita.
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return strings.Contains(strings.ToLower(linkErr.Err.Error()), "cross-device")
	}
	return false
}

// copyFile copia byte a byte. No usa io.Copy directo para que el caller
// pueda ver el error de cada fase por separado en el log.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open src: %w", err)
	}
	defer in.Close() //nolint:errcheck

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return fmt.Errorf("create dst: %w", err)
	}
	defer out.Close() //nolint:errcheck

	// 1 MiB buffer en vez del default 32 KiB de io.Copy — para
	// uploads de varios GB recorta ~30x el ratio syscalls/byte.
	buf := make([]byte, 1<<20)
	if _, err := io.CopyBuffer(out, in, buf); err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("copy: %w", err)
	}
	return out.Sync()
}

// RandomID devuelve un identificador hex aleatorio para uploads
// generados server-side (p.ej. el id de la auditoría). 16 bytes
// → 32 chars hex; colisión despreciable.
func RandomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Casi imposible (crypto/rand fallando es kernel-level), pero
		// si pasa devolvemos un id no-vacío para no panicar — los
		// callers ven el rastro raro en logs y la auditoría sigue.
		return "rand-fallback-broken"
	}
	return hex.EncodeToString(b[:])
}
