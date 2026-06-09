package libhandler

import (
	"context"
	"net/http"
	"testing"

	librarymodel "hubplay/internal/library/model"
)

// These tests cover the per-library ACL added to the local VOD surface:
// a non-admin without a grant must not read a library or its items, and
// the cross-library endpoints must be scoped to the caller's grants.

func TestLibraryHandler_Get_DeniesUngrantedUser(t *testing.T) {
	t.Parallel()
	env := newLibTestEnv(t)
	env.svc.getByIDFn = func(_ context.Context, id string) (*librarymodel.Library, error) {
		return &librarymodel.Library{ID: id, Name: "Secret"}, nil
	}
	env.svc.userHasAccessFn = func(_ context.Context, _, _ string) (bool, error) { return false, nil }

	rr := env.do(http.MethodGet, "/api/v1/libraries/lib-1", "", userClaims())
	if rr.Code != http.StatusNotFound {
		t.Fatalf("ungranted Get: got %d, want 404", rr.Code)
	}
}

func TestLibraryHandler_Get_AllowsGrantedUserAndAdmin(t *testing.T) {
	t.Parallel()
	env := newLibTestEnv(t)
	env.svc.getByIDFn = func(_ context.Context, id string) (*librarymodel.Library, error) {
		return &librarymodel.Library{ID: id, Name: "Secret"}, nil
	}
	env.svc.itemCountFn = func(_ context.Context, _ string) (int, error) { return 0, nil }

	// Granted non-admin.
	env.svc.userHasAccessFn = func(_ context.Context, _, _ string) (bool, error) { return true, nil }
	if rr := env.do(http.MethodGet, "/api/v1/libraries/lib-1", "", userClaims()); rr.Code != http.StatusOK {
		t.Fatalf("granted Get: got %d, want 200", rr.Code)
	}

	// Admin bypasses the grant (UserHasAccess must not even be consulted).
	env.svc.userHasAccessFn = func(_ context.Context, _, _ string) (bool, error) {
		t.Fatal("admin must bypass UserHasAccess")
		return false, nil
	}
	if rr := env.do(http.MethodGet, "/api/v1/libraries/lib-1", "", adminClaims()); rr.Code != http.StatusOK {
		t.Fatalf("admin Get: got %d, want 200", rr.Code)
	}
}

func TestLibraryHandler_Items_DeniesUngrantedUser(t *testing.T) {
	t.Parallel()
	env := newLibTestEnv(t)
	env.svc.userHasAccessFn = func(_ context.Context, _, _ string) (bool, error) { return false, nil }
	env.svc.listItemsFn = func(_ context.Context, _ librarymodel.ItemFilter) ([]*librarymodel.Item, int, error) {
		t.Fatal("ListItems must not run for an ungranted caller")
		return nil, 0, nil
	}
	rr := env.do(http.MethodGet, "/api/v1/libraries/lib-1/items", "", userClaims())
	if rr.Code != http.StatusNotFound {
		t.Fatalf("ungranted Items: got %d, want 404", rr.Code)
	}
}

func TestLibraryHandler_AllItems_ScopesToAccessibleLibraries(t *testing.T) {
	t.Parallel()
	env := newLibTestEnv(t)
	env.svc.listForUserFn = func(_ context.Context, _ string) ([]*librarymodel.Library, error) {
		return []*librarymodel.Library{{ID: "lib-allowed"}}, nil
	}
	var gotFilter librarymodel.ItemFilter
	env.svc.listItemsFn = func(_ context.Context, f librarymodel.ItemFilter) ([]*librarymodel.Item, int, error) {
		gotFilter = f
		return nil, 0, nil
	}

	rr := env.do(http.MethodGet, "/api/v1/items?limit=10", "", userClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("AllItems: got %d, want 200", rr.Code)
	}
	if len(gotFilter.LibraryIDs) != 1 || gotFilter.LibraryIDs[0] != "lib-allowed" {
		t.Fatalf("AllItems filter LibraryIDs = %v, want [lib-allowed]", gotFilter.LibraryIDs)
	}
}

func TestLibraryHandler_AllItems_NoAccessReturnsEmpty(t *testing.T) {
	t.Parallel()
	env := newLibTestEnv(t)
	env.svc.listForUserFn = func(_ context.Context, _ string) ([]*librarymodel.Library, error) {
		return nil, nil // zero grants
	}
	env.svc.listItemsFn = func(_ context.Context, _ librarymodel.ItemFilter) ([]*librarymodel.Item, int, error) {
		t.Fatal("ListItems must not run when the caller has no library access")
		return nil, 0, nil
	}
	rr := env.do(http.MethodGet, "/api/v1/items?limit=10", "", userClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("AllItems no-access: got %d, want 200 (empty)", rr.Code)
	}
}

func TestLibraryHandler_AllItems_AdminUnrestricted(t *testing.T) {
	t.Parallel()
	env := newLibTestEnv(t)
	env.svc.listForUserFn = func(_ context.Context, _ string) ([]*librarymodel.Library, error) {
		t.Fatal("admin must not be scoped via ListForUser")
		return nil, nil
	}
	var gotFilter librarymodel.ItemFilter
	env.svc.listItemsFn = func(_ context.Context, f librarymodel.ItemFilter) ([]*librarymodel.Item, int, error) {
		gotFilter = f
		return nil, 0, nil
	}
	rr := env.do(http.MethodGet, "/api/v1/items?limit=10", "", adminClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("admin AllItems: got %d, want 200", rr.Code)
	}
	if gotFilter.LibraryIDs != nil {
		t.Fatalf("admin AllItems must be unrestricted, got LibraryIDs=%v", gotFilter.LibraryIDs)
	}
}

func TestLibraryHandler_LatestItems_DeniesUngrantedSpecificLibrary(t *testing.T) {
	t.Parallel()
	env := newLibTestEnv(t)
	env.svc.listForUserFn = func(_ context.Context, _ string) ([]*librarymodel.Library, error) {
		return []*librarymodel.Library{{ID: "other"}}, nil
	}
	env.svc.latestFn = func(_ context.Context, _, _ string, _ int) ([]*librarymodel.Item, error) {
		t.Fatal("LatestItems must not run for an ungranted specific library")
		return nil, nil
	}
	rr := env.do(http.MethodGet, "/api/v1/libraries/latest-items?library_id=lib-1&type=movie", "", userClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 (empty rail)", rr.Code)
	}
}

// Ensure the claim-less rig path (auth handled upstream) still passes.
func TestLibraryHandler_Get_AnonymousPassesThrough(t *testing.T) {
	t.Parallel()
	env := newLibTestEnv(t)
	env.svc.getByIDFn = func(_ context.Context, id string) (*librarymodel.Library, error) {
		return &librarymodel.Library{ID: id, Name: "X"}, nil
	}
	env.svc.itemCountFn = func(_ context.Context, _ string) (int, error) { return 0, nil }
	env.svc.userHasAccessFn = func(_ context.Context, _, _ string) (bool, error) {
		t.Fatal("nil-claims request must not consult UserHasAccess")
		return false, nil
	}
	if rr := env.do(http.MethodGet, "/api/v1/libraries/lib-1", "", nil); rr.Code != http.StatusOK {
		t.Fatalf("anonymous Get: got %d, want 200", rr.Code)
	}
}
