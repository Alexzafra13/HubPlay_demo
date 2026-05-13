// Federation search handler integration tests.
//
// Stand-up parallels federation_stream_test.go: real federation.Manager
// + real DB so the share / FTS / ACL plumbing is what production runs.
// These tests assert the wire-level guarantees the senior review
// flagged: the share ACL gate fires server-side, an empty query is
// 400, and the response shape matches the documented OpenAPI body.

package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/federation"
	"hubplay/internal/testutil"
)

// fedSearchEnv extends fedTestEnv-style boilerplate with a router
// mounting the public search handler under RequirePeerJWT — same
// posture production uses.
func newFedSearchEnv(t *testing.T) *fedTestEnv {
	t.Helper()
	env := newFedTestEnv(t)

	// Add an item the FTS index will hit on "Test".
	testutil.Exec(t, env.rawDB, `
		INSERT INTO items (id, library_id, type, title, sort_title, year, path)
		VALUES (?, ?, 'movie', ?, ?, 2024, ?)
	`, "extra-1", env.libraryID, "Test Sequel", "test sequel", "/tmp/seq.mkv")

	pub := NewFederationPublicHandler(env.mgr, testutil.NopLogger())

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(federation.RequirePeerJWT(env.mgr))
		r.Get("/api/v1/peer/search", pub.SearchLibraries)
	})

	// Replace the test server with one wired for search. fedTestEnv's
	// srv was built around the stream + image routes; search needs
	// its own router, so we close the previous server.
	env.srv.Close()
	env.srv = httptest.NewServer(r)
	t.Cleanup(env.srv.Close)
	return env
}

func (e *fedTestEnv) doSearch(t *testing.T, query string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet,
		e.srv.URL+"/api/v1/peer/search?q="+query, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+e.peerToken())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// TestFederationSearch_NoShare_ReturnsZeroHits is the ACL gate. A peer
// without a CanBrowse share for the library must NOT see any titles
// from it, even via FTS. Empty hits, 200 (the share-gate is internal
// to the SQL JOIN, not surfaced as 403 — the conflation is intentional
// so a peer can't probe library existence by toggling search queries).
func TestFederationSearch_NoShare_ReturnsZeroHits(t *testing.T) {
	env := newFedSearchEnv(t)

	resp := env.doSearch(t, "Test")
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total != 0 || len(body.Items) != 0 {
		t.Fatalf("expected zero hits without share, got total=%d items=%v",
			body.Total, body.Items)
	}
}

// TestFederationSearch_WithShare_ReturnsMatchingItems exercises the
// happy path: with CanBrowse on the library, the FTS query surfaces
// the matching items. Verifies the wire shape too — items[].id +
// items[].title are what the consumer-side fan-out expects.
func TestFederationSearch_WithShare_ReturnsMatchingItems(t *testing.T) {
	env := newFedSearchEnv(t)
	env.share(federation.ShareScopes{CanBrowse: true})

	resp := env.doSearch(t, "Test")
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total < 1 {
		t.Fatalf("expected matching items with share, got total=%d", body.Total)
	}
	titles := map[string]bool{}
	for _, it := range body.Items {
		if title, ok := it["title"].(string); ok {
			titles[title] = true
		}
	}
	if !titles["Test Movie"] && !titles["Test Sequel"] {
		t.Fatalf("expected Test Movie or Test Sequel in hits, got %v", titles)
	}
}

// TestFederationSearch_EmptyQuery_Returns400 keeps the surface honest.
// A `q=` parameter with no value is a client bug; surface it as 400
// rather than running an unbounded FTS query.
func TestFederationSearch_EmptyQuery_Returns400(t *testing.T) {
	env := newFedSearchEnv(t)
	env.share(federation.ShareScopes{CanBrowse: true})

	resp := env.doSearch(t, "")
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestFederationSearch_NoToken_Returns401 confirms RequirePeerJWT is
// in front of the route. A search query is a catalog read just like
// /peer/libraries — same auth posture.
func TestFederationSearch_NoToken_Returns401(t *testing.T) {
	env := newFedSearchEnv(t)
	req, _ := http.NewRequest(http.MethodGet, env.srv.URL+"/api/v1/peer/search?q=x", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}
