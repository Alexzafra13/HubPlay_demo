package upload

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrSubpathInvalid: el subpath propuesto por el cliente para
// aterrizar el upload no es seguro. Cubre traversal (`..`), paths
// absolutos, separadores invertidos, segmentos vacíos.  El handler
// HTTP lo traduce a 400 INVALID_SUBPATH para que el cliente entienda
// que el problema es de validación, no de servidor.
var ErrSubpathInvalid = errors.New("upload subpath is invalid")

// SanitizeSubpath valida y canonicaliza un subpath relativo a una
// librería destino.  Reglas:
//
//   1. Vacío y "." colapsan a "" — significa "raíz de la librería".
//   2. NO absoluto: rechaza "/foo" y "C:\foo".
//   3. NO traversal: rechaza cualquier segmento "..".
//   4. NO segmentos vacíos consecutivos ("foo//bar"); colapsa.
//   5. Cada segmento pasa por SanitizeFilename — mismo rigor que el
//      nombre del fichero (control chars, exotic, etc.). Si algún
//      segmento se sanitiza a "", rechaza el subpath entero (mejor
//      eso que perder silenciosamente carpetas que el usuario quiso
//      crear).
//   6. La forma canónica usa "/" como separador (path/filepath en
//      Windows usaría "\\"; aquí queremos consistencia portable).
//
// La salida nunca empieza ni acaba con "/" — esto facilita que el
// caller componga `<library_path> + "/" + subpath + "/" + filename`
// sin colisiones de separadores.
func SanitizeSubpath(raw string) (string, error) {
	// Normaliza backslashes a slashes para que el mismo subpath
	// funcione independientemente del cliente que lo envíe.
	normalised := strings.ReplaceAll(raw, "\\", "/")
	cleaned := strings.TrimSpace(normalised)

	if cleaned == "" || cleaned == "." || cleaned == "/" {
		return "", nil
	}

	// Path absoluto: rechazo explícito. filepath.IsAbs cubre "/foo"
	// en POSIX y "C:\foo" en Windows; nosotros normalizamos
	// backslashes así que el check posix es suficiente.
	if strings.HasPrefix(cleaned, "/") {
		return "", ErrSubpathInvalid
	}
	// Windows drive letter como "C:" o "C:\":
	if len(cleaned) >= 2 && cleaned[1] == ':' {
		return "", ErrSubpathInvalid
	}

	segments := strings.Split(cleaned, "/")
	canon := make([]string, 0, len(segments))
	for _, seg := range segments {
		if seg == "" {
			// Salta segmentos vacíos (foo//bar → foo/bar) — laxo
			// porque el cliente puede meter doble slash por error.
			continue
		}
		if seg == "." {
			continue
		}
		if seg == ".." {
			return "", ErrSubpathInvalid
		}
		// Cada segmento pasa el sanitizer del nombre. Si se rompe a
		// vacío (caso: usuario mete sólo emojis), rechaza entero.
		sane := SanitizeFilename(seg)
		if sane == "" {
			return "", ErrSubpathInvalid
		}
		canon = append(canon, sane)
	}

	if len(canon) == 0 {
		return "", nil
	}

	return strings.Join(canon, "/"), nil
}

// ResolveSubpath compone la ruta absoluta destino donde un upload
// con `subpath` aterriza dentro de `libraryRoot`. Aplica la
// validación de SanitizeSubpath internamente y verifica con
// filepath.Rel que el resultado vive efectivamente dentro de
// `libraryRoot` — defensa en profundidad contra symlinks o casos
// que SanitizeSubpath no haya cubierto.
//
// `libraryRoot` se asume absoluto (el caller lo lee de
// library.Paths[0]; ResolveFinalPath o el picker ya lo canonicalizan).
func ResolveSubpath(libraryRoot, subpath string) (string, error) {
	clean, err := SanitizeSubpath(subpath)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(libraryRoot)
	if err != nil {
		return "", err
	}
	// filepath.Join con "" devuelve `abs` tal cual — la raíz de la
	// librería; cualquier subpath se concatena con el separador
	// nativo de la plataforma.
	full := filepath.Join(abs, clean)
	rel, err := filepath.Rel(abs, full)
	if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
		return "", ErrSubpathInvalid
	}
	return full, nil
}
