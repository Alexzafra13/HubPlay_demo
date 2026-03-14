package api_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hubplay/internal/api"
	"hubplay/internal/auth"
	"hubplay/internal/clock"
	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/stream"
	"hubplay/internal/testutil"
	"hubplay/internal/user"
)

type streamTestApp struct {
	testApp
	repos *db.Repositories
}

func newStreamTestApp(t *testing.T) *streamTestApp {
	t.Helper()

	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)
	cfg := config.TestConfig()
	clk := &clock.Mock{CurrentTime: time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC)}

	authSvc := auth.NewService(repos.Users, repos.Sessions, cfg.Auth, clk, slog.Default())
	userSvc := user.NewService(repos.Users, slog.Default())
	streamMgr := stream.NewManager(repos.Items, repos.MediaStreams, cfg.Streaming, slog.Default())
	t.Cleanup(streamMgr.Shutdown)

	router := api.NewRouter(api.Dependencies{
		Auth:          authSvc,
		Users:         userSvc,
		StreamManager: streamMgr,
		Items:         repos.Items,
		MediaStreams:   repos.MediaStreams,
		Config:        cfg,
		Logger:        slog.Default(),
	})

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	return &streamTestApp{
		testApp: testApp{server: server},
		repos:   repos,
	}
}

func (a *streamTestApp) setupUser(t *testing.T) string {
	t.Helper()
	resp := a.do(t, "POST", "/api/v1/auth/setup", map[string]string{
		"username": "admin", "password": "admin12345",
	}, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup failed: %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp = a.do(t, "POST", "/api/v1/auth/login", map[string]string{
		"username": "admin", "password": "admin12345",
	}, "")
	data := a.decode(t, resp)["data"].(map[string]any)
	return data["access_token"].(string)
}

func (a *streamTestApp) createItem(t *testing.T, id, container, path string) {
	t.Helper()
	now := time.Now()
	err := a.repos.Items.Create(context.Background(), &db.Item{
		ID: id, LibraryID: "lib-1", Type: "movie", Title: "Test Movie",
		SortTitle: "test movie", Container: container, Path: path,
		AddedAt: now, UpdatedAt: now, IsAvailable: true,
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
}

func (a *streamTestApp) createLibrary(t *testing.T) {
	t.Helper()
	err := a.repos.Libraries.Create(context.Background(), &db.Library{
		ID: "lib-1", Name: "Movies", ContentType: "movies",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
}

func (a *streamTestApp) addStreams(t *testing.T, itemID string, streams []*db.MediaStream) {
	t.Helper()
	err := a.repos.MediaStreams.ReplaceForItem(context.Background(), itemID, streams)
	if err != nil {
		t.Fatalf("add streams: %v", err)
	}
}

// ─── Stream Info ───

func TestStreamInfo_RequiresAuth(t *testing.T) {
	app := newStreamTestApp(t)
	resp := app.do(t, "GET", "/api/v1/stream/item1/info", nil, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestStreamInfo_ItemNotFound(t *testing.T) {
	app := newStreamTestApp(t)
	token := app.setupUser(t)

	resp := app.do(t, "GET", "/api/v1/stream/nonexistent/info", nil, token)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestStreamInfo_DirectPlayMP4(t *testing.T) {
	app := newStreamTestApp(t)
	token := app.setupUser(t)
	app.createLibrary(t)
	app.createItem(t, "movie-1", "mov,mp4,m4a,3gp,3g2,mj2", "/media/test.mp4")
	app.addStreams(t, "movie-1", []*db.MediaStream{
		{ItemID: "movie-1", StreamIndex: 0, StreamType: "video", Codec: "h264", IsDefault: true},
		{ItemID: "movie-1", StreamIndex: 1, StreamType: "audio", Codec: "aac", IsDefault: true},
	})

	resp := app.do(t, "GET", "/api/v1/stream/movie-1/info", nil, token)
	if resp.StatusCode != http.StatusOK {
		body := app.decode(t, resp)
		t.Fatalf("expected 200, got %d: %v", resp.StatusCode, body)
	}

	body := app.decode(t, resp)
	data := body["data"].(map[string]any)
	if data["method"] != "DirectPlay" {
		t.Errorf("expected DirectPlay, got %v", data["method"])
	}
	if data["video_codec"] != "h264" {
		t.Errorf("expected h264, got %v", data["video_codec"])
	}
}

func TestStreamInfo_TranscodeHEVC(t *testing.T) {
	app := newStreamTestApp(t)
	token := app.setupUser(t)
	app.createLibrary(t)
	app.createItem(t, "movie-2", "matroska", "/media/test.mkv")
	app.addStreams(t, "movie-2", []*db.MediaStream{
		{ItemID: "movie-2", StreamIndex: 0, StreamType: "video", Codec: "hevc", IsDefault: true},
		{ItemID: "movie-2", StreamIndex: 1, StreamType: "audio", Codec: "dts", IsDefault: true},
	})

	resp := app.do(t, "GET", "/api/v1/stream/movie-2/info", nil, token)
	if resp.StatusCode != http.StatusOK {
		body := app.decode(t, resp)
		t.Fatalf("expected 200, got %d: %v", resp.StatusCode, body)
	}

	body := app.decode(t, resp)
	data := body["data"].(map[string]any)
	if data["method"] != "Transcode" {
		t.Errorf("expected Transcode, got %v", data["method"])
	}
}

// ─── Subtitles ───

func TestSubtitles_ListEmpty(t *testing.T) {
	app := newStreamTestApp(t)
	token := app.setupUser(t)
	app.createLibrary(t)
	app.createItem(t, "movie-3", "mp4", "/media/test.mp4")

	resp := app.do(t, "GET", "/api/v1/stream/movie-3/subtitles", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := app.decode(t, resp)
	if body["data"] != nil {
		t.Errorf("expected no subtitles, got %v", body["data"])
	}
}

func TestSubtitles_ListWithTracks(t *testing.T) {
	app := newStreamTestApp(t)
	token := app.setupUser(t)
	app.createLibrary(t)
	app.createItem(t, "movie-4", "matroska", "/media/test.mkv")
	app.addStreams(t, "movie-4", []*db.MediaStream{
		{ItemID: "movie-4", StreamIndex: 0, StreamType: "video", Codec: "h264"},
		{ItemID: "movie-4", StreamIndex: 1, StreamType: "audio", Codec: "aac"},
		{ItemID: "movie-4", StreamIndex: 2, StreamType: "subtitle", Codec: "srt", Language: "eng", Title: "English"},
		{ItemID: "movie-4", StreamIndex: 3, StreamType: "subtitle", Codec: "ass", Language: "spa", Title: "Spanish"},
	})

	resp := app.do(t, "GET", "/api/v1/stream/movie-4/subtitles", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := app.decode(t, resp)
	subs := body["data"].([]any)
	if len(subs) != 2 {
		t.Fatalf("expected 2 subtitle tracks, got %d", len(subs))
	}

	first := subs[0].(map[string]any)
	if first["language"] != "eng" {
		t.Errorf("expected eng, got %v", first["language"])
	}
}

// ─── Master Playlist ───

func TestMasterPlaylist_RequiresAuth(t *testing.T) {
	app := newStreamTestApp(t)
	resp := app.do(t, "GET", "/api/v1/stream/item1/master.m3u8", nil, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestMasterPlaylist_ItemNotFound(t *testing.T) {
	app := newStreamTestApp(t)
	token := app.setupUser(t)

	resp := app.do(t, "GET", "/api/v1/stream/nonexistent/master.m3u8", nil, token)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestMasterPlaylist_ReturnsM3U8(t *testing.T) {
	app := newStreamTestApp(t)
	token := app.setupUser(t)
	app.createLibrary(t)
	app.createItem(t, "movie-5", "mp4", "/media/test.mp4")

	resp := app.do(t, "GET", "/api/v1/stream/movie-5/master.m3u8", nil, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/vnd.apple.mpegurl" {
		t.Errorf("expected HLS content type, got %s", ct)
	}
	_ = resp.Body.Close()
}

// ─── Direct Play ───

func TestDirectPlay_RequiresAuth(t *testing.T) {
	app := newStreamTestApp(t)
	resp := app.do(t, "GET", "/api/v1/stream/item1/direct", nil, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestDirectPlay_ItemNotFound(t *testing.T) {
	app := newStreamTestApp(t)
	token := app.setupUser(t)

	resp := app.do(t, "GET", "/api/v1/stream/nonexistent/direct", nil, token)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// ─── Stop Session ───

func TestStopSession_RequiresAuth(t *testing.T) {
	app := newStreamTestApp(t)
	resp := app.do(t, "DELETE", "/api/v1/stream/item1/session", nil, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestStopSession_NoActiveSession(t *testing.T) {
	app := newStreamTestApp(t)
	token := app.setupUser(t)

	resp := app.do(t, "DELETE", "/api/v1/stream/item1/session", nil, token)
	// Should succeed even if no session exists (idempotent)
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}
