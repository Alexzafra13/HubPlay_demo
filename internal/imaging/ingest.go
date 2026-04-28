package imaging

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// IngestedImage describes an image that has been downloaded, validated,
// and atomically written to local storage. The caller turns this struct
// into a `db.Image` (or any other persistence layer) — keeping that
// mapping outside `imaging` keeps this package free of database imports
// and reusable from both the scanner and the refresher without an
// import cycle.
type IngestedImage struct {
	// Absolute path on disk under the configured image root.
	LocalPath string
	// Filename only (no directory) — handy for path-mapping tables that
	// don't want to store the full path twice.
	Filename string
	// Detected by SafeGet's content-type sniff. Useful for the served
	// MIME header later and for picking an extension.
	ContentType string
	// May be empty when the format isn't decodable as PNG/JPEG (the
	// blurhash library only handles those today). Callers should treat
	// "" as "no preview placeholder available", not as an error.
	Blurhash string
	// Hex-encoded SHA-256 of the bytes that landed on disk. The first
	// 16 chars are used in the filename to keep distinct content under
	// the same kind from colliding; the full hash is exposed so future
	// dedup work has a stable key without re-hashing.
	SHA256 string
	// Pre-computed dominant + dark-muted colours formatted as CSS
	// rgb() strings. Empty when extraction failed or the format wasn't
	// decodable — mirrors the Blurhash field's "" sentinel so callers
	// don't need a separate present/absent check.
	DominantColor      string
	DominantColorMuted string
}

// IngestRemoteImage fetches `url`, runs it through the same SSRF +
// pixel-bomb guards used by uploads (SafeGet + EnforceMaxPixels),
// computes a blurhash placeholder, and writes the bytes into
// `<dir>/<kind>_<hash16>.<ext>` atomically.
//
// "Atomically" means: a server crash, disk-full, or context cancellation
// mid-download can never leave a half-written file at the destination.
// The bytes go to `dst+".tmp"` first and are then renamed; rename(2) is
// atomic on POSIX, so a concurrent reader either sees no file or the
// fully-written one.
//
// Errors returned wrap the failing stage so the caller can log a useful
// message without re-deriving cause:
//
//	download:           SafeGet failed (network, content-type, size)
//	validate dimensions: EnforceMaxPixels rejected the image
//	create dir / write file: filesystem refused the operation
func IngestRemoteImage(dir, kind, url string, logger *slog.Logger) (*IngestedImage, error) {
	data, contentType, err := SafeGet(url, MaxUploadBytes, 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	if err := EnforceMaxPixels(data); err != nil {
		return nil, fmt.Errorf("validate dimensions: %w", err)
	}

	sum := sha256.Sum256(data)
	hashHex := hex.EncodeToString(sum[:])
	filename := fmt.Sprintf("%s_%s%s", kind, hashHex[:16], ExtensionForContentType(contentType))

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create dir: %w", err)
	}
	fullPath := filepath.Join(dir, filename)
	if err := AtomicWriteFile(fullPath, data, 0o644); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	vibrant, muted := ExtractDominantColors(data, logger)
	return &IngestedImage{
		LocalPath:          fullPath,
		Filename:           filename,
		ContentType:        contentType,
		Blurhash:           ComputeBlurhash(data, logger),
		SHA256:             hashHex,
		DominantColor:      vibrant,
		DominantColorMuted: muted,
	}, nil
}

// AtomicWriteFile writes `data` to `dst` so that a partial or failed
// write never leaves a corrupt file in place. It writes to `dst+".tmp"`
// first (with the requested perm), then renames over `dst`. Cleanup of
// the .tmp on rename failure is best-effort; an OS that can't rename a
// freshly-written file usually can't unlink either, but trying costs
// nothing and avoids leaking under transient errors.
func AtomicWriteFile(dst string, data []byte, perm os.FileMode) error {
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
