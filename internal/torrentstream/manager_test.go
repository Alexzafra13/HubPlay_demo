package torrentstream

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestOptionsWithDefaults(t *testing.T) {
	got := Options{}.withDefaults()
	if got.MaxSessions != 4 {
		t.Errorf("MaxSessions: got %d want 4", got.MaxSessions)
	}
	if got.IdleTimeout != 5*time.Minute {
		t.Errorf("IdleTimeout: got %s want 5m", got.IdleTimeout)
	}
	if got.Readahead != 16<<20 {
		t.Errorf("Readahead: got %d want 16MiB", got.Readahead)
	}
	if got.MetadataTimeout != 60*time.Second {
		t.Errorf("MetadataTimeout: got %s want 60s", got.MetadataTimeout)
	}
	if got.DataDir == "" {
		t.Error("DataDir should default to a temp path")
	}

	// Explicit values survive.
	custom := Options{MaxSessions: 2, IdleTimeout: time.Minute, Readahead: 1, MetadataTimeout: time.Second, DataDir: "/x"}.withDefaults()
	if custom.MaxSessions != 2 || custom.IdleTimeout != time.Minute || custom.Readahead != 1 || custom.DataDir != "/x" {
		t.Errorf("explicit options were overwritten: %+v", custom)
	}
}

func TestGuessContentType(t *testing.T) {
	cases := map[string]string{
		"movie.mp4":     "video/mp4",
		"clip.M4V":      "video/mp4",
		"show.mkv":      "video/x-matroska",
		"x.webm":        "video/webm",
		"a.avi":         "video/x-msvideo",
		"seg.ts":        "video/mp2t",
		"song.mp3":      "audio/mpeg",
		"track.flac":    "audio/flac",
		"unknown.xyz":   "application/octet-stream",
		"noext":         "application/octet-stream",
		"dir/Movie.MoV": "video/quicktime",
	}
	for name, want := range cases {
		if got := guessContentType(name); got != want {
			t.Errorf("guessContentType(%q): got %q want %q", name, got, want)
		}
	}
}

func TestSessionTouchIdle(t *testing.T) {
	s := &Session{}
	t0 := time.Unix(1_000_000, 0)
	s.touch(t0)

	if s.idle(t0.Add(30*time.Second), time.Minute) {
		t.Error("session should not be idle within the timeout window")
	}
	if !s.idle(t0.Add(2*time.Minute), time.Minute) {
		t.Error("session should be idle once the timeout has elapsed")
	}
}

// reapIdle works on the sessions map directly, so we can exercise the
// reaper logic without a live torrent client (sessions with a nil
// torrent skip the Drop call).
func TestReapIdleEvictsStaleSessions(t *testing.T) {
	t0 := time.Unix(2_000_000, 0)
	m := &Manager{
		opts:     Options{IdleTimeout: time.Minute},
		logger:   slog.Default(),
		sessions: map[string]*Session{},
		now:      func() time.Time { return t0 },
	}
	fresh := &Session{infoHash: "fresh"}
	fresh.touch(t0)
	stale := &Session{infoHash: "stale"}
	stale.touch(t0.Add(-2 * time.Minute))
	m.sessions["fresh"] = fresh
	m.sessions["stale"] = stale

	m.reapIdle(t0)

	if m.activeCount() != 1 {
		t.Fatalf("active sessions: got %d want 1", m.activeCount())
	}
	if _, ok := m.sessions["stale"]; ok {
		t.Error("stale session should have been reaped")
	}
	if _, ok := m.sessions["fresh"]; !ok {
		t.Error("fresh session should have survived")
	}
}

func TestNewDisabledReturnsErrDisabled(t *testing.T) {
	m, err := New(Options{Enabled: false}, nil)
	if err != ErrDisabled {
		t.Fatalf("err: got %v want ErrDisabled", err)
	}
	if m != nil {
		t.Error("manager should be nil when disabled")
	}
}

func TestArchiveTorrentURL(t *testing.T) {
	got := ArchiveTorrentURL("sintel")
	want := "https://archive.org/download/sintel/sintel_archive.torrent"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if !strings.HasPrefix(got, "https://archive.org/") {
		t.Error("torrent URL must point at archive.org")
	}
}
