// Package pathmap persists the mapping from an image ID to its on-disk path.
//
// The upload handler stores each uploaded/downloaded image under
// <imageDir>/<itemID>/<filename> but the public URL is keyed by the image's
// UUID. Since filenames are content-addressed (and can't be derived back from
// the ID alone), we write one tiny file per image under a .mappings/ directory
// that contains just the absolute on-disk path. Readers load the mapping, then
// stream the referenced file.
//
// Returning errors — rather than silently swallowing them — is the intentional
// change vs. the previous ad-hoc helpers in handlers/image.go: callers can now
// log a warning without hiding real failures (full disk, permission denied).
package pathmap

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// ErrInvalidID is returned when an ID fails UUID validation. The store never
// touches the filesystem for invalid IDs, so callers can use this as a
// boundary check against path-traversal attempts like "../etc/passwd".
var ErrInvalidID = errors.New("pathmap: invalid id")

// ErrNotFound is returned by Read when no mapping exists for the given ID.
// It wraps os.ErrNotExist so callers can test with errors.Is(err, fs.ErrNotExist).
var ErrNotFound = fmt.Errorf("pathmap: mapping not found: %w", os.ErrNotExist)

// Store persists image-id → on-disk-path mappings under a single directory.
// It is safe for concurrent use — all operations are backed by plain
// filesystem calls with no shared in-memory state.
type Store struct {
	dir string // directory that contains one file per mapping
}

// New returns a Store that writes mappings under "<parent>/.mappings/".
// The directory is created lazily on the first Write call.
func New(parent string) *Store {
	return &Store{dir: filepath.Join(parent, ".mappings")}
}

// Write records that imageID resolves to localPath. The directory is created
// on demand. Returns ErrInvalidID for non-UUID imageIDs.
func (s *Store) Write(imageID, localPath string) error {
	if err := validID(imageID); err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("pathmap: create dir: %w", err)
	}
	target := filepath.Join(s.dir, imageID)
	if err := os.WriteFile(target, []byte(localPath), 0o644); err != nil {
		return fmt.Errorf("pathmap: write: %w", err)
	}
	return nil
}

// Read returns the on-disk path stored for imageID, or ErrNotFound when no
// mapping exists. Invalid (non-UUID) IDs return ErrInvalidID without touching
// the filesystem.
func (s *Store) Read(imageID string) (string, error) {
	if err := validID(imageID); err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(s.dir, imageID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("pathmap: read: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// Remove deletes the mapping for imageID. Missing mappings are not an error
// (idempotent). Invalid IDs return ErrInvalidID.
func (s *Store) Remove(imageID string) error {
	if err := validID(imageID); err != nil {
		return err
	}
	err := os.Remove(filepath.Join(s.dir, imageID))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("pathmap: remove: %w", err)
	}
	return nil
}

// validID rejects anything that doesn't parse as a UUID. This is the
// fundamental defense against path-traversal via the {imageId} URL parameter:
// "../foo" / "abs/olu/te" / "" all fail here before any os.* call.
func validID(id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return ErrInvalidID
	}
	return nil
}
