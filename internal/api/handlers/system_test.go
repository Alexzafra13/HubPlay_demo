package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/stream"
	"hubplay/internal/sysmetrics"
	"hubplay/internal/testutil"
)

// fakeSystemStreams is a SystemStatsProvider stub for SystemHandler tests.
// Kept local to system_test.go so it doesn't collide with the streaming
// fakes that live next to stream_test.go.
type fakeSystemStreams struct {
	active     int
	max        int
	hwEnabled  bool
	hwResult   stream.HWAccelResult
	cacheDir   string
}

func (f *fakeSystemStreams) ActiveSessions() int                  { return f.active }
func (f *fakeSystemStreams) MaxTranscodeSessions() int            { return f.max }
func (f *fakeSystemStreams) HWAccelInfo() stream.HWAccelResult    { return f.hwResult }
func (f *fakeSystemStreams) HWAccelEnabled() bool                 { return f.hwEnabled }
func (f *fakeSystemStreams) CacheDir() string                     { return f.cacheDir }

var _ SystemStatsProvider = (*fakeSystemStreams)(nil)

// fakeSystemLibs is a LibraryStatsProvider stub. counts is keyed by
// library ID; missing IDs return 0 so the inventory rollup degrades
// cleanly when a single library can't be counted.
type fakeSystemLibs struct {
	libs   []*db.Library
	counts map[string]int
	listErr error
	countErr map[string]error
}

func (f *fakeSystemLibs) List(_ context.Context) ([]*db.Library, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.libs, nil
}

func (f *fakeSystemLibs) ItemCount(_ context.Context, libraryID string) (int, error) {
	if err := f.countErr[libraryID]; err != nil {
		return 0, err
	}
	return f.counts[libraryID], nil
}

var _ LibraryStatsProvider = (*fakeSystemLibs)(nil)

// fakeHost is a HostInfoProvider stub returning a canned snapshot.
// Lets the handler tests assert that the host section serialises
// correctly without spinning a real gopsutil sampler.
type fakeHost struct {
	snap sysmetrics.HostInfo
}

func (f *fakeHost) Snapshot() sysmetrics.HostInfo { return f.snap }

var _ HostInfoProvider = (*fakeHost)(nil)

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
			BindAddress   string `json:"bind_address"`
			BaseURL       string `json:"base_url"`
			Timezone      string `json:"timezone"`
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
			HWAccelEnabled    bool     `json:"hw_accel_enabled"`
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
		Libraries struct {
			Total      int `json:"total"`
			ItemsTotal int `json:"items_total"`
			ByType     []struct {
				ContentType string `json:"content_type"`
				Count       int    `json:"count"`
				Items       int    `json:"items"`
			} `json:"by_type"`
		} `json:"libraries"`
		Host struct {
			CPUModel            string  `json:"cpu_model"`
			CPUCoresPhysical    int     `json:"cpu_cores_physical"`
			CPUCoresLogical     int     `json:"cpu_cores_logical"`
			CPUPercent          float64 `json:"cpu_percent"`
			RAMTotalBytes       uint64  `json:"ram_total_bytes"`
			RAMUsedBytes        uint64  `json:"ram_used_bytes"`
			GPUModel            string  `json:"gpu_model"`
			GPUMemoryTotalBytes uint64  `json:"gpu_memory_total_bytes"`
			GPUDriverVersion    string  `json:"gpu_driver_version"`
		} `json:"host"`
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
		active:    2,
		max:       8,
		hwEnabled: true,
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

	h := NewSystemHandler(SystemHandlerConfig{
		DB:          database,
		Streams:     streams,
		ImageDir:       imageDir,
		DBPath:         dbPath,
		BindAddress:    "0.0.0.0:8096",
		BaseURLDefault: "https://hubplay.example.com",
		Version:        "test-9.9.9",
		Logger:         newQuietLogger(),
	})

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
	if out.Data.Server.BindAddress != "0.0.0.0:8096" {
		t.Errorf("bind_address: %q", out.Data.Server.BindAddress)
	}
	if out.Data.Server.BaseURL != "https://hubplay.example.com" {
		t.Errorf("base_url: %q", out.Data.Server.BaseURL)
	}
	if out.Data.Server.Timezone == "" {
		t.Errorf("timezone should be populated")
	}
	if !out.Data.FFmpeg.HWAccelEnabled {
		t.Errorf("hw_accel_enabled should mirror the streams provider's flag")
	}
}

