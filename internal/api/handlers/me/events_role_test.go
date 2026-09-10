package me

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hubplay/internal/auth"
	"hubplay/internal/event"
	"hubplay/internal/testutil"
)

// newSSETestServerWithRole monta el handler SSE con claims inyectados en
// el contexto (como haría el middleware de auth) para el rol dado.
func newSSETestServerWithRole(t *testing.T, role string) (*event.Bus, *httptest.Server) {
	t.Helper()
	bus := event.NewBus(testutil.NopLogger())
	h := NewEventHandler(bus, nil, testutil.NopLogger())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := auth.WithClaims(r.Context(), &auth.Claims{UserID: "u1", Username: "u", Role: role})
		h.Stream(w, r.WithContext(ctx))
	}))
	t.Cleanup(srv.Close)
	return bus, srv
}

func openSSE(t *testing.T, url string) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		cancel()
		t.Fatalf("connect: %v", err)
	}
	return func() {
		cancel()
		_ = resp.Body.Close()
	}
}

// TestEventHandler_TorrentDownloadEvents_AdminOnly: los eventos de descarga
// de torrent revelan qué se está bajando y en la API REST son admin-only
// (GET /torrent/downloads); el SSE debe aplicar el mismo límite.
func TestEventHandler_TorrentDownloadEvents_AdminOnly(t *testing.T) {
	t.Run("user no se suscribe a TorrentDownload", func(t *testing.T) {
		bus, srv := newSSETestServerWithRole(t, "user")
		closeConn := openSSE(t, srv.URL)
		defer closeConn()
		waitForHandlerCount(t, bus, event.ItemAdded, 1)
		if n := bus.HandlerCount(event.TorrentDownload); n != 0 {
			t.Errorf("non-admin must not subscribe to TorrentDownload, got %d handlers", n)
		}
	})
	t.Run("admin sí", func(t *testing.T) {
		bus, srv := newSSETestServerWithRole(t, "admin")
		closeConn := openSSE(t, srv.URL)
		defer closeConn()
		waitForHandlerCount(t, bus, event.TorrentDownload, 1)
	})
}
