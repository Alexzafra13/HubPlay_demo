package handlers

import (
	"os"
	"path/filepath"
	"testing"
)

// TestIsPathUnderImageDir_Symlink cubre el vector path-traversal que
// motiva ADR-021: un fichero dentro de imageDir es realmente un
// symlink que apunta fuera. La validación textual con
// filepath.Clean + filepath.Abs aceptaba el path porque no resolvía
// symlinks; ahora EvalSymlinks lo detecta y devuelve false.
func TestIsPathUnderImageDir_Symlink(t *testing.T) {
	t.Parallel()
	imageDir := t.TempDir()
	outside := t.TempDir()

	// Fichero secreto fuera de imageDir.
	secret := filepath.Join(outside, "passwd")
	if err := os.WriteFile(secret, []byte("LEAKED_SECRET"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	// Symlink dentro de imageDir que apunta a un fichero externo.
	link := filepath.Join(imageDir, "poster.jpg")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}

	if isPathUnderImageDir(imageDir, link) {
		t.Fatal("symlink que apunta fuera de imageDir fue aceptado (CVE: path traversal)")
	}
}

// TestIsPathUnderImageDir_HappyPath verifica que un fichero normal
// dentro de imageDir sí pasa la validación.
func TestIsPathUnderImageDir_HappyPath(t *testing.T) {
	t.Parallel()
	imageDir := t.TempDir()
	normal := filepath.Join(imageDir, "poster.jpg")
	if err := os.WriteFile(normal, []byte("data"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if !isPathUnderImageDir(imageDir, normal) {
		t.Fatal("fichero normal dentro de imageDir fue rechazado")
	}
}

// TestIsPathUnderImageDir_TraversalLiteral cubre el caso clásico
// "../etc/passwd" sin symlink — la validación textual ya lo cubría;
// confirmamos que el refactor de ADR-021 no regresiona.
func TestIsPathUnderImageDir_TraversalLiteral(t *testing.T) {
	t.Parallel()
	imageDir := t.TempDir()
	outside := filepath.Join(imageDir, "..", "outside.txt")

	if isPathUnderImageDir(imageDir, outside) {
		t.Fatal("path con .. literal fue aceptado")
	}
}

// TestIsPathUnderImageDir_NonExistentTargetUnderRoot acepta un path
// de DESTINO (fichero aún no creado) cuando el directorio padre
// existe y está bajo imageDir. Cubre el caso de thumbnails antes de
// generarlos.
func TestIsPathUnderImageDir_NonExistentTargetUnderRoot(t *testing.T) {
	t.Parallel()
	imageDir := t.TempDir()
	subDir := filepath.Join(imageDir, ".thumbnails")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	target := filepath.Join(subDir, "abc_w100.jpg") // aún no creado

	if !isPathUnderImageDir(imageDir, target) {
		t.Fatal("destino válido (parent existe, bajo imageDir) fue rechazado")
	}
}

// TestIsPathUnderImageDir_NonExistentParentUnderRootAccepted confirma
// el caso "destino con varios niveles aún no creados, todos
// textualmente bajo imageDir": el handler de imágenes hace este
// patrón al solicitar un thumbnail antes de MkdirAll. Mientras los
// componentes no existan no pueden ser symlinks, así que es seguro
// aceptarlo. Si en el futuro alguien crea `no-such-dir` como
// symlink fuera, EvalSymlinks lo detectará en la siguiente
// invocación.
func TestIsPathUnderImageDir_NonExistentParentUnderRootAccepted(t *testing.T) {
	t.Parallel()
	imageDir := t.TempDir()
	missing := filepath.Join(imageDir, "no-such-dir", "file.jpg")

	if !isPathUnderImageDir(imageDir, missing) {
		t.Fatal("destino con parent inexistente bajo imageDir fue rechazado (regresión del thumbnail flow)")
	}
}

// TestIsPathUnderImageDir_NonExistentOutsideRoot rechaza un path que
// textualmente vive fuera de imageDir, aunque el path entero sea
// inexistente.
func TestIsPathUnderImageDir_NonExistentOutsideRoot(t *testing.T) {
	t.Parallel()
	imageDir := t.TempDir()
	outside := filepath.Join(filepath.Dir(imageDir), "elsewhere", "file.jpg")

	if isPathUnderImageDir(imageDir, outside) {
		t.Fatal("path textualmente fuera de imageDir fue aceptado")
	}
}

// TestIsPathUnderImageDir_SymlinkInParentChain detecta el caso más
// sutil: el fichero final no existe pero un directorio intermedio
// es un symlink que apunta fuera de imageDir.
func TestIsPathUnderImageDir_SymlinkInParentChain(t *testing.T) {
	t.Parallel()
	imageDir := t.TempDir()
	outside := t.TempDir()

	// Crea un symlink imageDir/escape → outside.
	linkDir := filepath.Join(imageDir, "escape")
	if err := os.Symlink(outside, linkDir); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	// El path destino atraviesa el symlink; el fichero final no
	// existe (es un destino), pero el componente "escape" sí.
	target := filepath.Join(linkDir, "exfil.jpg")

	if isPathUnderImageDir(imageDir, target) {
		t.Fatal("symlink intermedio que apunta fuera fue aceptado (CVE: traversal por componente)")
	}
}
