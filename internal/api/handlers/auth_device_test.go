package handlers_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/api/handlers"
	"hubplay/internal/auth"
	"hubplay/internal/clock"
	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/event"
	"hubplay/internal/testutil"
)

// deviceAuthEnv wires a real DeviceCodeService against the in-memory
// testdb plus a real event.Bus so the SSE handler exercises the full
// publish → subscribe path, not a mock. The httptest.Server gives
// proper streaming flushes (httptest.ResponseRecorder does not
// implement Flusher).
type deviceAuthEnv struct {
	svc     *auth.DeviceCodeService
	authSvc *auth.Service
	user    *db.User
	bus     *event.Bus
	server  *httptest.Server
	authCfg config.AuthConfig
}

func newDeviceAuthEnv(t *testing.T) *deviceAuthEnv {
	t.Helper()
	database := testutil.NewTestDB(t)
	userRepo := db.NewUserRepository("sqlite", database)
	sessionRepo := db.NewSessionRepository("sqlite", database)
	keyRepo := db.NewSigningKeyRepository("sqlite", database)
	codeRepo := db.NewDeviceCodeRepository("sqlite", database)

	cfg := config.AuthConfig{
		JWTSecret:          "test-secret-32-bytes-long-enough!",
		BCryptCost:         10,
		AccessTokenTTL:     15 * time.Minute,
		RefreshTokenTTL:    720 * time.Hour,
		MaxSessionsPerUser: 5,
	}
	clk := &clock.Mock{CurrentTime: time.Now().UTC()}
	ctx := context.Background()
	if _, err := auth.Bootstrap(ctx, keyRepo, clk, cfg.JWTSecret); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	keyStore, err := auth.NewKeyStore(ctx, keyRepo, clk)
	if err != nil {
		t.Fatalf("keystore: %v", err)
	}
	authSvc := auth.NewService(userRepo, sessionRepo, keyStore, cfg, clk, slog.Default())

	bus := event.NewBus(slog.Default())
	authSvc.SetEventBus(bus)

	devSvc := auth.NewDeviceCodeService(authSvc, codeRepo, userRepo, slog.Default())

	user, err := authSvc.Register(ctx, auth.RegisterRequest{
		Username:    "alice",
		Password:    "correcthorsebatterystaple",
		DisplayName: "Alice",
		Role:        "user",
	})
	if err != nil {
		t.Fatalf("register user: %v", err)
	}

	h := handlers.NewDeviceAuthHandler(devSvc, nil, cfg, bus, nil, slog.Default())

	r := chi.NewRouter()
	r.Post("/auth/device/start", h.Start)
	r.Post("/auth/device/poll", h.Poll)
	r.Get("/auth/device/events", h.Events)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return &deviceAuthEnv{
		svc:     devSvc,
		authSvc: authSvc,
		user:    user,
		bus:     bus,
		server:  srv,
		authCfg: cfg,
	}
}

