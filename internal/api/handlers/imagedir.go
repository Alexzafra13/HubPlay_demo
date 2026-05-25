package handlers

import (
	"path/filepath"
	"strings"
)

// isPathUnderImageDir comprueba si p apunta a una ubicación dentro
// de imageDir, resolviendo cualquier symlink en la cadena antes de
// la comparación final. Sirve tanto para paths EXISTENTES (servir un
// fichero) como paths DESTINO (escribir un thumbnail que todavía no
// existe).
//
// La validación textual con `filepath.Clean` + `filepath.Abs` por sí
// ImageHandler.isUnderImageDir; antes vivía duplicado en ambos.
func isPathUnderImageDir(imageDir, p string) bool {
	rootAbs, err := filepath.Abs(imageDir)
	if err != nil {
		return false
	}
	rootResolved, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return false
	}

	pAbs, err := filepath.Abs(filepath.Clean(p))
	if err != nil {
		return false
	}
	pResolved, ok := resolveExistingPrefix(pAbs)
	if !ok {
		return false
	}

	rel, err := filepath.Rel(rootResolved, pResolved)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return !strings.HasPrefix(rel, "..")
}

// resolveExistingPrefix camina hacia arriba en la jerarquía hasta
// encontrar el primer componente que sí existe en el filesystem y
// lo resuelve con EvalSymlinks. El resto del path (la parte aún no
// creada) se pega tal cual al resultado.
//
// Por qué: durante la generación de un thumbnail, ni el fichero ni
// su directorio inmediato existen todavía. Forzar EvalSymlinks
// también.
func resolveExistingPrefix(abs string) (string, bool) {
	walk := abs
	suffix := ""
	for {
		resolved, err := filepath.EvalSymlinks(walk)
		if err == nil {
			if suffix == "" {
				return resolved, true
			}
			return filepath.Join(resolved, suffix), true
		}
		parent := filepath.Dir(walk)
		if parent == walk {
			return "", false
		}
		suffix = filepath.Join(filepath.Base(walk), suffix)
		walk = parent
	}
}
