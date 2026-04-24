package iptv

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

// The EPG refresh used to have no per-library lock, so two concurrent calls
// would race on ReplaceForChannel for every matched channel. This test does
// NOT exercise the DB layer (that needs a real sqlite + migrations); it
// reaches into Service.refreshes directly to prove the gate works.

func TestService_RefreshEPG_SecondCallIsRejectedWhileFirstRuns(t *testing.T) {
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)

	// Seed a library with an EPG URL that points to a slow httptest server.
	// The slow server lets us catch the "in-progress" window.
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Sleep just long enough for the second caller to race us.
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><tv></tv>`))
	}))
	defer slow.Close()

	// Unblock loopback for the duration of this test — the proxy-security
	// guard is not involved here but the EPG fetcher still hits 127.0.0.1.
	unblockLoopback(t)

	lib := &db.Library{
		ID: "lib-1", Name: "L1", ContentType: "livetv", ScanMode: "manual",
		EPGURL:    slow.URL,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := repos.Libraries.Create(context.Background(), lib); err != nil {
		t.Fatalf("seed library: %v", err)
	}

	svc := NewService(repos.Channels, repos.EPGPrograms, repos.Libraries,
		repos.ChannelFavorites, repos.LibraryEPGSources,
		slog.New(slog.NewTextHandler(new(discard), nil)))

	var wg sync.WaitGroup
	var firstErr, secondErr error
	var firstCount, secondCount int
	wg.Add(1)
	go func() {
		defer wg.Done()
		firstCount, firstErr = svc.RefreshEPG(context.Background(), "lib-1")
	}()

	// Give the first refresh a head start so it's holding the gate.
	time.Sleep(50 * time.Millisecond)
	secondCount, secondErr = svc.RefreshEPG(context.Background(), "lib-1")
	wg.Wait()

	if secondErr == nil {
		t.Errorf("second concurrent EPG refresh should fail, got nil (count=%d)", secondCount)
	} else if !strings.Contains(secondErr.Error(), "already in progress") {
		t.Errorf("expected 'already in progress' error, got %v", secondErr)
	}
	if firstErr != nil {
		t.Logf("first refresh returned err (expected — empty XMLTV): %v", firstErr)
	}
	_ = firstCount
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
