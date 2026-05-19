package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/api/handlers"
	"hubplay/internal/auth"
	authmodel "hubplay/internal/auth/model"
)

// ─── Test double ────────────────────────────────────────────────────

type fakePermissionsStore struct {
	mu        sync.Mutex
	users     map[string]*authmodel.User
	setCalls  []struct {
		ID     string
		Column string
		Value  bool
	}
	transferCalls []struct{ From, To string }
	transferErr   error
}

func newStore(users ...*authmodel.User) *fakePermissionsStore {
	m := make(map[string]*authmodel.User, len(users))
	for _, u := range users {
		uCopy := *u
		m[u.ID] = &uCopy
	}
	return &fakePermissionsStore{users: m}
}

func (f *fakePermissionsStore) GetByID(_ context.Context, id string) (*authmodel.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[id]
	if !ok {
		return nil, errors.New("not found")
	}
	out := *u
	return &out, nil
}

func (f *fakePermissionsStore) SetPermission(_ context.Context, id, column string, value bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setCalls = append(f.setCalls, struct {
		ID     string
		Column string
		Value  bool
	}{id, column, value})
	u := f.users[id]
	if u == nil {
		return errors.New("not found")
	}
	switch column {
	case "can_manage_admins":
		u.CanManageAdmins = value
	case "can_manage_users":
		u.CanManageUsers = value
	case "can_manage_libraries":
		u.CanManageLibraries = value
	case "can_manage_iptv":
		u.CanManageIPTV = value
	case "can_edit_metadata":
		u.CanEditMetadata = value
	case "can_change_artwork":
		u.CanChangeArtwork = value
	case "can_view_audit":
		u.CanViewAudit = value
	case "can_upload":
		u.CanUpload = value
	}
	return nil
}

func (f *fakePermissionsStore) TransferOwnership(_ context.Context, from, to string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.transferCalls = append(f.transferCalls, struct{ From, To string }{from, to})
	if f.transferErr != nil {
		return f.transferErr
	}
	fromU := f.users[from]
	toU := f.users[to]
	if fromU == nil || toU == nil {
		return errors.New("not found")
	}
	fromU.IsOwner = false
	toU.IsOwner = true
	return nil
}

// ─── Helpers ────────────────────────────────────────────────────────

func mount(h *handlers.PermissionsHandler) http.Handler {
	r := chi.NewRouter()
	r.Get("/users/{id}/permissions", h.GetPermissions)
	r.Put("/users/{id}/permissions", h.PutPermissions)
	r.Post("/users/{id}/transfer-ownership", h.TransferOwnership)
	return r
}

// doRequest evita httptest.NewServer porque el cliente HTTP NO
// propaga r.Context() (los claims se inyectan vía context.WithValue
// y mueren al cruzar TCP). Sirvimos por debajo via ServeHTTP así los
// tests pueden simular "usuario autenticado X" inyectando claims.
func doRequest(handler http.Handler, method, path string, body io.Reader, requesterID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	if requesterID != "" {
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: requesterID}))
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func decodeBody(t *testing.T, body io.Reader) map[string]any {
	t.Helper()
	var wrapper struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(body).Decode(&wrapper); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return wrapper.Data
}

// ─── GetPermissions ─────────────────────────────────────────────────

func TestPermissionsHandler_GetPermissions(t *testing.T) {
	store := newStore(&authmodel.User{
		ID: "u-1", Role: "admin", IsActive: true,
		CanEditMetadata: true, CanViewAudit: true,
	})
	h := handlers.NewPermissionsHandler(store, slog.Default())

	rr := doRequest(mount(h), http.MethodGet, "/users/u-1/permissions", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	d := decodeBody(t, rr.Body)
	if d["can_edit_metadata"] != true {
		t.Errorf("can_edit_metadata = %v", d["can_edit_metadata"])
	}
	if d["can_manage_admins"] != false {
		t.Errorf("can_manage_admins = %v", d["can_manage_admins"])
	}
}

// ─── PutPermissions ─────────────────────────────────────────────────

func TestPutPermissions_OwnerCanGrantManageAdmins(t *testing.T) {
	store := newStore(
		&authmodel.User{ID: "u-owner", Role: "admin", IsActive: true, IsOwner: true},
		&authmodel.User{ID: "u-tgt", Role: "admin", IsActive: true},
	)
	h := handlers.NewPermissionsHandler(store, slog.Default())

	rr := doRequest(mount(h), http.MethodPut, "/users/u-tgt/permissions",
		strings.NewReader(`{"can_manage_admins": true, "can_edit_metadata": true}`), "u-owner")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	tgt, _ := store.GetByID(context.Background(), "u-tgt")
	if !tgt.CanManageAdmins {
		t.Error("can_manage_admins not set")
	}
	if !tgt.CanEditMetadata {
		t.Error("can_edit_metadata not set")
	}
}

func TestPutPermissions_NonOwnerCannotGrantManageAdmins(t *testing.T) {
	// requester tiene can_manage_admins pero NO es owner — el spec
	// dice que sólo el owner puede otorgar este flag a otros.
	store := newStore(
		&authmodel.User{ID: "u-admin", Role: "admin", IsActive: true, CanManageAdmins: true},
		&authmodel.User{ID: "u-tgt", Role: "admin", IsActive: true},
	)
	h := handlers.NewPermissionsHandler(store, slog.Default())

	rr := doRequest(mount(h), http.MethodPut, "/users/u-tgt/permissions",
		strings.NewReader(`{"can_manage_admins": true}`), "u-admin")
	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d (want 403), body %s", rr.Code, rr.Body.String())
	}
	tgt, _ := store.GetByID(context.Background(), "u-tgt")
	if tgt.CanManageAdmins {
		t.Error("can_manage_admins leaked")
	}
}

