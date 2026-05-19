package upload

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newStaging(t *testing.T) *StagingDir {
	t.Helper()
	root := t.TempDir()
	s, err := NewStagingDir(filepath.Join(root, "staging"))
	if err != nil {
		t.Fatalf("NewStagingDir: %v", err)
	}
	return s
}

func TestNewStagingDir_RejectsEmpty(t *testing.T) {
	if _, err := NewStagingDir(""); err == nil {
		t.Error("want error on empty root")
	}
}

func TestStagingDir_UploadDir(t *testing.T) {
	s := newStaging(t)
	dir, err := s.UploadDir("u-alex", "upload-abc123")
	if err != nil {
		t.Fatalf("UploadDir: %v", err)
	}
	if !strings.HasPrefix(dir, s.Root()) {
		t.Errorf("dir %q not under root %q", dir, s.Root())
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Errorf("dir not created: %v", err)
	}
}

func TestStagingDir_UploadDir_RejectsUnsafeID(t *testing.T) {
	s := newStaging(t)
	cases := []struct {
		userID, uploadID string
	}{
		{"../evil", "ok"},
		{"ok", "../../etc"},
		{"", "ok"},
		{"ok", ""},
		{"with/slash", "ok"},
		{"ok", "with space"},
		{"ok", "with;semicolon"},
	}
	for _, tc := range cases {
		_, err := s.UploadDir(tc.userID, tc.uploadID)
		if !errors.Is(err, ErrUnsafePath) {
			t.Errorf("UploadDir(%q,%q) = %v, want ErrUnsafePath", tc.userID, tc.uploadID, err)
		}
	}
}

func TestStagingDir_ResolveFinalPath(t *testing.T) {
	s := newStaging(t)
	full, err := s.ResolveFinalPath("u-alex", "up-1", "movie.mkv")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := filepath.Join(s.Root(), "u-alex", "up-1", "movie.mkv")
	if full != want {
		t.Errorf("got %q, want %q", full, want)
	}
}

func TestStagingDir_RemoveUpload(t *testing.T) {
	s := newStaging(t)
	dir, _ := s.UploadDir("u-alex", "up-1")
	// Write a sentinel inside.
	sentinel := filepath.Join(dir, "blob")
	if err := os.WriteFile(sentinel, []byte("data"), 0o640); err != nil {
		t.Fatalf("write blob: %v", err)
	}
	if err := s.RemoveUpload("u-alex", "up-1"); err != nil {
		t.Fatalf("RemoveUpload: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("dir survived removal: %v", err)
	}
}

// TestStagingDir_MoveTo_HappyPath: rename atómico same-filesystem.
func TestStagingDir_MoveTo_HappyPath(t *testing.T) {
	s := newStaging(t)
	dir, _ := s.UploadDir("u-alex", "up-1")
	src := filepath.Join(dir, "movie.mkv")
	if err := os.WriteFile(src, []byte("test contents"), 0o640); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "library", "movies", "movie.mkv")
	if err := s.MoveTo(src, dst); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source still exists")
	}
	data, err := os.ReadFile(dst)
	if err != nil || string(data) != "test contents" {
		t.Errorf("destination missing/corrupt: %v %q", err, data)
	}
}

// TestStagingDir_MoveTo_TargetExists pin la decisión: no se pisa nada.
func TestStagingDir_MoveTo_TargetExists(t *testing.T) {
	s := newStaging(t)
	dir, _ := s.UploadDir("u-alex", "up-1")
	src := filepath.Join(dir, "movie.mkv")
	_ = os.WriteFile(src, []byte("new"), 0o640)
	dst := filepath.Join(t.TempDir(), "movie.mkv")
	_ = os.WriteFile(dst, []byte("existing"), 0o640)

	err := s.MoveTo(src, dst)
	if !errors.Is(err, ErrTargetExists) {
		t.Errorf("want ErrTargetExists, got %v", err)
	}
	// Source NO debe haberse borrado tras el rechazo — el caller puede
	// reintentar con sufijo.
	if _, err := os.Stat(src); err != nil {
		t.Errorf("source vanished on rejected move: %v", err)
	}
}

func TestStagingDir_MoveTo_RejectsTargetInsideStaging(t *testing.T) {
	s := newStaging(t)
	dir, _ := s.UploadDir("u-alex", "up-1")
	src := filepath.Join(dir, "movie.mkv")
	_ = os.WriteFile(src, []byte("x"), 0o640)

	dst := filepath.Join(s.Root(), "u-alex", "evil.mkv")
	if err := s.MoveTo(src, dst); err == nil {
		t.Error("MoveTo accepted target inside staging")
	}
}

func TestStagingDir_MoveTo_RejectsSourceOutsideStaging(t *testing.T) {
	s := newStaging(t)
	src := filepath.Join(t.TempDir(), "outside.mkv")
	_ = os.WriteFile(src, []byte("x"), 0o640)
	dst := filepath.Join(t.TempDir(), "library", "outside.mkv")

	if err := s.MoveTo(src, dst); !errors.Is(err, ErrUnsafePath) {
		t.Errorf("want ErrUnsafePath, got %v", err)
	}
}

func TestRandomID_Hex32(t *testing.T) {
	id := RandomID()
	if len(id) != 32 {
		t.Errorf("len = %d, want 32", len(id))
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("non-hex char %q in id", c)
		}
	}
}

func TestRandomID_Unique(t *testing.T) {
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		id := RandomID()
		if seen[id] {
			t.Fatalf("collision after %d iterations", i)
		}
		seen[id] = true
	}
}
