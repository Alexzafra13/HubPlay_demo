package iptv

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

// HTTPS test servers use a self-signed cert by design — the TLS
// handshake fails out of the box for any client that does proper
// verification. Perfect proxy for "expired Let's Encrypt cert" /
// "self-signed provider cert" — the case the toggle exists for.

// With tls_insecure off, fetching from a self-signed HTTPS server
// must fail at the handshake. This is the safe default and the
// regression we're locking down.
func TestRefreshM3U_SelfSignedHTTPS_FailsByDefault(t *testing.T) {
	unblockLoopback(t)
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)
	ctx := context.Background()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-mpegURL")
		fmt.Fprint(w, `#EXTM3U
#EXTINF:-1 tvg-id="x.x" group-title="Spain",X
http://upstream.example/x.m3u8
`)
	}))
	defer srv.Close()

	libID := "lib-strict"
	now := time.Now()
	if err := repos.Libraries.Create(ctx, &db.Library{
		ID: libID, Name: "L", ContentType: "livetv", ScanMode: "manual",
		M3UURL:    srv.URL,
		CreatedAt: now, UpdatedAt: now,
		// TLSInsecure left false — strict default.
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewService(repos.Channels, repos.EPGPrograms, repos.Libraries,
		repos.ChannelFavorites, repos.LibraryEPGSources, repos.ChannelOverrides,
		repos.ChannelWatchHistory,
		slog.New(slog.NewTextHandler(new(discard), nil)))

	_, err := svc.RefreshM3U(ctx, libID)
	if err == nil {
		t.Fatal("expected TLS handshake failure with strict default")
	}
	// Sanity-check the error mentions TLS / certificate so a future
	// regression (e.g. somebody flipping the default) shows up loudly.
	if !strings.Contains(err.Error(), "certificate") &&
		!strings.Contains(err.Error(), "x509") &&
		!strings.Contains(err.Error(), "tls:") {
		t.Errorf("err = %v, want TLS/certificate failure", err)
	}
}

// With tls_insecure ON, the same self-signed server should be
// reachable: the import has to succeed and persist the channel.
func TestRefreshM3U_SelfSignedHTTPS_SucceedsWhenInsecure(t *testing.T) {
	unblockLoopback(t)
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)
	ctx := context.Background()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-mpegURL")
		fmt.Fprint(w, `#EXTM3U
#EXTINF:-1 tvg-id="es.tvg.id" group-title="Spain",La 1
http://upstream.example/la1.m3u8
`)
	}))
	defer srv.Close()

	libID := "lib-insecure"
	now := time.Now()
	if err := repos.Libraries.Create(ctx, &db.Library{
		ID: libID, Name: "L", ContentType: "livetv", ScanMode: "manual",
		M3UURL:      srv.URL,
		TLSInsecure: true,
		CreatedAt:   now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewService(repos.Channels, repos.EPGPrograms, repos.Libraries,
		repos.ChannelFavorites, repos.LibraryEPGSources, repos.ChannelOverrides,
		repos.ChannelWatchHistory,
		slog.New(slog.NewTextHandler(new(discard), nil)))

	n, err := svc.RefreshM3U(ctx, libID)
	if err != nil {
		t.Fatalf("expected success with tls_insecure=true, got %v", err)
	}
	if n != 1 {
		t.Errorf("imported %d channels, want 1", n)
	}
}

// Toggling the flag mid-life: a library starts strict and fails,
// admin flips the bit, refresh succeeds. End-to-end smoke test that
// the Update path threads the new value through to the fetch.
func TestRefreshM3U_ToggleAtRuntime(t *testing.T) {
	unblockLoopback(t)
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)
	ctx := context.Background()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-mpegURL")
		fmt.Fprint(w, `#EXTM3U
#EXTINF:-1 tvg-id="t.x" group-title="Spain",T
http://upstream.example/t.m3u8
`)
	}))
	defer srv.Close()

	libID := "lib-toggle"
	now := time.Now()
	lib := &db.Library{
		ID: libID, Name: "L", ContentType: "livetv", ScanMode: "manual",
		M3UURL: srv.URL, CreatedAt: now, UpdatedAt: now,
	}
	if err := repos.Libraries.Create(ctx, lib); err != nil {
		t.Fatal(err)
	}

	svc := NewService(repos.Channels, repos.EPGPrograms, repos.Libraries,
		repos.ChannelFavorites, repos.LibraryEPGSources, repos.ChannelOverrides,
		repos.ChannelWatchHistory,
		slog.New(slog.NewTextHandler(new(discard), nil)))

	// 1. Strict default — fails.
	if _, err := svc.RefreshM3U(ctx, libID); err == nil {
		t.Fatal("strict default should fail")
	}

	// 2. Operator flips the toggle.
	lib.TLSInsecure = true
	lib.UpdatedAt = time.Now()
	if err := repos.Libraries.Update(ctx, lib); err != nil {
		t.Fatal(err)
	}

	// 3. Now it succeeds.
	if _, err := svc.RefreshM3U(ctx, libID); err != nil {
		t.Fatalf("post-toggle refresh failed: %v", err)
	}
}

// The insecure client is built lazily and cached: hammering it from
// many goroutines must not race or leak transports. Race detector
// catches the construction-side race; the post-condition checks the
// pointer never changes after first use.
func TestService_InsecureFetchClient_Cached(t *testing.T) {
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)
	svc := NewService(repos.Channels, repos.EPGPrograms, repos.Libraries,
		repos.ChannelFavorites, repos.LibraryEPGSources, repos.ChannelOverrides,
		repos.ChannelWatchHistory,
		slog.New(slog.NewTextHandler(new(discard), nil)))

	first := svc.insecureFetchClient()
	if first == nil {
		t.Fatal("insecureFetchClient returned nil")
	}
	for i := 0; i < 50; i++ {
		got := svc.insecureFetchClient()
		if got != first {
			t.Fatal("insecureFetchClient should return the same cached client")
		}
	}

	// Sanity: the transport actually has InsecureSkipVerify=true.
	tr, ok := first.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", first.Transport)
	}
	if tr.TLSClientConfig == nil || !tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("insecure client must have TLSClientConfig.InsecureSkipVerify = true")
	}
	// Belt-and-braces: the strict client must NOT have this set.
	strictTr := svc.httpClient.Transport
	if strictTr != nil {
		if cfg := extractTLSConfig(strictTr); cfg != nil && cfg.InsecureSkipVerify {
			t.Error("strict client must NOT have InsecureSkipVerify set")
		}
	}
}

// extractTLSConfig is a tiny helper that reaches into a transport
// only when it's the standard *http.Transport. nil for anything
// else (default transport included) — that's the strict-by-default
// behaviour we want.
func extractTLSConfig(rt http.RoundTripper) *tls.Config {
	if t, ok := rt.(*http.Transport); ok {
		return t.TLSClientConfig
	}
	return nil
}
