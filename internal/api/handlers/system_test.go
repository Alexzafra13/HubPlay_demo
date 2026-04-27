package handlers

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"hubplay/internal/stream"
	"hubplay/internal/testutil"
)

// fakeSystemStreams is a SystemStatsProvider stub for SystemHandler tests.
// Kept local to system_test.go so it doesn't collide with the streaming
// fakes that live next to stream_test.go.
type fakeSystemStreams struct {
	active   int
	max      int
	hwResult stream.HWAccelResult
	cacheDir string
}

func (f *fakeSystemStreams) ActiveSessions() int                  { return f.active }
func (f *fakeSystemStreams) MaxTranscodeSessions() int            { return f.max }
func (f *fakeSystemStreams) HWAccelInfo() stream.HWAccelResult    { return f.hwResult }
func (f *fakeSystemStreams) CacheDir() string                     { return f.cacheDir }

var _ SystemStatsProvider = (*fakeSystemStreams)(nil)

func newQuietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// systemStatsResponse mirrors the wire shape just enough for assertions.
// Kept private so production code can't be tempted to import the test
// type as a contract.
type systemStatsResponse struct {
	Data struct {
		Server struct {
			Version       string `json:"version"`
			GoVersion     string `json:"go_version"`
			UptimeSeconds int64  `json:"uptime_seconds"`
		} `json:"server"`
		Database struct {
			OK        bool   `json:"ok"`
			Path      string `json:"path"`
			SizeBytes int64  `json:"size_bytes"`
			Error     string `json:"error,omitempty"`
		} `json:"database"`
		FFmpeg struct {
			Found             bool     `json:"found"`
			Path              string   `json:"path"`
			HWAccelsAvailable []string `json:"hw_accels_available"`
			HWAccelSelected   string   `json:"hw_accel_selected"`
		} `json:"ffmpeg"`
		Runtime struct {
			Goroutines    int   `json:"goroutines"`
			MemoryAllocMB int64 `json:"memory_alloc_mb"`
			CPUCount      int   `json:"cpu_count"`
		} `json:"runtime"`
		Streaming struct {
			TranscodeSessionsActive int `json:"transcode_sessions_active"`
			TranscodeSessionsMax    int `json:"transcode_sessions_max"`
		} `json:"streaming"`
		Storage struct {
			ImageDirPath        string `json:"image_dir_path"`
			ImageDirBytes       int64  `json:"image_dir_bytes"`
			TranscodeCachePath  string `json:"transcode_cache_path"`
			TranscodeCacheBytes int64  `json:"transcode_cache_bytes"`
		} `json:"storage"`
	} `json:"data"`
}

