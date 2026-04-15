package auth

import (
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/domain"
)

// newTestKey builds a signing key with a stable id and secret for assertions
// that need a deterministic kid in the token header.
func newTestKey(id, secret string) *db.SigningKey {
	return &db.SigningKey{ID: id, Secret: secret, CreatedAt: time.Unix(0, 0)}
}

// resolverFor returns a keyResolver that recognises the provided keys and
// fails with domain.ErrNotFound for any other kid — exactly the semantics
// the real KeyStore.Lookup exposes.
func resolverFor(keys ...*db.SigningKey) keyResolver {
	index := make(map[string]*db.SigningKey, len(keys))
	for _, k := range keys {
		index[k.ID] = k
	}
	return func(kid string) (*db.SigningKey, error) {
		if k, ok := index[kid]; ok {
			return k, nil
		}
		return nil, domain.ErrNotFound
	}
}

func TestGenerateAndValidateAccessToken(t *testing.T) {
	key := newTestKey("kid-1", "test-secret-32-bytes-long-enough!")

	token, expiresAt, err := generateAccessToken(key, "user-123", "alex", "admin", 15*time.Minute, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token == "" {
		t.Fatal("token should not be empty")
	}
	if expiresAt.Before(time.Now()) {
		t.Error("expiresAt should be in the future")
	}

	claims, err := validateAccessToken(resolverFor(key), token)
	if err != nil {
		t.Fatalf("unexpected error validating: %v", err)
	}
	if claims.UserID != "user-123" {
		t.Errorf("expected user ID 'user-123', got %q", claims.UserID)
	}
	if claims.Username != "alex" {
		t.Errorf("expected username 'alex', got %q", claims.Username)
	}
	if claims.Role != "admin" {
		t.Errorf("expected role 'admin', got %q", claims.Role)
	}
}

func TestValidateAccessToken_Expired(t *testing.T) {
	key := newTestKey("kid-1", "test-secret-32-bytes-long-enough!")

	// Generate token that expired 1 minute ago.
	token, _, err := generateAccessToken(key, "user-123", "alex", "user", -1*time.Minute, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = validateAccessToken(resolverFor(key), token)
	if err == nil {
		t.Error("expected error for expired token")
	}
}

func TestValidateAccessToken_WrongSecret(t *testing.T) {
	// Same kid, different secret → signature mismatch.
	signWith := newTestKey("kid-1", "test-secret-32-bytes-long-enough!")
	validateWith := newTestKey("kid-1", "a-completely-different-secret!!!")

	token, _, err := generateAccessToken(signWith, "user-123", "alex", "user", 15*time.Minute, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = validateAccessToken(resolverFor(validateWith), token)
	if err == nil {
		t.Error("expected error when resolver returns a key with a different secret")
	}
}

func TestValidateAccessToken_UnknownKid(t *testing.T) {
	// Token signed with kid-a; resolver only knows kid-b. The missing kid
	// must fail validation, otherwise a retired/rotated-away key could
	// still be accepted.
	signWith := newTestKey("kid-a", "test-secret-32-bytes-long-enough!")
	other := newTestKey("kid-b", "some-other-secret-32-bytes-long!!")

	token, _, err := generateAccessToken(signWith, "user-123", "alex", "user", 15*time.Minute, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = validateAccessToken(resolverFor(other), token)
	if err == nil {
		t.Error("expected error for unknown kid")
	}
}

func TestValidateAccessToken_Tampered(t *testing.T) {
	key := newTestKey("kid-1", "test-secret-32-bytes-long-enough!")
	token, _, err := generateAccessToken(key, "user-123", "alex", "user", 15*time.Minute, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tampered := token[:len(token)-5] + "XXXXX"
	_, err = validateAccessToken(resolverFor(key), tampered)
	if err == nil {
		t.Error("expected error for tampered token")
	}
}

func TestValidateAccessToken_Garbage(t *testing.T) {
	key := newTestKey("kid-1", "test-secret-32-bytes-long-enough!")
	_, err := validateAccessToken(resolverFor(key), "not-a-jwt-token")
	if err == nil {
		t.Error("expected error for garbage input")
	}
}

func TestGenerateAccessToken_StampsKidHeader(t *testing.T) {
	// Critical invariant: without a kid in the header, validation cannot
	// route to the right key, and rotation would become impossible. If a
	// future refactor drops the kid, this test fails immediately.
	key := newTestKey("kid-specific", "test-secret-32-bytes-long-enough!")
	token, _, err := generateAccessToken(key, "u", "n", "r", time.Minute, time.Now())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// Validation through a resolver that records the kid asked for.
	var seenKid string
	resolve := func(kid string) (*db.SigningKey, error) {
		seenKid = kid
		return key, nil
	}
	if _, err := validateAccessToken(resolve, token); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if seenKid != "kid-specific" {
		t.Errorf("resolver received kid %q, want %q", seenKid, "kid-specific")
	}
}

func TestHashToken(t *testing.T) {
	hash1 := hashToken("token-abc")
	hash2 := hashToken("token-abc")
	hash3 := hashToken("token-xyz")

	if hash1 != hash2 {
		t.Error("same input should produce same hash")
	}
	if hash1 == hash3 {
		t.Error("different input should produce different hash")
	}
	if len(hash1) != 64 {
		t.Errorf("expected 64-char hex hash, got %d chars", len(hash1))
	}
}