func TestPutPermissions_OtherFlagsByManageAdmins(t *testing.T) {
	// can_manage_admins (no owner) PUEDE otorgar flags distintos a
	// can_manage_admins.
	store := newStore(
		&authmodel.User{ID: "u-admin", Role: "admin", IsActive: true, CanManageAdmins: true},
		&authmodel.User{ID: "u-tgt", Role: "admin", IsActive: true},
	)
	h := handlers.NewPermissionsHandler(store, slog.Default())

	rr := doRequest(mount(h), http.MethodPut, "/users/u-tgt/permissions",
		strings.NewReader(`{"can_edit_metadata": true, "can_change_artwork": true}`), "u-admin")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	tgt, _ := store.GetByID(context.Background(), "u-tgt")
	if !tgt.CanEditMetadata || !tgt.CanChangeArtwork {
		t.Errorf("flags not applied: %+v", tgt)
	}
}

func TestPutPermissions_RejectsOwnerTarget(t *testing.T) {
	store := newStore(
		&authmodel.User{ID: "u-other", Role: "admin", IsActive: true, IsOwner: true},
		&authmodel.User{ID: "u-owner", Role: "admin", IsActive: true, IsOwner: true},
	)
	// Note: store no enforce unicidad — es un fake. La cuestión que
	// testeamos es que el HANDLER niega tocar a un usuario con
	// is_owner=true, no la unicidad SQL.
	h := handlers.NewPermissionsHandler(store, slog.Default())

	rr := doRequest(mount(h), http.MethodPut, "/users/u-owner/permissions",
		strings.NewReader(`{"can_edit_metadata": false}`), "u-other")
	if rr.Code != http.StatusForbidden {
		t.Errorf("status %d (want 403) body %s", rr.Code, rr.Body.String())
	}
}

func TestPutPermissions_RejectsNonAdminTarget(t *testing.T) {
	store := newStore(
		&authmodel.User{ID: "u-owner", Role: "admin", IsActive: true, IsOwner: true},
		&authmodel.User{ID: "u-plain", Role: "user", IsActive: true},
	)
	h := handlers.NewPermissionsHandler(store, slog.Default())

	rr := doRequest(mount(h), http.MethodPut, "/users/u-plain/permissions",
		strings.NewReader(`{"can_edit_metadata": true}`), "u-owner")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status %d (want 400)", rr.Code)
	}
}

// ─── TransferOwnership ──────────────────────────────────────────────

func TestTransferOwnership_HappyPath(t *testing.T) {
	store := newStore(
		&authmodel.User{ID: "u-owner", Role: "admin", IsActive: true, IsOwner: true},
		&authmodel.User{ID: "u-target", Role: "admin", IsActive: true},
	)
	h := handlers.NewPermissionsHandler(store, slog.Default())

	rr := doRequest(mount(h), http.MethodPost, "/users/u-target/transfer-ownership",
		bytes.NewBufferString(`{"new_owner_id":"u-target","confirmation":"TRANSFER"}`), "u-owner")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	if len(store.transferCalls) != 1 {
		t.Errorf("transfer calls = %d", len(store.transferCalls))
	}
}

func TestTransferOwnership_RequiresConfirmation(t *testing.T) {
	store := newStore(
		&authmodel.User{ID: "u-owner", Role: "admin", IsActive: true, IsOwner: true},
		&authmodel.User{ID: "u-target", Role: "admin", IsActive: true},
	)
	h := handlers.NewPermissionsHandler(store, slog.Default())

	for _, body := range []string{
		`{"new_owner_id":"u-target"}`,
		`{"new_owner_id":"u-target","confirmation":"yes"}`,
	} {
		rr := doRequest(mount(h), http.MethodPost, "/users/u-target/transfer-ownership",
			strings.NewReader(body), "u-owner")
		if rr.Code != http.StatusBadRequest {
			t.Errorf("body %s → status %d (want 400)", body, rr.Code)
		}
	}
	if len(store.transferCalls) != 0 {
		t.Errorf("transfer called despite bad confirmation: %d", len(store.transferCalls))
	}
}

func TestTransferOwnership_RejectsSelf(t *testing.T) {
	store := newStore(&authmodel.User{ID: "u-owner", Role: "admin", IsActive: true, IsOwner: true})
	h := handlers.NewPermissionsHandler(store, slog.Default())

	rr := doRequest(mount(h), http.MethodPost, "/users/u-owner/transfer-ownership",
		strings.NewReader(`{"new_owner_id":"u-owner","confirmation":"TRANSFER"}`), "u-owner")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status %d (want 400)", rr.Code)
	}
}
