package media

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/auth"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/stream"
	"hubplay/internal/testutil"
)

// fakeLibraryAccess is a per-(user,library) ACL oracle. Missing entries
// deny; a non-nil err is returned verbatim so the fail-closed-on-error
// path can be exercised.
type fakeLibraryAccess struct {
	allow map[string]map[string]bool
	err   error
}

func (f *fakeLibraryAccess) UserHasAccess(_ context.Context, userID, libraryID string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.allow[userID][libraryID], nil
}

// ListForUser lets fakeLibraryAccess double as a media.LibraryACL for the
// cross-library list/search tests.
func (f *fakeLibraryAccess) ListForUser(_ context.Context, userID string) ([]*librarymodel.Library, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []*librarymodel.Library
	for lib, ok := range f.allow[userID] {
		if ok {
			out = append(out, &librarymodel.Library{ID: lib})
		}
	}
	return out, nil
}

// aclTestEnv wires a StreamHandler with a real access gate and a single
// item that lives in "lib-restricted".
type aclTestEnv struct {
	t       *testing.T
	manager *fakeStreamManager
	access  *fakeLibraryAccess
	router  chi.Router
}

func newACLTestEnv(t *testing.T, access *fakeLibraryAccess) *aclTestEnv {
	t.Helper()
	mgr := newFakeStreamManager()
	items := &streamFakeItemRepo{byID: map[string]*librarymodel.Item{
		"item-1": {
			ID:            "item-1",
			LibraryID:     "lib-restricted",
			Type:          "movie",
			Title:         "Secret",
			DurationTicks: 60 * 10_000_000,
			Path:          "/tmp/does-not-matter.mkv",
			Container:     "mkv",
			IsAvailable:   true,
		},
	}}
	streams := &streamFakeMediaStreamRepo{byItem: map[string][]*librarymodel.MediaStream{}}
	h := NewStreamHandler(mgr, items, streams, nil, nil, access, nil, "http://test", testutil.NopLogger())

	r := chi.NewRouter()
	r.Route("/api/v1/stream/{itemId}", func(r chi.Router) {
		r.Get("/info", h.Info)
		r.Get("/master.m3u8", h.MasterPlaylist)
		r.Get("/{quality}/index.m3u8", h.QualityPlaylist)
		r.Get("/direct", h.DirectPlay)
		r.Get("/subtitles", h.Subtitles)
		r.Get("/subtitles/{trackIndex}", h.SubtitleTrack)
	})
	return &aclTestEnv{t: t, manager: mgr, access: access, router: r}
}

func (e *aclTestEnv) get(path string, claims *auth.Claims) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if claims != nil {
		req = req.WithContext(auth.WithClaims(req.Context(), claims))
	}
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

// gatedPaths are every content-exposing endpoint that must enforce the
// per-library ACL. Segment is intentionally absent: it is reachable only
// through a session that QualityPlaylist creates, and QualityPlaylist is
// gated here, so the session never exists for an unauthorised caller.
var gatedPaths = []string{
	"/api/v1/stream/item-1/info",
	"/api/v1/stream/item-1/master.m3u8",
	"/api/v1/stream/item-1/720p/index.m3u8",
	"/api/v1/stream/item-1/direct",
	"/api/v1/stream/item-1/subtitles",
	"/api/v1/stream/item-1/subtitles/0",
}

func TestStreamACL_UserWithoutAccess_GetsNotFoundEverywhere(t *testing.T) {
	env := newACLTestEnv(t, &fakeLibraryAccess{allow: map[string]map[string]bool{}})
	bob := &auth.Claims{UserID: "bob", Role: "user"}

	for _, p := range gatedPaths {
		rr := env.get(p, bob)
		if rr.Code != http.StatusNotFound {
			t.Errorf("%s: got %d, want 404 (ACL deny)", p, rr.Code)
		}
	}
}

func TestStreamACL_NoSessionCreatedWhenDenied(t *testing.T) {
	env := newACLTestEnv(t, &fakeLibraryAccess{allow: map[string]map[string]bool{}})
	started := false
	env.manager.startSessionFn = func(context.Context, stream.StartSessionRequest) (*stream.ManagedSession, error) {
		started = true
		return nil, nil
	}

	rr := env.get("/api/v1/stream/item-1/720p/index.m3u8", &auth.Claims{UserID: "bob", Role: "user"})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", rr.Code)
	}
	if started {
		t.Fatal("StartSession was called for an unauthorised user — transcode session leaked before the ACL gate")
	}
}

func TestStreamACL_UnauthenticatedDenied(t *testing.T) {
	env := newACLTestEnv(t, &fakeLibraryAccess{allow: map[string]map[string]bool{}})
	rr := env.get("/api/v1/stream/item-1/info", nil)
	if rr.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404 for missing claims", rr.Code)
	}
}

func TestStreamACL_GrantedUserAllowed(t *testing.T) {
	env := newACLTestEnv(t, &fakeLibraryAccess{allow: map[string]map[string]bool{
		"alice": {"lib-restricted": true},
	}})
	rr := env.get("/api/v1/stream/item-1/info", &auth.Claims{UserID: "alice", Role: "user"})
	if rr.Code != http.StatusOK {
		t.Errorf("granted user: got %d, want 200", rr.Code)
	}
}

func TestStreamACL_AdminBypassesGrant(t *testing.T) {
	// Admin has no explicit grant but must still pass.
	env := newACLTestEnv(t, &fakeLibraryAccess{allow: map[string]map[string]bool{}})
	rr := env.get("/api/v1/stream/item-1/info", &auth.Claims{UserID: "root", Role: "admin"})
	if rr.Code != http.StatusOK {
		t.Errorf("admin: got %d, want 200", rr.Code)
	}
}

func TestStreamACL_ErrorFailsClosed(t *testing.T) {
	// A transient ACL lookup error must deny, never widen access.
	env := newACLTestEnv(t, &fakeLibraryAccess{err: context.DeadlineExceeded})
	rr := env.get("/api/v1/stream/item-1/direct", &auth.Claims{UserID: "alice", Role: "user"})
	if rr.Code != http.StatusNotFound {
		t.Errorf("ACL error: got %d, want 404 (fail closed)", rr.Code)
	}
}
