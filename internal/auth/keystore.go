package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"hubplay/internal/clock"
	"hubplay/internal/db"
	"hubplay/internal/domain"
)

// signingKeyRepo is the tiny slice of db.SigningKeyRepository the keystore
// needs. Declaring it here keeps the auth package testable with a fake repo
// and decoupled from SQL details.
type signingKeyRepo interface {
	Insert(ctx context.Context, k *db.SigningKey) error
	GetByID(ctx context.Context, id string) (*db.SigningKey, error)
	ListActive(ctx context.Context) ([]*db.SigningKey, error)
	ListAll(ctx context.Context) ([]*db.SigningKey, error)
	SetRetiredAt(ctx context.Context, id string, retiredAt time.Time) error
	DeleteRetiredBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// KeyStore owns the set of JWT signing keys. It caches active keys in memory
// so every token validation is a map lookup rather than a SQL query; the
// cache is rebuilt on every mutation (Rotate, Retire, Prune) and once on
// startup via Reload.
//
// Concurrency: a read-write mutex guards the cache. Validation (hot path)
// only takes a read lock; rotations are rare and take the write lock.
type KeyStore struct {
	repo  signingKeyRepo
	clock clock.Clock

	mu       sync.RWMutex
	active   map[string]*db.SigningKey // kid → key, includes overlap keys
	primary  *db.SigningKey            // most recent active key
	retired  map[string]*db.SigningKey // kid → retired key (still useful until pruned)
}

// NewKeyStore creates a keystore and loads the current key set. Returns an
// error if the table is empty — callers bootstrap first via Bootstrap.
func NewKeyStore(ctx context.Context, repo signingKeyRepo, clk clock.Clock) (*KeyStore, error) {
	ks := &KeyStore{repo: repo, clock: clk}
	if err := ks.Reload(ctx); err != nil {
		return nil, err
	}
	if ks.primary == nil {
		return nil, fmt.Errorf("keystore: no active signing key — call Bootstrap first")
	}
	return ks, nil
}

// Bootstrap inserts a first key with the provided secret, if and only if the
// table is empty. This is how we seed the DB from config.Auth.JWTSecret on
// the very first run; subsequent calls are no-ops so it is safe to invoke
// unconditionally at startup.
//
// A non-empty seed is used verbatim (so existing tokens signed with the
// config secret keep validating). An empty seed falls back to a fresh
// random key.
func Bootstrap(ctx context.Context, repo signingKeyRepo, clk clock.Clock, seed string) (*db.SigningKey, error) {
	existing, err := repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		// Already seeded — return the current primary so callers can log it
		// without re-running bootstrap logic.
		return existing[0], nil
	}

	secret := seed
	if secret == "" {
		secret, err = randomSecret()
		if err != nil {
			return nil, err
		}
	}
	k := &db.SigningKey{
		ID:        uuid.New().String(),
		Secret:    secret,
		CreatedAt: clk.Now(),
	}
	if err := repo.Insert(ctx, k); err != nil {
		return nil, err
	}
	return k, nil
}

// Reload rebuilds the in-memory cache from the database. Called once at
// startup and again after every Rotate/Retire/Prune so callers never see
// stale state.
func (ks *KeyStore) Reload(ctx context.Context) error {
	active, err := ks.repo.ListActive(ctx)
	if err != nil {
		return err
	}
	all, err := ks.repo.ListAll(ctx)
	if err != nil {
		return err
	}

	activeMap := make(map[string]*db.SigningKey, len(active))
	for _, k := range active {
		activeMap[k.ID] = k
	}
	retiredMap := make(map[string]*db.SigningKey)
	for _, k := range all {
		if k.RetiredAt.Valid {
			retiredMap[k.ID] = k
		}
	}

	ks.mu.Lock()
	defer ks.mu.Unlock()
	ks.active = activeMap
	ks.retired = retiredMap
	if len(active) > 0 {
		ks.primary = active[0] // ListActive is newest-first
	} else {
		ks.primary = nil
	}
	return nil
}

// Current returns the primary signing key used for new tokens.
func (ks *KeyStore) Current() (*db.SigningKey, error) {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	if ks.primary == nil {
		return nil, fmt.Errorf("keystore: no primary key")
	}
	return ks.primary, nil
}

