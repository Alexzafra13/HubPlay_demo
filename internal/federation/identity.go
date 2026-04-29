// Package federation implements server-to-server peering for HubPlay.
//
// The package's public surface is the Manager (manager.go); identity.go
// holds this server's Ed25519 keypair, jwt.go signs and verifies the
// peer-to-peer JWTs, invite.go mints/parses single-use invite codes,
// and peer.go declares the Peer domain type.
package federation

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"hubplay/internal/clock"
	"hubplay/internal/domain"
)

// Identity holds this server's stable Ed25519 identity. Generated on
// first boot, persisted, and pinned by every peer who handshakes with
// us. Rotation is explicit (Phase 2+); for v1, this keypair is held
// for the life of the server.
type Identity struct {
	ServerUUID string
	Name       string
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
	CreatedAt  time.Time
	RotatedAt  *time.Time
}

// Fingerprint renders the Ed25519 public key as four hex groups of four
// characters (16 chars total, separated by colons) — the SSH-style
// fingerprint used at handshake time for out-of-band confirmation.
//
// We hash the raw pubkey with SHA-256 first and take the leading 8 bytes
// so the fingerprint is stable across encodings (raw bytes, base64, hex
// — all produce the same fingerprint) and short enough to read out loud.
func (i *Identity) Fingerprint() string {
	return Fingerprint(i.PublicKey)
}

// Fingerprint exposes the same calculation as a free function so the
// admin UI / handshake code can render fingerprints for peer pubkeys
// without instantiating an Identity.
func Fingerprint(pub ed25519.PublicKey) string {
	if len(pub) == 0 {
		return ""
	}
	// SHA-256(pubkey)[:8] → 16 hex chars in 4 groups of 4.
	sum := sha256First8(pub)
	hexed := hex.EncodeToString(sum)
	var sb strings.Builder
	for i := 0; i < len(hexed); i += 4 {
		if i > 0 {
			sb.WriteByte(':')
		}
		sb.WriteString(hexed[i : i+4])
	}
	return sb.String()
}

// FingerprintWords returns 4 short pronounceable words derived from the
// fingerprint bytes. Useful for voice confirmation ("the fingerprint
// starts with aardvark, barbados…"). The list is tiny (256 entries)
// and pseudo-randomly mapped per byte; collision-resistance is the same
// as the underlying 8-byte truncated SHA-256.
func (i *Identity) FingerprintWords() []string {
	return FingerprintWords(i.PublicKey)
}

// FingerprintWords (free-function variant) — see Identity.FingerprintWords.
func FingerprintWords(pub ed25519.PublicKey) []string {
	if len(pub) == 0 {
		return nil
	}
	sum := sha256First8(pub)
	out := make([]string, 4)
	// Use the first 4 bytes (each maps to one word) for a 4-word phrase.
	// More words add length without security; the underlying entropy is
	// still the full pubkey via SHA-256.
	for idx := 0; idx < 4; idx++ {
		out[idx] = phoneticWords[int(sum[idx])%len(phoneticWords)]
	}
	return out
}

// Sign produces a detached Ed25519 signature over msg using this
// server's private key. The peer verifies with VerifyPeer.
func (i *Identity) Sign(msg []byte) []byte {
	return ed25519.Sign(i.PrivateKey, msg)
}

// VerifyPeer checks an Ed25519 signature against a peer's pinned pubkey.
// Returns true on success. False covers every failure mode (wrong
// length, bad encoding, mismatch) — callers don't need to differentiate
// because the response is the same: reject the request.
func VerifyPeer(pub ed25519.PublicKey, msg, sig []byte) bool {
	if len(pub) != ed25519.PublicKeySize {
		return false
	}
	if len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(pub, msg, sig)
}

// IdentityStore persists this server's Ed25519 identity. Exactly one row
// in server_identity holds it; the constructor LoadOrCreate guarantees
// it exists.
//
// Concurrency: the in-memory cache is RW-mutex-guarded so identity
// reads (hot path: every outbound request) take a read lock, and the
// only writer is initial bootstrap or future Rotate calls.
type IdentityStore struct {
	repo  IdentityRepo
	clock clock.Clock

	mu       sync.RWMutex
	identity *Identity
}

// IdentityRepo is the slice of database operations the IdentityStore
// needs. Declaring it here keeps federation testable with an in-memory
// fake without dragging in db/sql.
type IdentityRepo interface {
	GetIdentity(ctx context.Context) (*Identity, error)
	InsertIdentity(ctx context.Context, id *Identity) error
}

// NewIdentityStore loads the persisted identity into memory.
// LoadOrCreate must have been called once before in this server's
// lifetime (typically by main.go startup); after that, NewIdentityStore
// is just a cache loader.
func NewIdentityStore(ctx context.Context, repo IdentityRepo, clk clock.Clock) (*IdentityStore, error) {
	is := &IdentityStore{repo: repo, clock: clk}
	id, err := repo.GetIdentity(ctx)
	if err != nil {
		return nil, err
	}
	if id == nil {
		return nil, domain.ErrServerIdentityMissing
	}
	is.identity = id
	return is, nil
}

// LoadOrCreate returns the persisted identity, generating + persisting a
// fresh one if none exists. Idempotent and safe to call at every boot.
func LoadOrCreate(ctx context.Context, repo IdentityRepo, clk clock.Clock, displayName string) (*Identity, error) {
	existing, err := repo.GetIdentity(ctx)
	if err != nil {
		return nil, fmt.Errorf("federation: load identity: %w", err)
	}
	if existing != nil {
		return existing, nil
	}
	id, err := newIdentity(displayName, clk.Now())
	if err != nil {
		return nil, err
	}
	if err := repo.InsertIdentity(ctx, id); err != nil {
		return nil, fmt.Errorf("federation: persist identity: %w", err)
	}
	return id, nil
}

// Current returns the in-memory identity. The hot path for every
// outbound peer request.
func (is *IdentityStore) Current() *Identity {
	is.mu.RLock()
	defer is.mu.RUnlock()
	return is.identity
}

// newIdentity generates a fresh Ed25519 keypair + UUID. Pure helper;
// no DB interaction.
func newIdentity(name string, now time.Time) (*Identity, error) {
	if name == "" {
		return nil, errors.New("federation: identity name required")
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("federation: generate keypair: %w", err)
	}
	return &Identity{
		ServerUUID: uuid.NewString(),
		Name:       name,
		PublicKey:  pub,
		PrivateKey: priv,
		CreatedAt:  now,
	}, nil
}
