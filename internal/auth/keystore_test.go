package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"hubplay/internal/auth"
	"hubplay/internal/clock"
	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/testutil"
)

// newKeystore wires a freshly-migrated DB + mock clock + bootstrapped
// keystore. Tests operate against the real repository rather than a fake
// because the keystore's cache-vs-DB consistency is a big part of what we
// want to lock down.
func newKeystore(t *testing.T, seed string) (*auth.KeyStore, *db.SigningKeyRepository, *clock.Mock) {
	t.Helper()
	database := testutil.NewTestDB(t)
	repo := db.NewSigningKeyRepository(database)
	clk := &clock.Mock{CurrentTime: time.Now().UTC()}
	ctx := context.Background()

	if _, err := auth.Bootstrap(ctx, repo, clk, seed); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	ks, err := auth.NewKeyStore(ctx, repo, clk)
	if err != nil {
		t.Fatalf("new keystore: %v", err)
	}
	return ks, repo, clk
}

func TestBootstrap_SeedsFromProvidedSecret(t *testing.T) {
	// Seeding with the config secret is what preserves token continuity
	// across the upgrade. If Bootstrap ever stops using `seed`, tokens
	// signed before the upgrade would start failing on deploy day.
	ks, _, _ := newKeystore(t, "config-secret-verbatim")

	primary, err := ks.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if primary.Secret != "config-secret-verbatim" {
		t.Errorf("seeded secret not used verbatim: got %q", primary.Secret)
	}
}

func TestBootstrap_EmptySeedGeneratesRandom(t *testing.T) {
	// A fresh install with no config secret should still get a valid primary
	// key, just randomly-generated. Assert non-empty and that two
	// independent bootstraps produce different secrets (entropy check).
	ks1, _, _ := newKeystore(t, "")
	k1, err := ks1.Current()
	if err != nil {
		t.Fatalf("k1: %v", err)
	}
	if k1.Secret == "" {
		t.Fatal("random bootstrap produced empty secret")
	}

	ks2, _, _ := newKeystore(t, "")
	k2, err := ks2.Current()
	if err != nil {
		t.Fatalf("k2: %v", err)
	}
	if k1.Secret == k2.Secret {
		t.Error("two random bootstraps collided — rand is broken or deterministic")
	}
}

func TestBootstrap_Idempotent(t *testing.T) {
	// Calling Bootstrap twice must not create two primaries; the second call
	// is a no-op that returns the existing key. main.go runs this on every
	// boot — any regression here would silently create keys at every start.
	database := testutil.NewTestDB(t)
	repo := db.NewSigningKeyRepository(database)
	clk := &clock.Mock{CurrentTime: time.Now().UTC()}
	ctx := context.Background()

	first, err := auth.Bootstrap(ctx, repo, clk, "seed")
	if err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}
	second, err := auth.Bootstrap(ctx, repo, clk, "different-seed")
	if err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("second bootstrap inserted new key: %q != %q", first.ID, second.ID)
	}
	all, _ := repo.ListAll(ctx)
	if len(all) != 1 {
		t.Errorf("expected 1 key after two bootstraps, got %d", len(all))
	}
}

func TestKeyStore_LookupFindsActiveAndRetired(t *testing.T) {
	ks, repo, clk := newKeystore(t, "seed")
	ctx := context.Background()

	// Rotate with a long overlap: the previous primary is retired in the
	// future, so it's still active *right now*.
	_, err := ks.Rotate(ctx, 1*time.Hour)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}

	all, _ := repo.ListAll(ctx)
	if len(all) != 2 {
		t.Fatalf("expected 2 keys after rotate, got %d", len(all))
	}

	// Every kid must resolve via Lookup — that's the validation contract.
	for _, k := range all {
		got, err := ks.Lookup(k.ID)
		if err != nil {
			t.Errorf("Lookup(%s) unexpected error: %v", k.ID, err)
			continue
		}
		if got.Secret != k.Secret {
			t.Errorf("Lookup(%s) returned wrong secret", k.ID)
		}
	}

	// Advance past the overlap and confirm the old key is still looked up
	// (it only moves to the retired map, not gone) — pruning removes it,
	// not time.
	clk.Advance(2 * time.Hour)
	if err := ks.Reload(ctx); err != nil {
		t.Fatalf("reload: %v", err)
	}
	for _, k := range all {
		if _, err := ks.Lookup(k.ID); err != nil {
			t.Errorf("Lookup(%s) after overlap: %v", k.ID, err)
		}
	}
}

func TestKeyStore_LookupUnknownReturnsNotFound(t *testing.T) {
	ks, _, _ := newKeystore(t, "seed")

	_, err := ks.Lookup("no-such-kid")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown kid must return ErrNotFound, got: %v", err)
	}
}

