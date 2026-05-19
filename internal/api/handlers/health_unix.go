//go:build linux || darwin || freebsd || netbsd || openbsd

package handlers

import (
	"path/filepath"
	"syscall"
)

// freeDiskBytes (Unix) — usa Statfs sobre el directorio padre del
// path indicado. Bavail es "blocks available to non-root" — lo
// correcto para reportar al operador, que normalmente corre el
// binario como usuario no-root. Bfree incluiría la reserva root y
// daría una cifra demasiado optimista.
func freeDiskBytes(path string) (uint64, error) {
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}
