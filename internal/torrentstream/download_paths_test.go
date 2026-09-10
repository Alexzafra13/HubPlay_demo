package torrentstream

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCopyFiles_RejectsEscapingPaths: las rutas relativas vienen del
// metainfo del torrent; un ".." no puede escribir fuera de destDir ni
// leer fuera del scratch.
func TestCopyFiles_RejectsEscapingPaths(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "ok.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"../escape.mkv",
		filepath.Join("..", "..", "escape.mkv"),
		".",
		"",
	} {
		if err := copyFiles(src, []string{rel}, dst); err == nil {
			t.Errorf("copyFiles(%q) should fail", rel)
		}
	}
	// Nada se ha escrito fuera del destino.
	if _, err := os.Stat(filepath.Join(filepath.Dir(dst), "escape.mkv")); err == nil {
		t.Error("escaping file must not be written next to destDir")
	}
	// Y una ruta benigna sigue funcionando.
	if err := copyFiles(src, []string{"ok.mkv"}, dst); err != nil {
		t.Fatalf("benign copy: %v", err)
	}
}

func TestContainedPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	if _, err := containedPath(root, "a/b.mkv"); err != nil {
		t.Errorf("nested path should be contained: %v", err)
	}
	if _, err := containedPath(root, "../x"); err == nil {
		t.Error("parent escape should be rejected")
	}
	if _, err := containedPath(root, ""); err == nil {
		t.Error("empty rel resolves to root itself and must be rejected")
	}
}
