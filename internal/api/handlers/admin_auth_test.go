package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hubplay/internal/api/handlers"
	"hubplay/internal/auth"
	"hubplay/internal/clock"
	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

func newAdminAuthFixture(t *testing.T) (*handlers.AdminAuthHandler, *auth.KeyStore, *clock.Mock, *[]string) {
	t.Helper()
	database := testutil.NewTestDB(t)
	repo := db.NewSigningKeyRepository(database)
	clk := &clock.Mock{CurrentTime: time.Now().UTC()}
	ctx := context.Background()

	if _, err := auth.Bootstrap(ctx, repo, clk, "seed-secret"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	ks, err := auth.NewKeyStore(ctx, repo, clk)
	if err != nil {
		t.Fatalf("new keystore: %v", err)
	}

	var observed []string
	observer := func(outcome string) { observed = append(observed, outcome) }

	h := handlers.NewAdminAuthHandler(ks, clk.Now, observer, slog.Default())
	return h, ks, clk, &observed
}

func TestAdminAuth_ListKeys_IncludesPrimaryFlagNoSecret(t *testing.T) {
	// The response must never contain secrets — only metadata an admin
	// needs to reason about the keyset. Exactly one key must be flagged
	// primary for the request to render a useful UI.
	h, _, _, _ := newAdminAuthFixture(t)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/auth/keys", nil)
	h.ListKeys(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d", rr.Code)
	}

	// Secrets never travel over the wire.
	if strings.Contains(rr.Body.String(), "seed-secret") {
		t.Errorf("list response leaked secret: %s", rr.Body.String())
	}

	var body struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data) != 1 {
		t.Fatalf("expected 1 key after bootstrap, got %d", len(body.Data))
	}
	if body.Data[0]["is_primary"] != true {
		t.Errorf("sole key must be primary, got %v", body.Data[0])
	}
}

func TestAdminAuth_Rotate_DefaultOverlap(t *testing.T) {
	// An empty body triggers the safe default overlap. The response must
	// echo both the new key id and the overlap that was applied — the
	// operator is going to want to double-check that in the audit trail.
	h, ks, _, observed := newAdminAuthFixture(t)
	before, _ := ks.Current()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/auth/keys/rotate", nil)
	h.Rotate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d: %s", rr.Code, rr.Body.String())
	}

	var body struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	after, _ := ks.Current()
	if body.Data["id"] != after.ID {
		t.Errorf("response id must match new primary: got %v, want %q", body.Data["id"], after.ID)
	}
	if before.ID == after.ID {
		t.Error("keystore primary did not advance after rotate")
	}
	if body.Data["overlap_seconds"] == nil {
		t.Error("response missing overlap_seconds")
	}

	if len(*observed) != 1 || (*observed)[0] != "success" {
		t.Errorf("rotation observer: got %v, want [success]", *observed)
	}
}

func TestAdminAuth_Rotate_ExplicitZeroOverlap(t *testing.T) {
	// Zero overlap is the compromised-key path: the old key must be retired
	// immediately so a subsequent prune reaps it. Trust the handler to pass
	// the value straight through to the keystore.
	h, ks, clk, _ := newAdminAuthFixture(t)

	body := `{"overlap_seconds": 0}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/rotate", bytes.NewBufferString(body))
	req.ContentLength = int64(len(body))
	h.Rotate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d", rr.Code)
	}

	clk.Advance(1 * time.Nanosecond)
	pruned, err := ks.Prune(context.Background(), clk.Now())
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if pruned != 1 {
		t.Errorf("with zero overlap prune should reap 1 key, got %d", pruned)
	}
}

func TestAdminAuth_Rotate_InvalidJSONReturns400(t *testing.T) {
	h, _, _, observed := newAdminAuthFixture(t)

	body := `not json`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/rotate", bytes.NewBufferString(body))
	req.ContentLength = int64(len(body))
	h.Rotate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rr.Code)
	}
	// A malformed body must NOT count as either a success or a failure
	// rotation — no actual rotation was attempted.
	if len(*observed) != 0 {
		t.Errorf("observer fired on invalid input: %v", *observed)
	}
}

func TestAdminAuth_Prune_ReportsCount(t *testing.T) {
	h, ks, clk, _ := newAdminAuthFixture(t)

	// Rotate with zero overlap so the old key is instantly retirable.
	if _, err := ks.Rotate(context.Background(), 0); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	clk.Advance(1 * time.Second)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/prune", nil)
	h.Prune(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d", rr.Code)
	}
	var body struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Data["pruned"].(float64) != 1 {
		t.Errorf("pruned count: got %v, want 1", body.Data["pruned"])
	}
}
