package federation

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"hubplay/internal/clock"
	"hubplay/internal/domain"
)

// fakeRepo is a tiny in-memory IdentityRepo used only by these tests.
type fakeRepo struct {
	identity *Identity
}

func (f *fakeRepo) GetIdentity(_ context.Context) (*Identity, error) {
	return f.identity, nil
}

func (f *fakeRepo) InsertIdentity(_ context.Context, id *Identity) error {
	f.identity = id
	return nil
}

func TestFingerprint_DeterministicForSamePubkey(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	a := Fingerprint(pub)
	b := Fingerprint(pub)
	if a != b {
		t.Fatalf("fingerprint should be deterministic: %s != %s", a, b)
	}
	// Format check: 4 hex groups of 4 separated by colons.
	if len(a) != 19 {
		t.Errorf("fingerprint length = %d, want 19", len(a))
	}
	if strings.Count(a, ":") != 3 {
		t.Errorf("fingerprint should have 3 colons, got %q", a)
	}
}

func TestFingerprint_DifferentPubkeysYieldDifferentFingerprints(t *testing.T) {
	pub1, _, _ := ed25519.GenerateKey(rand.Reader)
	pub2, _, _ := ed25519.GenerateKey(rand.Reader)
	if Fingerprint(pub1) == Fingerprint(pub2) {
		t.Fatal("two different pubkeys produced the same fingerprint")
	}
}

func TestFingerprint_EmptyKey(t *testing.T) {
	if Fingerprint(nil) != "" {
		t.Errorf("nil pubkey should yield empty fingerprint")
	}
	if Fingerprint([]byte{}) != "" {
		t.Errorf("empty pubkey should yield empty fingerprint")
	}
}

func TestFingerprintWords_LengthAndPronounceable(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	words := FingerprintWords(pub)
	if len(words) != 4 {
		t.Fatalf("want 4 words, got %d", len(words))
	}
	for _, w := range words {
		if w == "" {
			t.Errorf("empty word in fingerprint phrase: %v", words)
		}
	}
}

func TestLoadOrCreate_GeneratesOnFirstCall(t *testing.T) {
	repo := &fakeRepo{}
	clk := clock.New()
	id, err := LoadOrCreate(context.Background(), repo, clk, "TestServer")
	if err != nil {
		t.Fatal(err)
	}
	if id == nil {
		t.Fatal("identity is nil after LoadOrCreate")
	}
	if id.ServerUUID == "" {
		t.Errorf("ServerUUID empty")
	}
	if id.Name != "TestServer" {
		t.Errorf("Name = %q, want %q", id.Name, "TestServer")
	}
	if len(id.PublicKey) != ed25519.PublicKeySize {
		t.Errorf("PublicKey size = %d, want %d", len(id.PublicKey), ed25519.PublicKeySize)
	}
	if len(id.PrivateKey) != ed25519.PrivateKeySize {
		t.Errorf("PrivateKey size = %d, want %d", len(id.PrivateKey), ed25519.PrivateKeySize)
	}
}

func TestLoadOrCreate_IdempotentOnSecondCall(t *testing.T) {
	repo := &fakeRepo{}
	clk := clock.New()
	id1, err := LoadOrCreate(context.Background(), repo, clk, "TestServer")
	if err != nil {
		t.Fatal(err)
	}
	id2, err := LoadOrCreate(context.Background(), repo, clk, "DifferentName")
	if err != nil {
		t.Fatal(err)
	}
	if id1.ServerUUID != id2.ServerUUID {
		t.Errorf("UUIDs should match across calls: %s != %s", id1.ServerUUID, id2.ServerUUID)
	}
}

func TestSignAndVerify_RoundTrip(t *testing.T) {
	repo := &fakeRepo{}
	clk := clock.New()
	id, _ := LoadOrCreate(context.Background(), repo, clk, "Tester")
	msg := []byte("hello peer")
	sig := id.Sign(msg)
	if !VerifyPeer(id.PublicKey, msg, sig) {
		t.Fatal("verify failed for own signature")
	}
	if VerifyPeer(id.PublicKey, []byte("tampered"), sig) {
		t.Error("verify accepted tampered message")
	}
}

func TestVerifyPeer_RejectsWrongLengthInputs(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	good := ed25519.Sign(priv, []byte("x"))

	if VerifyPeer(pub[:5], []byte("x"), good) {
		t.Error("accepted truncated pubkey")
	}
	if VerifyPeer(pub, []byte("x"), good[:5]) {
		t.Error("accepted truncated signature")
	}
}

func TestNewIdentityStore_ErrorsWhenIdentityMissing(t *testing.T) {
	repo := &fakeRepo{} // empty
	clk := clock.New()
	_, err := NewIdentityStore(context.Background(), repo, clk)
	if err == nil {
		t.Fatal("expected error when identity missing")
	}
	if err != domain.ErrServerIdentityMissing {
		t.Errorf("err = %v, want ErrServerIdentityMissing", err)
	}
}

// guard: phonetic words list is full and non-empty.
func TestPhoneticWords_Coverage(t *testing.T) {
	// 256 entries, none empty, no leading/trailing spaces.
	for i, w := range phoneticWords {
		if w == "" {
			t.Errorf("phoneticWords[%d] is empty", i)
		}
		if w != strings.TrimSpace(w) {
			t.Errorf("phoneticWords[%d] has whitespace: %q", i, w)
		}
	}
}

// guard: clock conformance — newIdentity propagates the now value.
func TestNewIdentity_PropagatesClock(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	id, err := newIdentity("X", now)
	if err != nil {
		t.Fatal(err)
	}
	if !id.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", id.CreatedAt, now)
	}
}