func TestSystemHandler_Stats_ReportsDBError(t *testing.T) {
	database := testutil.NewTestDB(t)
	_ = database.Close() // induce a Ping() error.

	h := NewSystemHandler(SystemHandlerConfig{
		DB:      database,
		Version: "v",
		Logger:  newQuietLogger(),
	})

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
	h := NewSystemHandler(SystemHandlerConfig{
		DB:      database,
		Version: "v",
		Logger:  newQuietLogger(),
	})

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

	h := NewSystemHandler(SystemHandlerConfig{
		DB:       database,
		ImageDir: imageDir,
		Version:  "v",
		Logger:   newQuietLogger(),
	})

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
	h := NewSystemHandler(SystemHandlerConfig{
		DB:       database,
		ImageDir: filepath.Join(t.TempDir(), "does-not-exist"),
		Version:  "v",
		Logger:   newQuietLogger(),
	})

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

func TestSystemHandler_Stats_LibrariesRollup(t *testing.T) {
	database := testutil.NewTestDB(t)

	libs := &fakeSystemLibs{
		libs: []*db.Library{
			{ID: "a", ContentType: "movies"},
			{ID: "b", ContentType: "movies"},
			{ID: "c", ContentType: "shows"},
			{ID: "d", ContentType: "livetv"},
		},
		counts: map[string]int{
			"a": 120,
			"b": 80,
			"c": 30,
			"d": 240,
		},
	}

	h := NewSystemHandler(SystemHandlerConfig{
		DB:        database,
		Libraries: libs,
		Version:   "v",
		Logger:    newQuietLogger(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/stats", nil)
	rr := httptest.NewRecorder()
	h.Stats(rr, req)

	out := decodeStats(t, rr.Body)
	if out.Data.Libraries.Total != 4 {
		t.Errorf("libraries.total: %d want 4", out.Data.Libraries.Total)
	}
	if out.Data.Libraries.ItemsTotal != 470 {
		t.Errorf("libraries.items_total: %d want 470", out.Data.Libraries.ItemsTotal)
	}
	if len(out.Data.Libraries.ByType) != 3 {
		t.Fatalf("libraries.by_type: want 3 buckets, got %d", len(out.Data.Libraries.ByType))
	}

	// Sorted alphabetically: livetv, movies, shows.
	wantOrder := []string{"livetv", "movies", "shows"}
	for i, want := range wantOrder {
		if out.Data.Libraries.ByType[i].ContentType != want {
			t.Errorf("by_type[%d].content_type: %q want %q", i, out.Data.Libraries.ByType[i].ContentType, want)
		}
	}

	// Movies bucket should aggregate both libraries.
	for _, b := range out.Data.Libraries.ByType {
		if b.ContentType == "movies" {
			if b.Count != 2 || b.Items != 200 {
				t.Errorf("movies bucket: count=%d items=%d, want 2/200", b.Count, b.Items)
			}
		}
	}
}

func TestSystemHandler_Stats_LibrariesRollup_SkipsCountErrors(t *testing.T) {
	// One library's ItemCount fails — the rollup should still report the
	// other ones cleanly and never 500 the panel.
	database := testutil.NewTestDB(t)

	libs := &fakeSystemLibs{
		libs: []*db.Library{
			{ID: "ok", ContentType: "movies"},
			{ID: "broken", ContentType: "movies"},
		},
		counts: map[string]int{"ok": 50},
		countErr: map[string]error{
			"broken": errors.New("simulated counter error"),
		},
	}

	h := NewSystemHandler(SystemHandlerConfig{
		DB:        database,
		Libraries: libs,
		Version:   "v",
		Logger:    newQuietLogger(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/stats", nil)
	rr := httptest.NewRecorder()
	h.Stats(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("partial-failure rollup must still 200, got %d", rr.Code)
	}
	out := decodeStats(t, rr.Body)
	if out.Data.Libraries.Total != 2 {
		t.Errorf("total libs counted regardless of count error: got %d want 2", out.Data.Libraries.Total)
	}
	if out.Data.Libraries.ItemsTotal != 50 {
		t.Errorf("only the working library contributes items: got %d want 50", out.Data.Libraries.ItemsTotal)
	}
}

// TestSystemHandler_StreamActivity_BackfillsEmptyDays exercises the
// admin Resumen sparkline endpoint. The query must always emit a
// contiguous date series — gaps would render as visual breaks
// rather than "no plays that day".
func TestSystemHandler_StreamActivity_BackfillsEmptyDays(t *testing.T) {
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories("sqlite", database)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := repos.Users.Create(ctx, &db.User{
		ID: "u-1", Username: "u", PasswordHash: "h",
		Role: "user", CreatedAt: now, IsActive: true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repos.Libraries.Create(ctx, &db.Library{
		ID: "lib-1", Name: "L", ContentType: "movies", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := repos.Items.Create(ctx, &db.Item{
		ID: "movie-1", LibraryID: "lib-1", Type: "movie", Title: "M", SortTitle: "m",
		DurationTicks: 60 * 60 * 10_000_000, AddedAt: now, UpdatedAt: now, IsAvailable: true,
	}); err != nil {
		t.Fatalf("create item: %v", err)
	}
	// Plant one progress row in the user_data table so today's
	// bucket carries non-zero numbers; previous days stay empty.
	if err := repos.UserData.UpdateProgress(ctx, "u-1", "movie-1", 30*60*10_000_000, false); err != nil {
		t.Fatalf("update progress: %v", err)
	}

	h := NewSystemHandler(SystemHandlerConfig{DB: database, Version: "v", Logger: newQuietLogger()})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/stream-activity?days=7", nil)
	rr := httptest.NewRecorder()
	h.StreamActivity(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d body: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data struct {
			Days    int `json:"days"`
			Buckets []struct {
				Date         string `json:"date"`
				WatchMinutes int    `json:"watch_minutes"`
				SessionCount int    `json:"session_count"`
			} `json:"buckets"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Days != 7 {
		t.Errorf("days = %d, want 7", resp.Data.Days)
	}
	if len(resp.Data.Buckets) != 7 {
		t.Fatalf("expected contiguous 7 buckets, got %d", len(resp.Data.Buckets))
	}
	// Last bucket = today, has the play. The other six are zero-padded.
	last := resp.Data.Buckets[len(resp.Data.Buckets)-1]
	if last.SessionCount != 1 {
		t.Errorf("today session_count = %d, want 1 (full series: %+v)", last.SessionCount, resp.Data.Buckets)
	}
	if last.WatchMinutes != 30 {
		t.Errorf("today watch_minutes = %d, want 30 (30 min play)", last.WatchMinutes)
	}
}

// TestSystemHandler_TopItems_EpisodesRolledUpToSeries ensures the
// admin "most watched" list rolls up episode plays to their parent
// series instead of polluting the chart with individual episodes.
func TestSystemHandler_TopItems_EpisodesRolledUpToSeries(t *testing.T) {
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories("sqlite", database)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := repos.Users.Create(ctx, &db.User{
		ID: "u-1", Username: "u", PasswordHash: "h",
		Role: "user", CreatedAt: now, IsActive: true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repos.Libraries.Create(ctx, &db.Library{
		ID: "lib-1", Name: "L", ContentType: "shows", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create library: %v", err)
	}
	mustItem := func(id, parent, kind, title string) {
		if err := repos.Items.Create(ctx, &db.Item{
			ID: id, LibraryID: "lib-1", ParentID: parent, Type: kind, Title: title, SortTitle: title,
			DurationTicks: 1, AddedAt: now, UpdatedAt: now, IsAvailable: true,
		}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	mustItem("series-1", "", "series", "Mr Robot")
	mustItem("season-1", "series-1", "season", "S1")
	mustItem("ep-1", "season-1", "episode", "S1E1")
	mustItem("ep-2", "season-1", "episode", "S1E2")

	if err := repos.UserData.UpdateProgress(ctx, "u-1", "ep-1", 1, false); err != nil {
		t.Fatalf("progress ep-1: %v", err)
	}
	if err := repos.UserData.UpdateProgress(ctx, "u-1", "ep-2", 1, false); err != nil {
		t.Fatalf("progress ep-2: %v", err)
	}

	h := NewSystemHandler(SystemHandlerConfig{DB: database, Version: "v", Logger: newQuietLogger()})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/top-items?days=7&limit=5", nil)
	rr := httptest.NewRecorder()
	h.TopItems(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d body: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data struct {
			Items []struct {
				ID        string `json:"id"`
				Type      string `json:"type"`
				Title     string `json:"title"`
				PlayCount int    `json:"play_count"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data.Items) != 1 {
		t.Fatalf("expected 1 rolled-up entry, got %d: %+v", len(resp.Data.Items), resp.Data.Items)
	}
	row := resp.Data.Items[0]
	if row.ID != "series-1" || row.Type != "series" {
		t.Errorf("expected series-1, got %+v", row)
	}
	// Two episodes by the same user roll up to one distinct play.
	if row.PlayCount != 1 {
		t.Errorf("play_count = %d, want 1 (DISTINCT user_id per rollup)", row.PlayCount)
	}
}

// TestSystemHandler_Stats_HostBlock_SerialisesAllFields covers the
// admin "Host" card wire shape. The handler must not silently swallow
// any sysmetrics snapshot field — the panel reads each one directly
// and missing values would render as dashes for no reason.
func TestSystemHandler_Stats_HostBlock_SerialisesAllFields(t *testing.T) {
	database := testutil.NewTestDB(t)
	host := &fakeHost{snap: sysmetrics.HostInfo{
		CPUModel:            "AMD Ryzen 5 5600 6-Core Processor",
		CPUCoresPhysical:    6,
		CPUCoresLogical:     12,
		CPUPercent:          42.5,
		RAMTotalBytes:       16 * 1024 * 1024 * 1024,
		RAMUsedBytes:        4 * 1024 * 1024 * 1024,
		GPUModel:            "NVIDIA GeForce GTX 1660",
		GPUMemoryTotalBytes: 6 * 1024 * 1024 * 1024,
		GPUDriverVersion:    "560.35.03",
	}}
	h := NewSystemHandler(SystemHandlerConfig{
		DB:     database,
		Host:   host,
		Logger: newQuietLogger(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/stats", nil)
	rr := httptest.NewRecorder()
	h.Stats(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	got := decodeStats(t, rr.Body).Data.Host
	if got.CPUModel != "AMD Ryzen 5 5600 6-Core Processor" {
		t.Errorf("cpu_model = %q", got.CPUModel)
	}
	if got.CPUCoresPhysical != 6 || got.CPUCoresLogical != 12 {
		t.Errorf("cores: p=%d l=%d, want 6/12", got.CPUCoresPhysical, got.CPUCoresLogical)
	}
	if got.CPUPercent != 42.5 {
		t.Errorf("cpu_percent = %f, want 42.5", got.CPUPercent)
	}
	if got.RAMTotalBytes == 0 || got.RAMUsedBytes == 0 {
		t.Errorf("ram missing: total=%d used=%d", got.RAMTotalBytes, got.RAMUsedBytes)
	}
	if got.GPUModel != "NVIDIA GeForce GTX 1660" {
		t.Errorf("gpu_model = %q", got.GPUModel)
	}
	if got.GPUDriverVersion != "560.35.03" {
		t.Errorf("gpu_driver_version = %q", got.GPUDriverVersion)
	}
}

// TestSystemHandler_Stats_HostBlock_AbsentProviderEmitsZeroes pins the
// nil-host degradation contract: a test rig / minimal startup that
// doesn't wire HostInfoProvider gets a zero-value host section, not a
// 500. The frontend renders dashes for empty rows.
func TestSystemHandler_Stats_HostBlock_AbsentProviderEmitsZeroes(t *testing.T) {
	h := NewSystemHandler(SystemHandlerConfig{
		DB:     testutil.NewTestDB(t),
		Host:   nil, // explicit — same shape the test rigs use
		Logger: newQuietLogger(),
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/stats", nil)
	rr := httptest.NewRecorder()
	h.Stats(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	host := decodeStats(t, rr.Body).Data.Host
	if host.CPUModel != "" || host.GPUModel != "" || host.RAMTotalBytes != 0 || host.CPUPercent != 0 {
		t.Errorf("nil host provider should yield zero-value section; got %+v", host)
	}
}
