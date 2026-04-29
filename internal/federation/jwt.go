package federation

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"hubplay/internal/clock"
	"hubplay/internal/domain"
)

// PeerClaims is the JWT payload travelling between paired servers. The
// shape is intentionally minimal — federation auth is not user-scoped
// at the JWT layer; it's server-scoped (issuer = peer's server_uuid).
// Per-user attribution for federation streaming travels in the request
// body of stream-session requests, not in this token.
type PeerClaims struct {
	jwt.RegisteredClaims
	// Nonce is server-issued randomness to defeat replay within the
	// short TTL window. The receiver caches recently-seen nonces for
	// the token TTL and rejects duplicates.
	Nonce string `json:"nonce"`
}

// peerTokenTTL is the maximum lifetime of a peer JWT. Five minutes is
// short enough that a stolen token has bounded utility (the attacker
// can replay until expiry) and long enough to absorb mild clock skew
// between the two servers without reissuance churn.
const peerTokenTTL = 5 * time.Minute

// peerTokenSkew is the clock-skew tolerance when validating expiry.
// One minute either side of the issuer's nominal expiry covers
// well-synced NTP between two home servers without weakening replay
// resistance materially.
const peerTokenSkew = time.Minute

// IssuePeerToken signs a fresh peer JWT for an outbound request from
// us to `peerServerUUID`. The token's iss is our server_uuid (the
// receiver looks up our pubkey in their peers table to verify),
// aud is the target peer's server_uuid (so a stolen token replayed
// against a third server is rejected for wrong audience).
func IssuePeerToken(clk clock.Clock, ourPriv ed25519.PrivateKey, ourServerUUID, peerServerUUID string) (string, error) {
	if len(ourPriv) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("federation: private key wrong size (%d)", len(ourPriv))
	}
	now := clk.Now()
	claims := PeerClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    ourServerUUID,
			Subject:   "peer-call",
			Audience:  jwt.ClaimStrings{peerServerUUID},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(peerTokenTTL)),
			NotBefore: jwt.NewNumericDate(now.Add(-peerTokenSkew)),
			ID:        uuid.NewString(),
		},
		Nonce: uuid.NewString(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	signed, err := tok.SignedString(ourPriv)
	if err != nil {
		return "", fmt.Errorf("federation: sign peer token: %w", err)
	}
	return signed, nil
}

// PeerLookup resolves an inbound JWT issuer (the peer's server_uuid)
// to the peer's pinned pubkey + status. Implemented by the Manager so
// validation has a clean dependency boundary in tests.
type PeerLookup interface {
	LookupByServerUUID(serverUUID string) (*Peer, error)
}

// ValidatePeerToken parses an inbound peer JWT and verifies:
//
//  1. Algorithm is EdDSA (no algorithm-confusion attack).
//  2. The issuer (claims.iss) maps to a known, paired peer.
//  3. The signature verifies against that peer's pinned pubkey.
//  4. The audience (claims.aud) is OUR server_uuid.
//  5. iat/exp are within tolerance of clk.Now().
//
// Returns the parsed PeerClaims + the matching *Peer on success.
// Returns one of the domain sentinel errors on failure so the API
// layer can map to the correct HTTP status.
//
// Replay protection is the *caller's* responsibility — a token is
// individually valid; the per-nonce cache lives in the Manager so it
// can scope nonces by peer + window.
func ValidatePeerToken(clk clock.Clock, lookup PeerLookup, ourServerUUID, raw string) (*PeerClaims, *Peer, error) {
	var foundPeer *Peer

	parsed, err := jwt.ParseWithClaims(raw, &PeerClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodEd25519); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		claims, ok := t.Claims.(*PeerClaims)
		if !ok {
			return nil, errors.New("malformed claims")
		}
		if claims.Issuer == "" {
			return nil, errors.New("missing issuer")
		}
		peer, err := lookup.LookupByServerUUID(claims.Issuer)
		if err != nil {
			return nil, err
		}
		if peer.Status == PeerRevoked {
			return nil, domain.ErrPeerRevoked
		}
		if peer.Status != PeerPaired {
			return nil, domain.ErrPeerUnauthorized
		}
		foundPeer = peer
		return ed25519.PublicKey(peer.PublicKey), nil
	}, jwt.WithLeeway(peerTokenSkew))

	if err != nil {
		// Map jwt library errors into domain sentinels for clean API mapping.
		switch {
		case errors.Is(err, jwt.ErrTokenExpired):
			return nil, nil, domain.ErrTokenExpired
		case errors.Is(err, jwt.ErrTokenSignatureInvalid):
			return nil, nil, domain.ErrPeerKeyMismatch
		case errors.Is(err, domain.ErrPeerNotFound),
			errors.Is(err, domain.ErrPeerRevoked),
			errors.Is(err, domain.ErrPeerUnauthorized),
			errors.Is(err, domain.ErrPeerKeyMismatch):
			return nil, nil, err
		default:
			return nil, nil, fmt.Errorf("federation: validate peer token: %w", err)
		}
	}

	claims, ok := parsed.Claims.(*PeerClaims)
	if !ok || !parsed.Valid {
		return nil, nil, domain.ErrInvalidToken
	}
	// Audience must be us — defends against a stolen token being
	// replayed against a third peer.
	audOk := false
	for _, a := range claims.Audience {
		if a == ourServerUUID {
			audOk = true
			break
		}
	}
	if !audOk {
		return nil, nil, domain.ErrPeerUnauthorized
	}

	return claims, foundPeer, nil
}
