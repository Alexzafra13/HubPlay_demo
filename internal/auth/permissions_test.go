package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"hubplay/internal/auth"
	authmodel "hubplay/internal/auth/model"
)

type fakeStore struct {
	user *authmodel.User
	err  error
}

func (f *fakeStore) GetByID(_ context.Context, _ string) (*authmodel.User, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.user, nil
}

func handlerOK() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
}

func runWithClaims(t *testing.T, mw func(http.Handler) http.Handler, claims *auth.Claims) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	if claims != nil {
		req = req.WithContext(auth.WithClaims(req.Context(), claims))
	}
	rr := httptest.NewRecorder()
	mw(handlerOK()).ServeHTTP(rr, req)
	return rr.Code
}

// ─── Require(perm) ──────────────────────────────────────────────────

func TestRequire_OwnerPassesAnything(t *testing.T) {
	store := &fakeStore{user: &authmodel.User{ID: "u-owner", IsActive: true, IsOwner: true}}
	pc := auth.NewPermissionChecker(store)
	for _, p := range authmodel.AllPermissions() {
		code := runWithClaims(t, pc.Require(p), &auth.Claims{UserID: "u-owner"})
		if code != http.StatusNoContent {
			t.Errorf("owner blocked on %s (code=%d)", p, code)
		}
	}
}

func TestRequire_PassesWhenFlagOn(t *testing.T) {
	store := &fakeStore{user: &authmodel.User{
		ID: "u-1", IsActive: true, CanEditMetadata: true,
	}}
	pc := auth.NewPermissionChecker(store)
	code := runWithClaims(t, pc.Require(authmodel.PermEditMetadata), &auth.Claims{UserID: "u-1"})
	if code != http.StatusNoContent {
		t.Errorf("flag on but blocked (code=%d)", code)
	}
}

func TestRequire_RejectsWhenFlagOff(t *testing.T) {
	store := &fakeStore{user: &authmodel.User{
		ID: "u-1", IsActive: true, CanEditMetadata: false,
	}}
	pc := auth.NewPermissionChecker(store)
	code := runWithClaims(t, pc.Require(authmodel.PermEditMetadata), &auth.Claims{UserID: "u-1"})
	if code != http.StatusForbidden {
		t.Errorf("flag off but passed (code=%d)", code)
	}
}

func TestRequire_RejectsInactiveUser(t *testing.T) {
	store := &fakeStore{user: &authmodel.User{
		ID: "u-1", IsActive: false, IsOwner: true,
	}}
	pc := auth.NewPermissionChecker(store)
	code := runWithClaims(t, pc.Require(authmodel.PermManageUsers), &auth.Claims{UserID: "u-1"})
	if code != http.StatusForbidden {
		t.Errorf("inactive owner passed (code=%d)", code)
	}
}

func TestRequire_RejectsNoClaims(t *testing.T) {
	store := &fakeStore{user: &authmodel.User{ID: "u-1", IsActive: true, IsOwner: true}}
	pc := auth.NewPermissionChecker(store)
	code := runWithClaims(t, pc.Require(authmodel.PermManageUsers), nil)
	if code != http.StatusUnauthorized {
		t.Errorf("missing claims accepted (code=%d)", code)
	}
}

func TestRequire_RejectsLookupFailure(t *testing.T) {
	store := &fakeStore{err: errors.New("db down")}
	pc := auth.NewPermissionChecker(store)
	code := runWithClaims(t, pc.Require(authmodel.PermManageUsers), &auth.Claims{UserID: "u-1"})
	if code != http.StatusInternalServerError {
		t.Errorf("lookup failure not surfaced (code=%d)", code)
	}
}

// ─── RequireOwner ───────────────────────────────────────────────────

func TestRequireOwner_PassesOwner(t *testing.T) {
	store := &fakeStore{user: &authmodel.User{ID: "u-1", IsActive: true, IsOwner: true}}
	pc := auth.NewPermissionChecker(store)
	code := runWithClaims(t, pc.RequireOwner, &auth.Claims{UserID: "u-1"})
	if code != http.StatusNoContent {
		t.Errorf("owner blocked (code=%d)", code)
	}
}

func TestRequireOwner_RejectsAdminWithAllFlags(t *testing.T) {
	store := &fakeStore{user: &authmodel.User{
		ID: "u-1", IsActive: true,
		Role:                "admin",
		CanManageAdmins:     true,
		CanManageUsers:      true,
		CanManageLibraries:  true,
		CanManageIPTV:       true,
		CanEditMetadata:     true,
		CanChangeArtwork:    true,
		CanViewAudit:        true,
	}}
	pc := auth.NewPermissionChecker(store)
	code := runWithClaims(t, pc.RequireOwner, &auth.Claims{UserID: "u-1"})
	if code != http.StatusForbidden {
		t.Errorf("super-admin slipped through owner gate (code=%d)", code)
	}
}
