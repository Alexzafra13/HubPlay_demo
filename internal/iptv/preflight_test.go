package iptv

import (
	"context"
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

// newPreflightSvc builds a real Service against an in-memory test
// DB so the preflight code path uses the same httpClient + insecure
// client paths as production. The DB is unused by PreflightCheck
// but Service construction needs the repos.
func newPreflightSvc(t *testing.T) *Service {
	t.Helper()
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)
	return NewService(repos.Channels, repos.EPGPrograms, repos.Libraries,
		repos.ChannelFavorites, repos.LibraryEPGSources, repos.ChannelOverrides,
		repos.ChannelWatchHistory,
		slog.New(slog.NewTextHandler(new(discard), nil)))
}

func TestPreflight_OK_StandardM3U(t *testing.T) {
	unblockLoopback(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-mpegURL")
		fmt.Fprint(w, `#EXTM3U url-tvg="http://x/epg.xml"
#EXTINF:-1 tvg-id="es.tve1" group-title="Spain",La 1
http://upstream.example/la1.m3u8
`)
	}))
	defer srv.Close()

	svc := newPreflightSvc(t)
	res := svc.PreflightCheck(context.Background(), srv.URL, false)

	if res.Status != PreflightOK {
		t.Fatalf("status = %s, want ok; full = %+v", res.Status, res)
	}
	if res.HTTPStatus != 200 {
		t.Errorf("http_status = %d, want 200", res.HTTPStatus)
	}
	if !strings.HasPrefix(res.BodyHint, "#EXTM3U") {
		t.Errorf("body_hint = %q, want #EXTM3U prefix", res.BodyHint)
	}
}

// Some providers skip the #EXTM3U header but still emit valid
// EXTINF entries. Accept that — the parser handles it.
func TestPreflight_OK_NoHeader_StartsWithExtinf(t *testing.T) {
	unblockLoopback(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `#EXTINF:-1,Channel
http://x/c.m3u8
`)
	}))
	defer srv.Close()

	svc := newPreflightSvc(t)
	res := svc.PreflightCheck(context.Background(), srv.URL, false)
	if res.Status != PreflightOK {
		t.Errorf("missing-header playlist should be ok, got %s (%s)", res.Status, res.Message)
	}
}