func decodeStats(t *testing.T, body io.Reader) systemStatsResponse {
	t.Helper()
	var out systemStatsResponse
	if err := json.NewDecoder(body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestSystemHandler_Stats_ReportsServerAndRuntime(t *testing.T) {
	database := testutil.NewTestDB(t)
	streams := &fakeSystemStreams{
		active: 2,
		max:    8,
		hwResult: stream.HWAccelResult{
			Available: []stream.HWAccelType{stream.HWAccelVAAPI, stream.HWAccelQSV},
			Selected:  stream.HWAccelVAAPI,
			Encoder:   "h264_vaapi",
		},
		cacheDir: t.TempDir(),
	}
	imageDir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "hubplay.db")
	// Touch the DB file so size readout returns >0 — the in-memory test
	// DB's path is empty, so we point dbPath at a separate sentinel file.
	if err := os.WriteFile(dbPath, []byte("not a real sqlite db, just bytes"), 0o600); err != nil {
		t.Fatalf("seed db file: %v", err)
	}

	h := NewSystemHandler(database, streams, imageDir, dbPath, "test-9.9.9", newQuietLogger())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/stats", nil)
	rr := httptest.NewRecorder()
	h.Stats(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d, body: %s", rr.Code, rr.Body.String())
	}

	out := decodeStats(t, rr.Body)
	if out.Data.Server.Version != "test-9.9.9" {
		t.Errorf("version: %q", out.Data.Server.Version)
	}
	if out.Data.Server.GoVersion == "" {
		t.Errorf("go_version should be populated")
	}
	if out.Data.Database.OK != true {
		t.Errorf("database should report OK on healthy test DB, got error %q", out.Data.Database.Error)
	}
	if out.Data.Database.Path != dbPath {
		t.Errorf("database.path: %q want %q", out.Data.Database.Path, dbPath)
	}
	if out.Data.Database.SizeBytes <= 0 {
		t.Errorf("database.size_bytes should reflect file size, got %d", out.Data.Database.SizeBytes)
	}
	if out.Data.Streaming.TranscodeSessionsActive != 2 {
		t.Errorf("transcode active: %d", out.Data.Streaming.TranscodeSessionsActive)
	}
	if out.Data.Streaming.TranscodeSessionsMax != 8 {
		t.Errorf("transcode max: %d", out.Data.Streaming.TranscodeSessionsMax)
	}
	if out.Data.FFmpeg.HWAccelSelected != "vaapi" {
		t.Errorf("hw selected: %q", out.Data.FFmpeg.HWAccelSelected)
	}
	if len(out.Data.FFmpeg.HWAccelsAvailable) != 2 {
		t.Errorf("hw available: %v", out.Data.FFmpeg.HWAccelsAvailable)
	}
	if out.Data.Storage.ImageDirPath != imageDir {
		t.Errorf("image_dir_path: %q", out.Data.Storage.ImageDirPath)
	}
	if out.Data.Storage.TranscodeCachePath != streams.cacheDir {
		t.Errorf("transcode_cache_path: %q", out.Data.Storage.TranscodeCachePath)
	}
	if out.Data.Runtime.CPUCount <= 0 {
		t.Errorf("cpu_count should be > 0, got %d", out.Data.Runtime.CPUCount)
	}
}

func TestSystemHandler_Stats_ReportsDBError(t *testing.T) {
	database := testutil.NewTestDB(t)
	_ = database.Close() // induce a Ping() error.

	h := NewSystemHandler(database, nil, "", "", "v", newQuietLogger())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/stats", nil)
	rr := httptest.NewRecorder()
	h.Stats(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handler should still return 200 on DB error (panel renders the badge), got %d", rr.Code)
	}
	out := decodeStats(t, rr.Body)
	if out.Data.Database.OK {
		t.Errorf("database.ok should be false on closed DB")
	}
	if out.Data.Database.Error == "" {
		t.Errorf("database.error should be populated on closed DB")
	}
}

func TestSystemHandler_Stats_NilStreams_ZeroEverything(t *testing.T) {
	database := testutil.NewTestDB(t)
	h := NewSystemHandler(database, nil, "", "", "v", newQuietLogger())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/stats", nil)
	rr := httptest.NewRecorder()
	h.Stats(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	out := decodeStats(t, rr.Body)
	if out.Data.Streaming.TranscodeSessionsActive != 0 {
		t.Errorf("active should be 0, got %d", out.Data.Streaming.TranscodeSessionsActive)
	}
	if out.Data.Streaming.TranscodeSessionsMax != 0 {
		t.Errorf("max should be 0, got %d", out.Data.Streaming.TranscodeSessionsMax)
	}
	if out.Data.FFmpeg.HWAccelSelected != "" {
		t.Errorf("no streams provider should yield empty hw selected, got %q", out.Data.FFmpeg.HWAccelSelected)
	}
}

func TestSystemHandler_Stats_DirSizeWalksImageDir(t *testing.T) {
	database := testutil.NewTestDB(t)
	imageDir := t.TempDir()

	// Seed a few files of known size across two subdirs.
	if err := os.MkdirAll(filepath.Join(imageDir, "movies", "primary"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(imageDir, "a.jpg"), make([]byte, 1024), 0o644); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(imageDir, "movies", "primary", "b.jpg"), make([]byte, 2048), 0o644); err != nil {
		t.Fatalf("write b: %v", err)
	}

	h := NewSystemHandler(database, nil, imageDir, "", "v", newQuietLogger())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/stats", nil)
	rr := httptest.NewRecorder()
	h.Stats(rr, req)

	out := decodeStats(t, rr.Body)
	if out.Data.Storage.ImageDirBytes != 1024+2048 {
		t.Errorf("image_dir_bytes: %d want %d", out.Data.Storage.ImageDirBytes, 1024+2048)
	}
}

func TestSystemHandler_Stats_MissingImageDir_ZeroBytes(t *testing.T) {
	database := testutil.NewTestDB(t)
	// Deliberately point at a non-existent path. dirSizeOrZero must
	// swallow ErrNotExist and return 0 (typical first-boot scenario).
	h := NewSystemHandler(database, nil, filepath.Join(t.TempDir(), "does-not-exist"), "", "v", newQuietLogger())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/stats", nil)
	rr := httptest.NewRecorder()
	h.Stats(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d, body: %s", rr.Code, rr.Body.String())
	}
	out := decodeStats(t, rr.Body)
	if out.Data.Storage.ImageDirBytes != 0 {
		t.Errorf("missing dir should yield 0 bytes, got %d", out.Data.Storage.ImageDirBytes)
	}
}
