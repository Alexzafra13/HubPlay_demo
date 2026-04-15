package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// stubPathWith prepares a synthetic PATH entry containing fake ffmpeg and
// ffprobe executables so Preflight's exec.LookPath succeeds without relying
// on whatever is installed on the test machine. Returns the PATH value to
// feed to t.Setenv.
func stubPathWith(t *testing.T, bins ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range bins {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("write fake %s: %v", name, err)
		}
	}
	return dir
}

// baseTestConfig builds a Config that points at two fresh temp dirs (for
// DB and cache) so individual tests only have to tweak the one knob they
// care about.
func baseTestConfig(t *testing.T) *Config {
	t.Helper()
	dbDir := t.TempDir()
	return &Config{
		Database: DatabaseConfig{
			Driver: "sqlite",
			Path:   filepath.Join(dbDir, "hubplay.db"),
		},
		Streaming: StreamingConfig{
			CacheDir: t.TempDir(),
		},
	}
}

func TestPreflight_HappyPath(t *testing.T) {
	t.Setenv("PATH", stubPathWith(t, "ffmpeg", "ffprobe"))
	cfg := baseTestConfig(t)

	if err := cfg.Preflight(slog.Default()); err != nil {
		t.Errorf("happy path should not error: %v", err)
	}
}

func TestPreflight_MissingBinariesAreReported(t *testing.T) {
	// Empty PATH → neither ffmpeg nor ffprobe resolves. Both must be
	// reported in the same error so the operator sees the full list.
	t.Setenv("PATH", t.TempDir())
	cfg := baseTestConfig(t)

	err := cfg.Preflight(slog.Default())
	if err == nil {
		t.Fatal("expected error when binaries are missing")
	}
	msg := err.Error()
	if !strings.Contains(msg, "ffmpeg not found") {
		t.Errorf("error should mention ffmpeg: %q", msg)
	}
	if !strings.Contains(msg, "ffprobe not found") {
		t.Errorf("error should mention ffprobe: %q", msg)
	}
}

func TestPreflight_AggregatesAllErrors(t *testing.T) {
	// Empty PATH + cache dir pointing at a non-creatable path: every
	// check must still run and every failure must appear in the joined
	// error. Iterating boots while fixing one problem at a time is a
	// slow loop; this test locks in the "fail loud, fail complete"
	// invariant.
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based permission tricks don't translate to Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores POSIX write bits")
	}

	// Create a parent dir we can't write into, then point the cache dir
	// at a subpath underneath — MkdirAll will fail.
	readOnlyParent := t.TempDir()
	if err := os.Chmod(readOnlyParent, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnlyParent, 0o755) })

	cfg := baseTestConfig(t)
	cfg.Streaming.CacheDir = filepath.Join(readOnlyParent, "subdir")
	t.Setenv("PATH", t.TempDir()) // empty → ffmpeg/ffprobe missing

	err := cfg.Preflight(slog.Default())
	if err == nil {
		t.Fatal("expected aggregated error")
	}
	msg := err.Error()
	// Three distinct problems: missing ffmpeg, missing ffprobe, cache dir.
	for _, want := range []string{"ffmpeg", "ffprobe", "cache_dir"} {
		if !strings.Contains(msg, want) {
			t.Errorf("aggregated error missing %q:\n%s", want, msg)
		}
	}
}

func TestPreflight_DatabaseDirNotWritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based permission tricks don't translate to Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores POSIX write bits")
	}

	t.Setenv("PATH", stubPathWith(t, "ffmpeg", "ffprobe"))

	// Make the DB parent dir exist but read-only. A real bind-mount with
	// the wrong uid in Docker produces the same failure mode — this is
	// the regression we are guarding against.
	readOnly := t.TempDir()
	if err := os.Chmod(readOnly, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o755) })

	cfg := baseTestConfig(t)
	cfg.Database.Path = filepath.Join(readOnly, "hubplay.db")

	err := cfg.Preflight(slog.Default())
	if err == nil {
		t.Fatal("expected error for read-only db dir")
	}
	if !strings.Contains(err.Error(), "database.path") {
		t.Errorf("error should mention database.path: %v", err)
	}
}

func TestPreflight_CacheDirGetsCreated(t *testing.T) {
	// Preflight's contract: if the cache dir doesn't exist yet, create it.
	// Otherwise fresh installs would fail their first boot before
	// reaching the first transcode.
	t.Setenv("PATH", stubPathWith(t, "ffmpeg", "ffprobe"))

	parent := t.TempDir()
	cacheDir := filepath.Join(parent, "deep", "nested", "cache")
	cfg := baseTestConfig(t)
	cfg.Streaming.CacheDir = cacheDir

	if err := cfg.Preflight(slog.Default()); err != nil {
		t.Fatalf("preflight should create missing dirs: %v", err)
	}
	if _, err := os.Stat(cacheDir); err != nil {
		t.Errorf("cache dir was not created: %v", err)
	}
}

func TestEffectiveCacheDir_ReturnsConfigured(t *testing.T) {
	s := StreamingConfig{CacheDir: "/explicit/path"}
	if got := s.EffectiveCacheDir(); got != "/explicit/path" {
		t.Errorf("got %q, want /explicit/path", got)
	}
}

func TestEffectiveCacheDir_FallsBackToHomeSubpath(t *testing.T) {
	// With an empty CacheDir, the helper must resolve under the user's
	// home dir so the stream.Manager and preflight agree on "the" dir.
	// Overriding HOME gives us a deterministic target without touching
	// the real home tree.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", fakeHome)
	}

	s := StreamingConfig{CacheDir: ""}
	got := s.EffectiveCacheDir()

	want := filepath.Join(fakeHome, ".hubplay", "cache", "transcode")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
