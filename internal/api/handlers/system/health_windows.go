//go:build windows

package system

import (
	"path/filepath"
	"syscall"
	"unsafe"
)

// freeDiskBytes (Windows) — usa GetDiskFreeSpaceExW del kernel32.
// Devuelve los bytes disponibles para el caller (no la cuota total
// del filesystem), que es la semántica que el handler espera.
//
// Llamada manual con syscall en vez de golang.org/x/sys/windows para
// no añadir una dep transitiva sólo para una sola función. Acepta
// path absoluto o relativo; convierte a UTF-16 antes de llamar.
func freeDiskBytes(path string) (uint64, error) {
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	utf16, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}

	kernel32, err := syscall.LoadLibrary("kernel32.dll")
	if err != nil {
		return 0, err
	}
	defer syscall.FreeLibrary(kernel32) //nolint:errcheck

	proc, err := syscall.GetProcAddress(kernel32, "GetDiskFreeSpaceExW")
	if err != nil {
		return 0, err
	}

	var freeAvailable, total, freeTotal uint64
	// GetDiskFreeSpaceExW(LPCWSTR, PULARGE_INTEGER, PULARGE_INTEGER, PULARGE_INTEGER)
	r1, _, e1 := syscall.SyscallN(proc,
		uintptr(unsafe.Pointer(utf16)),
		uintptr(unsafe.Pointer(&freeAvailable)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&freeTotal)),
	)
	if r1 == 0 {
		return 0, e1
	}
	return freeAvailable, nil
}
