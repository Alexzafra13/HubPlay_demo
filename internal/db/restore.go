package db

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// PendingRestoreFilename is the well-known sibling of the live DB
// where the admin "Restaurar copia" upload lands. Detected at boot
// (before sql.Open) and swapped into place if present. A separate
// file (rather than overwriting hubplay.db live) is the only
// consistent way to do an offline restore on a running SQLite — we
// can't replace a file that's currently open + WAL-checkpointed.
const PendingRestoreFilename = ".pending-restore.db"

// sqliteMagic is the 16-byte header every valid SQLite database
// starts with. We sanity-check uploaded restore files against this
// before accepting them so a stray .json doesn't get installed
// as the next live DB.
var sqliteMagic = []byte("SQLite format 3\x00")

// ApplyPendingRestoreIfAny swaps in a pending-restore file when one
// is sitting next to the live DB. Called from the binary's startup
// sequence BEFORE sql.Open so the live connection points at the
// restored bytes. The previous DB is renamed to a timestamped
// backup file in the same directory rather than deleted — if the
// restore was a mistake, the operator can swap back manually
// without losing data.
//
// No-op (and no error) when no pending file exists; that's the
// happy path on every boot that didn't follow an upload.
func ApplyPendingRestoreIfAny(livePath string, logger *slog.Logger) error {
	dir := filepath.Dir(livePath)
	pending := filepath.Join(dir, PendingRestoreFilename)

	info, err := os.Stat(pending)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat pending restore: %w", err)
	}
	if info.Size() < int64(len(sqliteMagic)) {
		// Truncated upload; refuse to swap. Keep the file around so
		// the operator can inspect what happened — we'd rather log
		// loudly than silently drop their attempted restore.
		logger.Error("pending restore file is too small to be a SQLite DB; refusing swap",
			"path", pending, "size", info.Size())
		return fmt.Errorf("pending restore: file too small (%d bytes)", info.Size())
	}

	if err := verifySQLiteHeader(pending); err != nil {
		logger.Error("pending restore file has invalid SQLite header; refusing swap",
			"path", pending, "error", err)
		return fmt.Errorf("pending restore: %w", err)
	}

	// Move the current live DB out of the way (if it exists) before
	// the swap. Timestamped name so two consecutive failed restores
	// don't overwrite the original. We also move WAL / SHM siblings
	// when present — leaving them around would have SQLite re-apply
	// the old WAL onto the freshly-restored bytes on first open.
	stamp := time.Now().UTC().Format("20060102-150405")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		src := livePath + suffix
		if _, err := os.Stat(src); err != nil {
			continue
		}
		dst := fmt.Sprintf("%s.bak-%s%s", livePath, stamp, suffix)
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("backing up live DB %s: %w", src, err)
		}
		logger.Info("moved previous DB aside before restore",
			"from", src, "to", dst)
	}

	// Atomic on the same filesystem (the conventional placement is
	// `<datadir>/hubplay.db` and `<datadir>/.pending-restore.db`,
	// guaranteed same FS).
	if err := os.Rename(pending, livePath); err != nil {
		return fmt.Errorf("install pending restore: %w", err)
	}
	logger.Info("pending DB restore applied",
		"target", livePath, "size_bytes", info.Size())
	return nil
}

// verifySQLiteHeader reads the first 16 bytes and compares to the
// canonical magic. Cheap sanity check; doesn't validate that every
// page is intact — that's SQLite's job once we open the file. The
// goal here is just to reject obvious garbage (text files, archives,
// truncated downloads).
func verifySQLiteHeader(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, len(sqliteMagic))
	if _, err := io.ReadFull(f, buf); err != nil {
		return fmt.Errorf("read header: %w", err)
	}
	for i, b := range sqliteMagic {
		if buf[i] != b {
			return fmt.Errorf("not a SQLite database (bad magic at byte %d)", i)
		}
	}
	return nil
}
