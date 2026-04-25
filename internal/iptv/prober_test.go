package iptv

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"hubplay/internal/db"
)

// fakeReporter records the per-channel outcomes the prober pushes.
// Lock-protected so tests with concurrency > 1 don't race the map.
type fakeReporter struct {
	mu    sync.Mutex
	ok    map[string]int
	fails map[string]error
}

func newFakeReporter() *fakeReporter {
	return &fakeReporter{ok: map[string]int{}, fails: map[string]error{}}
}

func (f *fakeReporter) RecordProbeSuccess(_ context.Context, channelID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ok[channelID]++
}

func (f *fakeReporter) RecordProbeFailure(_ context.Context, channelID string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fails[channelID] = err
}

func (f *fakeReporter) snapshot() (map[string]int, map[string]error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ok := make(map[string]int, len(f.ok))
	for k, v := range f.ok {
		ok[k] = v
	}
	fails := make(map[string]error, len(f.fails))
	for k, v := range f.fails {
		fails[k] = v
	}
	return ok, fails
}

func TestProber_HappyPath_HLSManifest(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:-1,sample\nhttp://x/y.ts\n"))
	}))
	defer srv.Close()

	rep := newFakeReporter()
	p := NewProber(nil, rep)
	p.SetTimeout(2 * time.Second)

	channels := []*db.Channel{{ID: "c1", StreamURL: srv.URL + "/playlist.m3u8"}}
	summary := p.ProbeChannels(context.Background(), channels)

	if summary.OK != 1 || summary.Failed != 0 {
		t.Fatalf("want 1 ok / 0 failed, got %+v", summary)
	}
	ok, fails := rep.snapshot()
	if ok["c1"] != 1 || len(fails) != 0 {
		t.Fatalf("reporter mismatch: ok=%v fails=%v", ok, fails)
	}
}

func TestProber_HLSWithoutMagicIsFailure(t *testing.T) {
	t.Parallel()
	// 200 OK but body is an HTML "blocked" page — common soft-404
	// pattern from CDNs that want to look healthy at the HTTP layer.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>blocked</html>"))
	}))
	defer srv.Close()

	rep := newFakeReporter()
	p := NewProber(nil, rep)
	p.SetTimeout(2 * time.Second)

	channels := []*db.Channel{{ID: "c1", StreamURL: srv.URL + "/x.m3u8"}}
	summary := p.ProbeChannels(context.Background(), channels)

	if summary.OK != 0 || summary.Failed != 1 {
		t.Fatalf("want 0 ok / 1 failed, got %+v", summary)
	}
	_, fails := rep.snapshot()
	if !strings.Contains(fails["c1"].Error(), "invalid HLS manifest") {
		t.Fatalf("expected manifest error, got %v", fails["c1"])
	}
}

func TestProber_HTTPErrorIsFailure(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusGone)
	}))
	defer srv.Close()

	rep := newFakeReporter()
	p := NewProber(nil, rep)
	p.SetTimeout(2 * time.Second)

	channels := []*db.Channel{{ID: "c1", StreamURL: srv.URL + "/x"}}
	summary := p.ProbeChannels(context.Background(), channels)

	if summary.Failed != 1 {
		t.Fatalf("want failed=1, got %+v", summary)
	}
	_, fails := rep.snapshot()
	if !strings.Contains(fails["c1"].Error(), "HTTP 410") {
		t.Fatalf("want 'HTTP 410' in error, got %v", fails["c1"])
	}
}

func TestProber_TimeoutCountsAsFailure(t *testing.T) {
	t.Parallel()
	// Block forever to force the per-probe ctx to expire.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	rep := newFakeReporter()
	p := NewProber(nil, rep)
	p.SetTimeout(50 * time.Millisecond)

	channels := []*db.Channel{{ID: "c1", StreamURL: srv.URL + "/x"}}
	summary := p.ProbeChannels(context.Background(), channels)

	if summary.Failed != 1 {
		t.Fatalf("want failed=1, got %+v", summary)
	}
}

func TestProber_NonHTTPSchemeIsSkippedAndCountsOk(t *testing.T) {
	t.Parallel()
	rep := newFakeReporter()
	p := NewProber(nil, rep)
	channels := []*db.Channel{
		{ID: "c1", StreamURL: "rtmp://server/live"},
		{ID: "c2", StreamURL: "udp://239.0.0.1:1234"},
	}
	summary := p.ProbeChannels(context.Background(), channels)
	if summary.Skipped != 2 {
		t.Fatalf("want skipped=2 got %+v", summary)
	}
	ok, fails := rep.snapshot()
	if ok["c1"] != 1 || ok["c2"] != 1 || len(fails) != 0 {
		t.Fatalf("non-HTTP must be reported as success: ok=%v fails=%v", ok, fails)
	}
}