// Lookup resolves a kid to its key for validation. Active keys win over
// retired ones; retired keys still resolve so in-flight tokens keep working
// until the admin prunes them. Returns domain.ErrNotFound when the kid is
// unknown, so callers can render a 401 consistently.
func (ks *KeyStore) Lookup(kid string) (*db.SigningKey, error) {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	if k, ok := ks.active[kid]; ok {
		return k, nil
	}
	if k, ok := ks.retired[kid]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("kid %q: %w", kid, domain.ErrNotFound)
}

// Rotate mints a new primary key. The previous primary stays active (still
// signing? no, only validating) for the overlap window so in-flight tokens
// signed with it continue to validate; once the overlap has elapsed it is
// retired with retired_at=now+overlap, which the admin or pruner can later
// reap. An overlap <= 0 retires the previous primary immediately, which is
// what you want when you suspect the key was compromised.
//
// Returns the new primary key.
func (ks *KeyStore) Rotate(ctx context.Context, overlap time.Duration) (*db.SigningKey, error) {
	secret, err := randomSecret()
	if err != nil {
		return nil, err
	}
	now := ks.clock.Now()
	k := &db.SigningKey{
		ID:        uuid.New().String(),
		Secret:    secret,
		CreatedAt: now,
	}
	if err := ks.repo.Insert(ctx, k); err != nil {
		return nil, err
	}

	// Retire every previously-active key. If overlap > 0 we postdate the
	// retirement so the pruner leaves them in place until the overlap
	// elapses; with overlap <= 0 we retire them right now (compromised key
	// scenario: no overlap, cut every in-flight session).
	prev, err := ks.Current() // still points at the OLD primary at this point
	if err == nil {
		retireAt := now
		if overlap > 0 {
			retireAt = now.Add(overlap)
		}
		if err := ks.repo.SetRetiredAt(ctx, prev.ID, retireAt); err != nil {
			return nil, fmt.Errorf("rotate: retire previous: %w", err)
		}
	}

	if err := ks.Reload(ctx); err != nil {
		return nil, err
	}
	return k, nil
}

// Prune deletes every key retired before the cutoff. Typically called with
// clock.Now() to reap anything whose retirement date has elapsed. Returns
// the number of keys removed.
func (ks *KeyStore) Prune(ctx context.Context, cutoff time.Time) (int64, error) {
	n, err := ks.repo.DeleteRetiredBefore(ctx, cutoff)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		if err := ks.Reload(ctx); err != nil {
			return n, err
		}
	}
	return n, nil
}

// Snapshot returns the current set of keys (active + retired) for admin UIs
// and metrics reporting. Secrets are redacted so log lines and API
// responses cannot leak them even if a future refactor forgets to scrub.
type KeySnapshot struct {
	ID        string
	CreatedAt time.Time
	RetiredAt *time.Time
	IsPrimary bool
}

func (ks *KeyStore) Snapshot() []KeySnapshot {
	ks.mu.RLock()
	defer ks.mu.RUnlock()

	out := make([]KeySnapshot, 0, len(ks.active)+len(ks.retired))
	for _, k := range ks.active {
		entry := KeySnapshot{
			ID:        k.ID,
			CreatedAt: k.CreatedAt,
			IsPrimary: ks.primary != nil && ks.primary.ID == k.ID,
		}
		if k.RetiredAt.Valid {
			t := k.RetiredAt.Time
			entry.RetiredAt = &t
		}
		out = append(out, entry)
	}
	for _, k := range ks.retired {
		t := k.RetiredAt.Time
		out = append(out, KeySnapshot{
			ID:        k.ID,
			CreatedAt: k.CreatedAt,
			RetiredAt: &t,
		})
	}
	return out
}

// ActiveCount / RetiredCount are tiny helpers for the metrics layer. They
// avoid exposing the internal maps while staying O(1).
func (ks *KeyStore) ActiveCount() int {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	return len(ks.active)
}

func (ks *KeyStore) RetiredCount() int {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	return len(ks.retired)
}

// randomSecret generates 32 bytes of cryptographic randomness, hex-encoded
// (so it fits cleanly in TEXT columns and log lines if it ever leaks).
func randomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("signing key entropy: %w", err)
	}
	return hex.EncodeToString(b), nil
}