// TestDeviceAuthHandler_StartIncludesVerificationURIComplete pins the
// RFC 8628 §3.3.1 extension the pairing UI relies on: the response
// MUST include a verification_uri_complete with the user_code already
// in the query so a QR encoding it lands on /link with the input
// pre-filled.
func TestDeviceAuthHandler_StartIncludesVerificationURIComplete(t *testing.T) {
	env := newDeviceAuthEnv(t)

	resp, err := http.Post(env.server.URL+"/auth/device/start", "application/json",
		bytes.NewBufferString(`{"device_name":"Living-room TV"}`))
	if err != nil {
		t.Fatalf("POST /start: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status: got %d, want 201", resp.StatusCode)
	}
	var body struct {
		Data struct {
			UserCode                string `json:"user_code"`
			VerificationURL         string `json:"verification_url"`
			VerificationURI         string `json:"verification_uri"`
			VerificationURIComplete string `json:"verification_uri_complete"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Data.UserCode == "" || body.Data.VerificationURL == "" {
		t.Fatal("user_code and verification_url must be present")
	}
	if !strings.Contains(body.Data.VerificationURIComplete, "code="+body.Data.UserCode) {
		t.Errorf("verification_uri_complete missing code query: %q (expected to contain code=%q)",
			body.Data.VerificationURIComplete, body.Data.UserCode)
	}
	if body.Data.VerificationURI != body.Data.VerificationURL {
		t.Errorf("verification_uri should mirror verification_url: %q vs %q",
			body.Data.VerificationURI, body.Data.VerificationURL)
	}
}

// TestDeviceAuthHandler_PollSetsCookies pins that a successful poll
// (operator already approved) writes the access + refresh cookies in
// the same response that carries the JSON tokens. The in-app pairing
// UI relies on this to swap to a logged-in state without exposing
// tokens to JavaScript.
func TestDeviceAuthHandler_PollSetsCookies(t *testing.T) {
	env := newDeviceAuthEnv(t)
	ctx := context.Background()

	pair, err := env.svc.StartDevice(ctx, "TV", "https://example.com/link")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := env.svc.ApproveDevice(ctx, pair.UserCode, env.user.ID); err != nil {
		t.Fatalf("approve: %v", err)
	}

	body := strings.NewReader(`{"device_code":"` + pair.DeviceCode + `"}`)
	// Advance well past the slow-down gap before polling — the
	// service uses a real clock here because the handler does not
	// expose a clock injection.
	time.Sleep(50 * time.Millisecond)
	resp, err := http.Post(env.server.URL+"/auth/device/poll", "application/json", body)
	if err != nil {
		t.Fatalf("POST /poll: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d, want 200 (body=%s)", resp.StatusCode, raw)
	}

	var hasAccess, hasRefresh bool
	for _, c := range resp.Cookies() {
		switch c.Name {
		case "hubplay_access":
			hasAccess = c.Value != ""
		case "hubplay_refresh":
			hasRefresh = c.Value != ""
		}
	}
	if !hasAccess || !hasRefresh {
		t.Errorf("cookies missing: access=%v refresh=%v (Set-Cookie headers: %v)",
			hasAccess, hasRefresh, resp.Header.Values("Set-Cookie"))
	}
}

// TestDeviceAuthHandler_EventsStreamApproved pins the SSE happy path
// the in-app pairing UI consumes: open the stream, see "pending", get
// "approved" within the timeout once ApproveDevice fires on the
// service. The browser-side counterpart then calls /poll once — this
// test stops after the SSE event arrives.
func TestDeviceAuthHandler_EventsStreamApproved(t *testing.T) {
	env := newDeviceAuthEnv(t)
	ctx := context.Background()

	pair, err := env.svc.StartDevice(ctx, "TV", "https://example.com/link")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet,
		env.server.URL+"/auth/device/events?device_code="+pair.DeviceCode, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("SSE status: got %d, want 200 (body=%s)", resp.StatusCode, raw)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("content-type: got %q, want text/event-stream", ct)
	}

	// Race the approve against the SSE consumer. The handler emits
	// "pending" immediately on connect, then publishes "approved" the
	// moment the bus fires. Trigger Approve in a goroutine so the
	// scanner below blocks on the read until both events have landed.
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = env.svc.ApproveDevice(ctx, pair.UserCode, env.user.ID)
	}()

	events, err := readSSEUntil(t, resp.Body, "approved", 3*time.Second)
	if err != nil {
		t.Fatalf("waiting for approved event: %v (events seen: %v)", err, events)
	}
	if !contains(events, "pending") {
		t.Errorf("did not observe initial pending event; saw %v", events)
	}
	if !contains(events, "approved") {
		t.Errorf("did not observe approved event; saw %v", events)
	}
}

// TestDeviceAuthHandler_EventsUnknownDeviceCode_404 keeps the
// pre-stream validation honest. A typo or stale code lands as a
// proper 404 before any SSE headers leave so the client can render an
// error state instead of staring at an empty stream.
func TestDeviceAuthHandler_EventsUnknownDeviceCode_404(t *testing.T) {
	env := newDeviceAuthEnv(t)

	resp, err := http.Get(env.server.URL + "/auth/device/events?device_code=deadbeef")
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", resp.StatusCode)
	}
}

// readSSEUntil reads event-stream framing until it sees the named
// terminal event or `timeout` elapses. Returns the list of event
// names observed for assertions.
func readSSEUntil(t *testing.T, r io.Reader, terminal string, timeout time.Duration) ([]string, error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 4096), 64*1024)
	type result struct {
		events []string
		err    error
	}
	done := make(chan result, 1)
	go func() {
		var seen []string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				name := strings.TrimPrefix(line, "event: ")
				seen = append(seen, name)
				if name == terminal {
					done <- result{seen, nil}
					return
				}
			}
		}
		done <- result{seen, scanner.Err()}
	}()
	select {
	case res := <-done:
		return res.events, res.err
	case <-time.After(time.Until(deadline)):
		return nil, context.DeadlineExceeded
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