func TestKeyStore_RotateSetsNewPrimary(t *testing.T) {
	ks, _, _ := newKeystore(t, "seed")
	ctx := context.Background()

	before, _ := ks.Current()
	fresh, err := ks.Rotate(ctx, 15*time.Minute)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	after, _ := ks.Current()

	if fresh.ID != after.ID {
		t.Errorf("Rotate return value should equal the new Current: %q != %q", fresh.ID, after.ID)
	}
	if before.ID == after.ID {
		t.Error("primary did not advance after rotate")
	}
	if fresh.Secret == before.Secret {
		t.Error("rotation reused the previous secret — entropy broken")
	}
}

func TestKeyStore_RotateWithOverlapDelaysRetirement(t *testing.T) {
	// Business case: rotate with a 15-minute overlap equal to access token
	// TTL. The old key's retired_at should be *in the future* so operators
	// can schedule pruning later; prune-before-now leaves it untouched.
	ks, repo, clk := newKeystore(t, "seed")
	ctx := context.Background()

	before, _ := ks.Current()
	_, err := ks.Rotate(ctx, 15*time.Minute)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}

	oldRow, err := repo.GetByID(ctx, before.ID)
	if err != nil {
		t.Fatalf("get old key: %v", err)
	}
	if !oldRow.RetiredAt.Valid {
		t.Fatal("old key should have retired_at set")
	}
	wantRetireAt := clk.Now().Add(15 * time.Minute)
	if !oldRow.RetiredAt.Time.Equal(wantRetireAt) {
		t.Errorf("retired_at: got %v, want %v", oldRow.RetiredAt.Time, wantRetireAt)
	}

	// Prune at "now" must NOT reap the old key — its retired_at is in the
	// future.
	pruned, err := ks.Prune(ctx, clk.Now())
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if pruned != 0 {
		t.Errorf("prune before overlap elapsed should reap 0, got %d", pruned)
	}
}

func TestKeyStore_RotateZeroOverlapRetiresImmediately(t *testing.T) {
	// Compromised-key scenario: operator passes overlap <= 0 to cut every
	// in-flight token right now. Prune at "now" should immediately reap.
	ks, _, clk := newKeystore(t, "seed")
	ctx := context.Background()

	_, err := ks.Rotate(ctx, 0)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	// Nudge the clock forward 1ns so "<" in DeleteRetiredBefore matches the
	// retired_at=now row. Otherwise the cutoff equals retired_at and the
	// row stays (strict-less semantics are deliberate — see repository).
	clk.Advance(1 * time.Nanosecond)
	pruned, err := ks.Prune(ctx, clk.Now())
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if pruned != 1 {
		t.Errorf("prune with zero overlap should reap 1 immediately, got %d", pruned)
	}
}

func TestKeyStore_PruneRemovesAndReloadsCache(t *testing.T) {
	ks, _, clk := newKeystore(t, "seed")
	ctx := context.Background()

	oldPrimary, _ := ks.Current()
	_, err := ks.Rotate(ctx, 0)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}

	clk.Advance(1 * time.Second)
	if _, err := ks.Prune(ctx, clk.Now()); err != nil {
		t.Fatalf("prune: %v", err)
	}

	// After prune the old kid must no longer resolve — otherwise a pruned
	// (potentially leaked) key would still validate tokens.
	if _, err := ks.Lookup(oldPrimary.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("pruned kid should not resolve, got err=%v", err)
	}
	if got := ks.RetiredCount(); got != 0 {
		t.Errorf("retired cache not cleared after prune: got %d", got)
	}
}

func TestKeyStore_ConcurrentLookupsUnderRotation(t *testing.T) {
	// The validation hot path takes an RLock; rotation takes a full Lock.
	// Spin up readers alongside rotations and assert no data race fires.
	// Prefer -race in CI to actually exercise this.
	ks, _, _ := newKeystore(t, "seed")
	ctx := context.Background()

	primary, _ := ks.Current()
	done := make(chan struct{})

	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			_, _ = ks.Lookup(primary.ID)
			_, _ = ks.Current()
		}
	}()

	for i := 0; i < 3; i++ {
		if _, err := ks.Rotate(ctx, time.Minute); err != nil {
			t.Fatalf("rotate #%d: %v", i, err)
		}
	}

	<-done
}

func TestKeyStore_SnapshotFlagsPrimary(t *testing.T) {
	ks, _, _ := newKeystore(t, "seed")
	ctx := context.Background()
	_, err := ks.Rotate(ctx, time.Hour)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}

	snap := ks.Snapshot()
	var primaries int
	for _, e := range snap {
		if e.IsPrimary {
			primaries++
		}
	}
	if primaries != 1 {
		t.Errorf("exactly one snapshot entry must be marked primary, got %d", primaries)
	}
}