// Provider returns an HTML error page (account suspended / IP block).
// Our M3U parser would refuse this; the preflight surfaces the cause
// so the operator sees it before clicking Save.
func TestPreflight_HTML_ErrorPage(t *testing.T) {
	unblockLoopback(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!DOCTYPE html><html><body><h1>Account suspended</h1></body></html>`)
	}))
	defer srv.Close()

	svc := newPreflightSvc(t)
	res := svc.PreflightCheck(context.Background(), srv.URL, false)
	if res.Status != PreflightHTML {
		t.Fatalf("status = %s, want html", res.Status)
	}
	if !strings.Contains(res.Message, "HTML") {
		t.Errorf("message should mention HTML; got %q", res.Message)
	}
	if !strings.HasPrefix(res.BodyHint, "<") {
		t.Errorf("body_hint should start with '<', got %q", res.BodyHint)
	}
}

// 401 / 403 → auth verdict so the UI can say "credenciales rechazadas".
func TestPreflight_Auth_401(t *testing.T) {
	unblockLoopback(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	svc := newPreflightSvc(t)
	res := svc.PreflightCheck(context.Background(), srv.URL, false)
	if res.Status != PreflightAuth {
		t.Errorf("status = %s, want auth", res.Status)
	}
	if res.HTTPStatus != http.StatusUnauthorized {
		t.Errorf("http_status = %d, want 401", res.HTTPStatus)
	}
}

func TestPreflight_NotFound_404(t *testing.T) {
	unblockLoopback(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	svc := newPreflightSvc(t)
	res := svc.PreflightCheck(context.Background(), srv.URL, false)
	if res.Status != PreflightNotFound {
		t.Errorf("status = %s, want not_found", res.Status)
	}
}

// 200 OK with empty body is the "account assigned but no channels"
// edge case. Different from html — different remediation.
func TestPreflight_Empty_200OK(t *testing.T) {
	unblockLoopback(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Force Content-Length: 0 so io.CopyN sees nothing.
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newPreflightSvc(t)
	res := svc.PreflightCheck(context.Background(), srv.URL, false)
	if res.Status != PreflightEmpty {
		t.Errorf("status = %s, want empty", res.Status)
	}
}

// Self-signed HTTPS without tlsInsecure → tls verdict so the UI can
// suggest the toggle.
func TestPreflight_TLS_SelfSignedRejected(t *testing.T) {
	unblockLoopback(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "#EXTM3U\n")
	}))
	defer srv.Close()

	svc := newPreflightSvc(t)
	res := svc.PreflightCheck(context.Background(), srv.URL, false)
	if res.Status != PreflightTLS {
		t.Errorf("status = %s, want tls; message = %s", res.Status, res.Message)
	}
}

// Same self-signed server WITH tlsInsecure → ok. The toggle works
// at the preflight surface as well as at the import surface.
func TestPreflight_TLS_InsecureBypass(t *testing.T) {
	unblockLoopback(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "#EXTM3U\n#EXTINF:-1,Foo\nhttp://x/y.m3u8\n")
	}))
	defer srv.Close()

	svc := newPreflightSvc(t)
	res := svc.PreflightCheck(context.Background(), srv.URL, true)
	if res.Status != PreflightOK {
		t.Errorf("status = %s, want ok; message = %s", res.Status, res.Message)
	}
}

// Server accepts the connection but never responds — verify the
// preflight returns "slow" with a useful message instead of bubbling
// up a generic timeout error.
func TestPreflight_Slow_ConnectButNoResponse(t *testing.T) {
	unblockLoopback(t)
	// Hold the connection open until the test finishes — context
	// cancellation will abort the per-request handler.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	svc := newPreflightSvc(t)
	// 1s budget overrides preflightBudget — cleaner than waiting 12s
	// in the test. We assert via context, not via the const.
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	res := svc.PreflightCheck(ctx, srv.URL, false)
	if res.Status != PreflightSlow {
		t.Errorf("status = %s, want slow; message = %s", res.Status, res.Message)
	}
}

// DNS failure → dns verdict.
func TestPreflight_DNS_Unresolvable(t *testing.T) {
	svc := newPreflightSvc(t)
	res := svc.PreflightCheck(context.Background(),
		"http://this-host-definitely-does-not-exist.invalid/playlist.m3u", false)
	if res.Status != PreflightDNS {
		t.Errorf("status = %s, want dns", res.Status)
	}
}

// Bad URL surface — file:// and missing scheme both bounce.
func TestPreflight_InvalidURL(t *testing.T) {
	svc := newPreflightSvc(t)
	for _, in := range []string{"file:///etc/passwd", "ftp://x/y", "not a url", ""} {
		res := svc.PreflightCheck(context.Background(), in, false)
		if res.Status != PreflightInvalidURL {
			t.Errorf("for %q: status = %s, want invalid_url", in, res.Status)
		}
	}
}

// Big content-length triggers the size-warning hint inside the OK
// message — same hint the user will see for their 331 MB list.
func TestPreflight_OK_LargeContentLength_WarnsInMessage(t *testing.T) {
	unblockLoopback(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Lie about content-length to trigger the threshold without
		// allocating a real 200 MB body.
		w.Header().Set("Content-Length", "350000000")
		w.Header().Set("Content-Type", "application/x-mpegURL")
		fmt.Fprint(w, "#EXTM3U\n#EXTINF:-1,X\nhttp://x/y.m3u8\n")
	}))
	defer srv.Close()

	svc := newPreflightSvc(t)
	res := svc.PreflightCheck(context.Background(), srv.URL, false)
	if res.Status != PreflightOK {
		t.Fatalf("status = %s, want ok", res.Status)
	}
	if !strings.Contains(res.Message, "MB") {
		t.Errorf("large content-length should warn in message; got %q", res.Message)
	}
}
