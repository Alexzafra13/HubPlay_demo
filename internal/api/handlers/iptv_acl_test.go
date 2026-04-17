package handlers

import (
	"context"
	"net/http"
	"testing"
	"time"

	"hubplay/internal/auth"
	"hubplay/internal/db"
)

// Reuses iptvFakeAccess / iptvTestEnv from iptv_test.go.

// userClaims returns non-admin Claims — exercises the ACL branch, not the
// admin bypass.
func iptvUserClaims() *auth.Claims {
	return &auth.Claims{UserID: "user-viewer", Role: "user"}
}

// ─── Unauthenticated ────────────────────────────────────────────────────────

func TestIPTVHandler_ListChannels_Unauthenticated_404(t *testing.T) {
	env := newIPTVTestEnv(t)
	rr := env.doAs(http.MethodGet, "/api/v1/libraries/lib-1/channels", "", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", rr.Code)
	}
}

// ─── User without access ────────────────────────────────────────────────────

func TestIPTVHandler_ListChannels_UserWithoutAccess_404(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.access.accessFn = func(_, libraryID string) (bool, error) {
		return libraryID == "lib-ok", nil
	}
	// Put some channels in the restricted library so we can be certain the
	// ACL — not an empty list — is what blocks the response.
	env.svc.channels["lib-restricted"] = []*db.Channel{
		{ID: "c-secret", LibraryID: "lib-restricted", IsActive: true},
	}
	rr := env.doAs(http.MethodGet, "/api/v1/libraries/lib-restricted/channels", "", iptvUserClaims())
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404 (channel-exists leak)", rr.Code)
	}
}

func TestIPTVHandler_ListChannels_UserWithAccess_200(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.access.accessFn = func(_, libraryID string) (bool, error) { return libraryID == "lib-ok", nil }
	env.svc.channels["lib-ok"] = []*db.Channel{{ID: "c-1", LibraryID: "lib-ok", IsActive: true}}
	rr := env.doAs(http.MethodGet, "/api/v1/libraries/lib-ok/channels", "", iptvUserClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rr.Code)
	}
}

func TestIPTVHandler_GetChannel_UserWithoutAccess_404(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channelByID["c-secret"] = &db.Channel{ID: "c-secret", LibraryID: "lib-restricted", IsActive: true}
	env.access.accessFn = func(_, libraryID string) (bool, error) { return libraryID == "lib-ok", nil }
	rr := env.doAs(http.MethodGet, "/api/v1/channels/c-secret", "", iptvUserClaims())
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", rr.Code)
	}
}

func TestIPTVHandler_Stream_UserWithoutAccess_404_NoProxyInvocation(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channelByID["c-secret"] = &db.Channel{
		ID: "c-secret", LibraryID: "lib-restricted", IsActive: true, StreamURL: "http://x/y",
	}
	env.access.accessFn = func(_, libraryID string) (bool, error) { return libraryID == "lib-ok", nil }
	proxyInvoked := false
	env.proxy.streamFn = func(http.ResponseWriter, string, string) error {
		proxyInvoked = true
		return nil
	}
	rr := env.doAs(http.MethodGet, "/api/v1/channels/c-secret/stream", "", iptvUserClaims())
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", rr.Code)
	}
	if proxyInvoked {
		t.Fatal("proxy.ProxyStream was invoked despite ACL denial — upstream leak")
	}
}

func TestIPTVHandler_ProxyURL_UserWithoutAccess_404(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channelByID["c-secret"] = &db.Channel{ID: "c-secret", LibraryID: "lib-restricted"}
	env.access.accessFn = func(_, libraryID string) (bool, error) { return libraryID == "lib-ok", nil }
	proxyInvoked := false
	env.proxy.urlFn = func(http.ResponseWriter, string, string) error {
		proxyInvoked = true
		return nil
	}
	rr := env.doAs(http.MethodGet, "/api/v1/channels/c-secret/proxy?url=https://x/y", "", iptvUserClaims())
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", rr.Code)
	}
	if proxyInvoked {
		t.Fatal("proxy.ProxyURL invoked despite ACL denial")
	}
}

func TestIPTVHandler_Schedule_UserWithoutAccess_404(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channelByID["c-secret"] = &db.Channel{ID: "c-secret", LibraryID: "lib-restricted"}
	env.access.accessFn = func(_, libraryID string) (bool, error) { return libraryID == "lib-ok", nil }
	rr := env.doAs(http.MethodGet, "/api/v1/channels/c-secret/schedule", "", iptvUserClaims())
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", rr.Code)
	}
}

func TestIPTVHandler_Groups_UserWithoutAccess_404(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.access.accessFn = func(_, libraryID string) (bool, error) { return libraryID == "lib-ok", nil }
	rr := env.doAs(http.MethodGet, "/api/v1/libraries/lib-restricted/channels/groups", "", iptvUserClaims())
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", rr.Code)
	}
}

// ─── BulkSchedule: per-channel filter ───────────────────────────────────────

func TestIPTVHandler_BulkSchedule_FiltersInaccessibleChannels(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channelByID["c-ok"] = &db.Channel{ID: "c-ok", LibraryID: "lib-ok"}
	env.svc.channelByID["c-blocked"] = &db.Channel{ID: "c-blocked", LibraryID: "lib-restricted"}
	env.access.accessFn = func(_, libraryID string) (bool, error) { return libraryID == "lib-ok", nil }

	var passedIDs []string
	env.svc.bulkFn = func(_ context.Context, ids []string, _, _ time.Time) (map[string][]*db.EPGProgram, error) {
		passedIDs = ids
		return map[string][]*db.EPGProgram{}, nil
	}

	rr := env.doAs(http.MethodGet, "/api/v1/iptv/schedule?channels=c-ok,c-blocked,c-ghost", "", iptvUserClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rr.Code)
	}
	// Only c-ok should reach the service — c-blocked is filtered, c-ghost is unknown.
	if len(passedIDs) != 1 || passedIDs[0] != "c-ok" {
		t.Fatalf("bulk filter: got %v want [c-ok]", passedIDs)
	}
}

// ─── Admin bypass ───────────────────────────────────────────────────────────

func TestIPTVHandler_AdminBypassesACL(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channelByID["c-any"] = &db.Channel{ID: "c-any", LibraryID: "lib-any"}
	// Access service returns false for everyone — but admin bypasses.
	env.access.accessFn = func(_, _ string) (bool, error) { return false, nil }

	rr := env.doAs(http.MethodGet, "/api/v1/channels/c-any", "",
		&auth.Claims{UserID: "root", Role: "admin"})
	if rr.Code != http.StatusOK {
		t.Fatalf("admin should bypass ACL, got %d", rr.Code)
	}
}
