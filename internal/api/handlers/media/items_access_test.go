package media

import (
	"context"
	"net/http"
	"testing"

	"hubplay/internal/auth"
	librarymodel "hubplay/internal/library/model"
)

// Covers the per-library ACL on the local VOD metadata surface: item
// detail, children, and cross-library search. fakeLibraryAccess (defined
// in stream_access_test.go) doubles as a media.LibraryACL.

func TestItemDetailACL_DeniedUserGets404(t *testing.T) {
	access := &fakeLibraryAccess{allow: map[string]map[string]bool{}}
	env := newItemTestEnvWithAccess(t, access)
	env.svc.getItemFn = func(_ context.Context, id string) (*librarymodel.Item, error) {
		return &librarymodel.Item{ID: id, LibraryID: "lib-secret", Type: "movie", Title: "X"}, nil
	}
	env.svc.getChildrenFn = func(_ context.Context, _ string) ([]*librarymodel.Item, error) { return nil, nil }

	bob := &auth.Claims{UserID: "bob", Role: "user"}
	for _, p := range []string{"/api/v1/items/item-1/", "/api/v1/items/item-1/children"} {
		rr := env.doWithClaims(http.MethodGet, p, bob)
		if rr.Code != http.StatusNotFound {
			t.Errorf("%s: got %d, want 404", p, rr.Code)
		}
	}
}

func TestItemDetailACL_GrantedUserAllowed(t *testing.T) {
	access := &fakeLibraryAccess{allow: map[string]map[string]bool{"alice": {"lib-a": true}}}
	env := newItemTestEnvWithAccess(t, access)
	env.svc.getItemFn = func(_ context.Context, id string) (*librarymodel.Item, error) {
		return &librarymodel.Item{ID: id, LibraryID: "lib-a", Type: "movie", Title: "X"}, nil
	}
	rr := env.doWithClaims(http.MethodGet, "/api/v1/items/item-1/", &auth.Claims{UserID: "alice", Role: "user"})
	if rr.Code != http.StatusOK {
		t.Fatalf("granted Get: got %d, want 200, body %s", rr.Code, rr.Body.String())
	}
}

func TestItemDetailACL_AdminBypasses(t *testing.T) {
	access := &fakeLibraryAccess{allow: map[string]map[string]bool{}}
	env := newItemTestEnvWithAccess(t, access)
	env.svc.getItemFn = func(_ context.Context, id string) (*librarymodel.Item, error) {
		return &librarymodel.Item{ID: id, LibraryID: "lib-secret", Type: "movie", Title: "X"}, nil
	}
	rr := env.doWithClaims(http.MethodGet, "/api/v1/items/item-1/", &auth.Claims{UserID: "root", Role: "admin"})
	if rr.Code != http.StatusOK {
		t.Fatalf("admin Get: got %d, want 200", rr.Code)
	}
}

func TestItemSearchACL_ScopesToAccessibleLibraries(t *testing.T) {
	access := &fakeLibraryAccess{allow: map[string]map[string]bool{"alice": {"lib-a": true}}}
	env := newItemTestEnvWithAccess(t, access)
	var gotFilter librarymodel.ItemFilter
	env.svc.listItemsFn = func(_ context.Context, f librarymodel.ItemFilter) ([]*librarymodel.Item, int, error) {
		gotFilter = f
		return nil, 0, nil
	}
	rr := env.doWithClaims(http.MethodGet, "/api/v1/items/search?q=foo", &auth.Claims{UserID: "alice", Role: "user"})
	if rr.Code != http.StatusOK {
		t.Fatalf("search: got %d, want 200", rr.Code)
	}
	if len(gotFilter.LibraryIDs) != 1 || gotFilter.LibraryIDs[0] != "lib-a" {
		t.Fatalf("search LibraryIDs = %v, want [lib-a]", gotFilter.LibraryIDs)
	}
}

func TestItemSearchACL_NoAccessReturnsEmpty(t *testing.T) {
	access := &fakeLibraryAccess{allow: map[string]map[string]bool{}}
	env := newItemTestEnvWithAccess(t, access)
	env.svc.listItemsFn = func(_ context.Context, _ librarymodel.ItemFilter) ([]*librarymodel.Item, int, error) {
		t.Fatal("ListItems must not run when the caller has no library access")
		return nil, 0, nil
	}
	rr := env.doWithClaims(http.MethodGet, "/api/v1/items/search?q=foo", &auth.Claims{UserID: "bob", Role: "user"})
	if rr.Code != http.StatusOK {
		t.Fatalf("no-access search: got %d, want 200", rr.Code)
	}
}
