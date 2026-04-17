package pathmap

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestStore_WriteReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	id := uuid.NewString()
	target := filepath.Join(dir, "item", "poster.jpg")

	if err := s.Write(id, target); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := s.Read(id)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != target {
		t.Fatalf("got %q want %q", got, target)
	}
}

func TestStore_RemoveIdempotent(t *testing.T) {
	s := New(t.TempDir())
	id := uuid.NewString()

	// Remove without prior Write — should be a no-op, not an error.
	if err := s.Remove(id); err != nil {
		t.Fatalf("remove on missing: %v", err)
	}

	// Write then Remove then Read → ErrNotFound.
	if err := s.Write(id, "/tmp/x"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := s.Remove(id); err != nil {
		t.Fatalf("remove after write: %v", err)
	}
	if _, err := s.Read(id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("read after remove: want ErrNotFound, got %v", err)
	}
	if _, err := s.Read(id); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ErrNotFound should wrap fs.ErrNotExist, got %v", err)
	}
}

func TestStore_Read_Missing_ReturnsErrNotFound(t *testing.T) {
	s := New(t.TempDir())
	_, err := s.Read(uuid.NewString())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestStore_InvalidID_Rejected(t *testing.T) {
	s := New(t.TempDir())

	invalid := []string{
		"",
		"not-a-uuid",
		"../etc/passwd",
		"..",
		"/absolute/path",
		"foo/bar",
	}
	for _, id := range invalid {
		if err := s.Write(id, "/tmp/x"); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Write(%q): want ErrInvalidID, got %v", id, err)
		}
		if _, err := s.Read(id); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Read(%q): want ErrInvalidID, got %v", id, err)
		}
		if err := s.Remove(id); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Remove(%q): want ErrInvalidID, got %v", id, err)
		}
	}
}

func TestStore_InvalidID_NoFilesystemSideEffects(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	// Rejected write must NOT create the .mappings/ directory.
	_ = s.Write("../escape", "/tmp/x")
	if _, err := os.Stat(filepath.Join(dir, ".mappings")); !os.IsNotExist(err) {
		t.Fatalf(".mappings/ created for invalid id: %v", err)
	}
}
