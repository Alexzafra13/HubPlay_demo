package federation

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"hubplay/internal/clock"
	"hubplay/internal/domain"
)

// stubLookup implements PeerLookup for tests; map keyed on issuer UUID.
type stubLookup struct {
	peers map[string]*Peer
}

func (s *stubLookup) LookupByServerUUID(uuid string) (*Peer, error) {
	p, ok := s.peers[uuid]
	if !ok {
		return nil, domain.ErrPeerNotFound
	}
	return p, nil
}

func TestIssueAndValidatePeerToken_HappyPath(t *testing.T) {
	clk := clock.New()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)

	mySrv := "server-A-uuid"
	theirSrv := "server-B-uuid"

	tok, err := IssuePeerToken(clk, priv, mySrv, theirSrv)
	if err != nil {
		t.Fatal(err)
	}
	if tok == "" {
		t.Fatal("empty token")
	}

	lookup := &stubLookup{peers: map[string]*Peer{
		mySrv: {ServerUUID: mySrv, PublicKey: pub, Status: PeerPaired},
	}}
	claims, peer, err := ValidatePeerToken(clk, lookup, theirSrv, tok)
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if peer == nil || peer.ServerUUID != mySrv {
		t.Errorf("wrong peer resolved: %+v", peer)
	}
	if claims.Issuer != mySrv {
		t.Errorf("Issuer = %q, want %q", claims.Issuer, mySrv)
	}
}

func TestValidatePeerToken_RejectsWrongAudience(t *testing.T) {
	clk := clock.New()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	tok, _ := IssuePeerToken(clk, priv, "issuer", "intended-audience")

	lookup := &stubLookup{peers: map[string]*Peer{
		"issuer": {ServerUUID: "issuer", PublicKey: pub, Status: PeerPaired},
	}}
	_, _, err := ValidatePeerToken(clk, lookup, "different-audience", tok)
	if err == nil {
		t.Fatal("expected audience-mismatch error")
	}
	if !errors.Is(err, domain.ErrPeerUnauthorized) {
		t.Errorf("err = %v, want ErrPeerUnauthorized", err)
	}
}

func TestValidatePeerToken_RejectsWrongSignature(t *testing.T) {
	clk := clock.New()
	_, priv1, _ := ed25519.GenerateKey(rand.Reader)
	pub2, _, _ := ed25519.GenerateKey(rand.Reader)

	tok, _ := IssuePeerToken(clk, priv1, "issuer", "audience")

	// Pin the WRONG public key on the peer.
	lookup := &stubLookup{peers: map[string]*Peer{
		"issuer": {ServerUUID: "issuer", PublicKey: pub2, Status: PeerPaired},
	}}
	_, _, err := ValidatePeerToken(clk, lookup, "audience", tok)
	if err == nil {
		t.Fatal("expected signature mismatch error")
	}
	if !errors.Is(err, domain.ErrPeerKeyMismatch) {
		t.Errorf("err = %v, want ErrPeerKeyMismatch", err)
	}
}

func TestValidatePeerToken_RejectsRevokedPeer(t *testing.T) {
	clk := clock.New()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	tok, _ := IssuePeerToken(clk, priv, "issuer", "audience")

	lookup := &stubLookup{peers: map[string]*Peer{
		"issuer": {ServerUUID: "issuer", PublicKey: pub, Status: PeerRevoked},
	}}
	_, _, err := ValidatePeerToken(clk, lookup, "audience", tok)
	if err == nil {
		t.Fatal("expected revoked-peer error")
	}
	if !errors.Is(err, domain.ErrPeerRevoked) {
		t.Errorf("err = %v, want ErrPeerRevoked", err)
	}
}

func TestValidatePeerToken_RejectsUnknownIssuer(t *testing.T) {
	clk := clock.New()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	tok, _ := IssuePeerToken(clk, priv, "unknown-issuer", "audience")

	lookup := &stubLookup{peers: map[string]*Peer{}}
	_, _, err := ValidatePeerToken(clk, lookup, "audience", tok)
	if err == nil {
		t.Fatal("expected unknown-issuer error")
	}
}

func TestValidatePeerToken_RejectsExpiredToken(t *testing.T) {
	frozen := &fixedClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)

	tok, _ := IssuePeerToken(frozen, priv, "issuer", "audience")

	// Advance well past the token's TTL + skew tolerance.
	frozen.now = frozen.now.Add(peerTokenTTL + 2*peerTokenSkew + time.Second)

	lookup := &stubLookup{peers: map[string]*Peer{
		"issuer": {ServerUUID: "issuer", PublicKey: pub, Status: PeerPaired},
	}}
	_, _, err := ValidatePeerToken(frozen, lookup, "audience", tok)
	if err == nil {
		t.Fatal("expected expired-token error")
	}
	if !errors.Is(err, domain.ErrTokenExpired) {
		t.Errorf("err = %v, want ErrTokenExpired", err)
	}
}

// fixedClock is a tiny deterministic clock for expiry tests.
type fixedClock struct {
	now time.Time
}

func (f *fixedClock) Now() time.Time {
	return f.now
}
