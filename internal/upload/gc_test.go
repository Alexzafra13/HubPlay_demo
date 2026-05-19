package upload_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hubplay/internal/upload"
)

// makeStagedFile crea un fichero en <staging>/<user>/<upload>/<name>
// con un mtime concreto. Devuelve la ruta del dir del upload.
func makeStagedFile(t *testing.T, root, user, uploadID, name string, mtime time.Time) string {
	t.Helper()
	dir := filepath.Join(root, user, uploadID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("data"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newGC(t *testing.T, staleAfter time.Duration) (*upload.GC, *upload.StagingDir) {
	t.Helper()
	staging, err := upload.NewStagingDir(filepath.Join(t.TempDir(), "staging"))
	if err != nil {
		t.Fatal(err)
	}
	gc := upload.NewGC(staging, time.Hour, staleAfter, slog.Default())
	return gc, staging
}

// TestGC_RemovesOldUploadDir es el happy path: un dir con todos sus
// ficheros más antiguos que staleAfter se borra.
func TestGC_RemovesOldUploadDir(t *testing.T) {
	gc, staging := newGC(t, 24*time.Hour)
	old := time.Now().Add(-48 * time.Hour)
	dir := makeStagedFile(t, staging.Root(), "u-alex", "up-1", "movie.mkv", old)

	upload.RunSweepForTests(gc, context.Background())

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("stale upload dir not removed: %v", err)
	}
}

// TestGC_KeepsActiveUpload pin la defensa "upload con un fichero
// reciente sobrevive entero" — incluso si los otros ficheros son
// viejos. tus.PATCH actualiza el modtime del blob; el .info puede
// quedarse atrás. Si SOLO el blob está fresco, el dir se preserva.
func TestGC_KeepsActiveUpload(t *testing.T) {
	gc, staging := newGC(t, 24*time.Hour)
	old := time.Now().Add(-48 * time.Hour)
	fresh := time.Now().Add(-1 * time.Minute)

	// Mismo dir, dos ficheros: uno viejo (.info), uno fresco (blob).
	dir := makeStagedFile(t, staging.Root(), "u-alex", "up-1", "up-1.info", old)
	_ = makeStagedFile(t, staging.Root(), "u-alex", "up-1", "movie.mkv", fresh)

	upload.RunSweepForTests(gc, context.Background())

	if _, err := os.Stat(dir); err != nil {
		t.Errorf("active upload dir removed: %v", err)
	}
}

// TestGC_CleansEmptyUserDir — un dir de usuario que queda vacío tras
// borrar todos sus uploads también se elimina.
func TestGC_CleansEmptyUserDir(t *testing.T) {
	gc, staging := newGC(t, 24*time.Hour)
	old := time.Now().Add(-48 * time.Hour)
	makeStagedFile(t, staging.Root(), "u-orphan", "up-1", "movie.mkv", old)

	upload.RunSweepForTests(gc, context.Background())

	userDir := filepath.Join(staging.Root(), "u-orphan")
	if _, err := os.Stat(userDir); !os.IsNotExist(err) {
		t.Errorf("empty user dir not cleaned: %v", err)
	}
}

// TestGC_LeavesMultipleUploadsIndependent — un usuario con uno fresco
// y uno viejo se queda con el fresco; el user dir sobrevive.
func TestGC_LeavesMultipleUploadsIndependent(t *testing.T) {
	gc, staging := newGC(t, 24*time.Hour)
	old := time.Now().Add(-48 * time.Hour)
	fresh := time.Now()

	makeStagedFile(t, staging.Root(), "u-alex", "old-upload", "movie.mkv", old)
	freshDir := makeStagedFile(t, staging.Root(), "u-alex", "fresh-upload", "movie.mkv", fresh)

	upload.RunSweepForTests(gc, context.Background())

	if _, err := os.Stat(filepath.Join(staging.Root(), "u-alex", "old-upload")); !os.IsNotExist(err) {
		t.Error("old upload survived")
	}
	if _, err := os.Stat(freshDir); err != nil {
		t.Errorf("fresh upload removed: %v", err)
	}
}

// TestGC_NoopOnEmptyStaging — sin uploads, sweep no falla y no toca
// nada.
func TestGC_NoopOnEmptyStaging(t *testing.T) {
	gc, _ := newGC(t, 24*time.Hour)
	upload.RunSweepForTests(gc, context.Background())
}

// TestGC_DoesNotDescendThirdLevel — un fichero dejado por error en
// <staging>/<user>/orphan.mkv (sin sub-dir de upload) NO se toca.
// Defensa contra borrar cosas que no son uploads bien formados.
func TestGC_DoesNotDescendThirdLevel(t *testing.T) {
	gc, staging := newGC(t, 24*time.Hour)
	old := time.Now().Add(-48 * time.Hour)

	// Fichero suelto a NIVEL DE USUARIO (no dentro de un upload dir).
	loose := filepath.Join(staging.Root(), "u-weird", "loose.txt")
	_ = os.MkdirAll(filepath.Dir(loose), 0o750)
	_ = os.WriteFile(loose, []byte("x"), 0o640)
	_ = os.Chtimes(loose, old, old)

	upload.RunSweepForTests(gc, context.Background())

	// loose.txt sobrevive porque no es un dir (el GC sólo borra dirs
	// de upload, no ficheros en el primer nivel).
	if _, err := os.Stat(loose); err != nil {
		t.Errorf("loose file unexpectedly removed: %v", err)
	}
}