func TestProber_EmptyURLFails(t *testing.T) {
	t.Parallel()
	rep := newFakeReporter()
	p := NewProber(nil, rep)
	channels := []*db.Channel{{ID: "c1", StreamURL: "   "}}
	summary := p.ProbeChannels(context.Background(), channels)
	if summary.Failed != 1 {
		t.Fatalf("want failed=1 got %+v", summary)
	}
}

func TestProber_ConcurrencyCap(t *testing.T) {
	t.Parallel()
	var (
		mu       sync.Mutex
		inflight int
		peak     int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		inflight++
		if inflight > peak {
			peak = inflight
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		inflight--
		mu.Unlock()
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = w.Write([]byte("#EXTM3U\n"))
	}))
	defer srv.Close()

	rep := newFakeReporter()
	p := NewProber(nil, rep)
	p.SetConcurrency(2)
	p.SetTimeout(2 * time.Second)

	channels := make([]*db.Channel, 12)
	for i := range channels {
		channels[i] = &db.Channel{ID: "c", StreamURL: srv.URL + "/p.m3u8"}
	}
	_ = p.ProbeChannels(context.Background(), channels)

	mu.Lock()
	defer mu.Unlock()
	if peak > 2 {
		t.Fatalf("concurrency cap exceeded: peak=%d (cap=2)", peak)
	}
}

func TestProber_ContextCancelStopsScheduling(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	rep := newFakeReporter()
	p := NewProber(nil, rep)
	p.SetConcurrency(1)
	p.SetTimeout(2 * time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediate cancel: no probe should ever start

	channels := []*db.Channel{
		{ID: "c1", StreamURL: srv.URL + "/x"},
		{ID: "c2", StreamURL: srv.URL + "/y"},
	}
	summary := p.ProbeChannels(ctx, channels)
	if summary.OK+summary.Failed > 0 {
		t.Fatalf("expected no probes to complete after pre-cancel, got %+v", summary)
	}
}

func TestProber_NilReporterIsNoop(t *testing.T) {
	t.Parallel()
	p := NewProber(nil, nil)
	summary := p.ProbeChannels(context.Background(), []*db.Channel{{ID: "c1", StreamURL: "http://x"}})
	if summary.OK != 0 || summary.Failed != 0 {
		t.Fatalf("nil reporter must short-circuit: %+v", summary)
	}
}

func TestHasHLSMagic(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   []byte
		want bool
	}{
		{"plain", []byte("#EXTM3U\n#EXTINF"), true},
		{"with bom", []byte{0xEF, 0xBB, 0xBF, '#', 'E', 'X', 'T', 'M', '3', 'U', '\n'}, true},
		{"leading whitespace", []byte("  \n\n#EXTM3U\n"), true},
		{"html page", []byte("<html><body>blocked</body></html>"), false},
		{"empty", []byte{}, false},
		{"only newlines", []byte("\n\n\n"), false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := hasHLSMagic(tc.in); got != tc.want {
				t.Fatalf("%s: want %v got %v", tc.name, tc.want, got)
			}
		})
	}
}

func TestLooksLikeHLS(t *testing.T) {
	t.Parallel()
	cases := []struct {
		url string
		ct  string
		hls bool
	}{
		{"https://x/y/z.m3u8", "", true},
		{"https://x/y/z.m3u8?token=abc", "", true},
		{"https://x/y/z.M3U8", "", true},
		{"https://x/y/z.ts", "", false},
		{"https://x/y/z.ts", "application/vnd.apple.mpegurl", true},
		{"https://x/y/z", "application/x-mpegurl", true},
		{"https://x/y/z", "video/mp2t", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.url+"|"+tc.ct, func(t *testing.T) {
			t.Parallel()
			if got := looksLikeHLS(tc.url, tc.ct); got != tc.hls {
				t.Fatalf("want %v got %v", tc.hls, got)
			}
		})
	}
}

// Sanity check that errors.New still routes through the prober's
// error sink — guards against future refactors that swap the
// reporter call site.
func TestProber_FailureForwardsError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusNotFound)
	}))
	defer srv.Close()
	rep := newFakeReporter()
	p := NewProber(nil, rep)
	p.SetTimeout(time.Second)
	_ = p.ProbeChannels(context.Background(), []*db.Channel{{ID: "c1", StreamURL: srv.URL}})
	_, fails := rep.snapshot()
	if fails["c1"] == nil || !errors.Is(fails["c1"], fails["c1"]) {
		t.Fatal("expected non-nil error forwarded to reporter")
	}
}
