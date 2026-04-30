package db_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"hubplay/internal/db"
)

// TestOpen_AppliesPragmas pins the configuration contract: every
// SQLite handle HubPlay opens MUST come back with the production
// PRAGMAs applied. This test catches a future edit that drops one
// (e.g. someone refactors the DSN builder and forgets `mmap_size`).
//
// We don't pin the exact numeric values — those can move based on
// hardware profiling. We pin the SHAPE: every category we depend on
// is set to a non-default value.
func TestOpen_AppliesPragmas(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "pragma-test.db")

	silent := slog.New(slog.NewTextHandler(io.Discard, nil))
	database, err := db.Open("sqlite", dbPath, silent)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	tests := []struct {
		pragma string
		check  func(string) bool
		want   string
	}{
		// WAL gives multiple-readers + one-writer concurrency. Default
		// is "delete" (rollback journal) which serialises everything.
		{"journal_mode", isExactly("wal"), "wal"},
		// FK enforcement. Default 0 (off) which silently ignores ON DELETE
		// CASCADE — we have several. Must be on.
		{"foreign_keys", isExactly("1"), "1"},
		// Synchronous mode. NORMAL is 1, FULL is 2. Pinning at NORMAL
		// because that's the WAL recommendation.
		{"synchronous", isExactly("1"), "1 (NORMAL)"},
		// Cache size as a NEGATIVE number = KiB; we set -65536 (64 MiB).
		// Default would be a positive page-count value.
		{"cache_size", func(v string) bool { return strings.HasPrefix(v, "-") }, "negative (KiB)"},
		// Temp store: 0=DEFAULT, 1=FILE, 2=MEMORY. We want 2.
		{"temp_store", isExactly("2"), "2 (MEMORY)"},
		// mmap_size: any non-zero value means memory-mapped I/O is on.
		{"mmap_size", func(v string) bool { return v != "0" }, "non-zero"},
		// Busy timeout in ms. Default 0 (immediate fail). We pin 5000.
		{"busy_timeout", isExactly("5000"), "5000"},
	}

	for _, tt := range tests {
		var got string
		err := database.QueryRow("PRAGMA " + tt.pragma).Scan(&got)
		if err != nil {
			t.Errorf("%s: query failed: %v", tt.pragma, err)
			continue
		}
		if !tt.check(got) {
			t.Errorf("%s: got %q, want %s", tt.pragma, got, tt.want)
		}
	}
}

// TestOptimize_DoesNotPanicOnFreshDB pins the contract that a freshly-
// opened DB with no tables is a valid input for Optimize. Real
// production runs always have tables (after migrations) but this
// guards against a regression where Optimize panics on edge inputs.
func TestOptimize_DoesNotPanicOnFreshDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "optimize-test.db")

	silent := slog.New(slog.NewTextHandler(io.Discard, nil))
	database, err := db.Open("sqlite", dbPath, silent)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	// Should be a no-op on an empty DB. The test passes if the call
	// returns without panicking and without printing an error to a
	// logger that's wired to t.Error (we use io.Discard above).
	db.Optimize(context.Background(), database, silent)
}

func isExactly(want string) func(string) bool {
	return func(got string) bool { return got == want }
}
