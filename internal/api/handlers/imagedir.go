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
// sola NO sigue symlinks: un symlink `imageDir/poster.jpg` apuntando
// a `/etc/passwd` queda "bajo" imageDir tras la normalización
// textual, pero el target real está fuera. ADR-021 obliga a
// `filepath.EvalSymlinks` antes de `filepath.Rel`.
//
// Para paths destino que aún no existen, resolvemos el directorio
// padre (que el caller acaba de crear / piensa crear) y
// reconstruimos el path final pegando el `filepath.Base`. Esto
// detecta un symlink en cualquier componente intermedio sin exigir
// que el fichero ya exista.
//
// Compartido entre PeopleHandler.isUnderImageDir e
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
// sobre el path completo rechazaría destinos legítimos. Trepar
// hasta el primer componente existente conserva la garantía: si
// algún ancestro es un symlink que apunta fuera, se detecta; si
// todos los componentes textuales caen bajo imageDir, el resultado
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
