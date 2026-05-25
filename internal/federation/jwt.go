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

// PeerClaims es el payload JWT entre servidores. Scope de servidor
// (issuer = server_uuid del peer); la atribucion por usuario viaja
// en el body de stream-session, no en el token.
type PeerClaims struct {
	jwt.RegisteredClaims
	// Nonce: aleatoriedad anti-replay. El receptor cachea nonces recientes
	// durante el TTL del token y rechaza duplicados.
	Nonce string `json:"nonce"`
}

// peerTokenTTL: 5 min. Corto para acotar utility de tokens robados,
// largo para absorber clock skew sin reemision excesiva.
const peerTokenTTL = 5 * time.Minute

// peerTokenSkew: tolerancia de clock-skew al validar expiry (1 min).
const peerTokenSkew = time.Minute

// IssuePeerToken firma un JWT para un request outbound. iss = nuestro
// server_uuid, aud = server_uuid del peer destino (token robado
// contra tercero se rechaza por audience incorrecto).
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

// PeerLookup resuelve un issuer JWT inbound al pubkey pineado del peer.
// Implementado por Manager para frontera limpia de deps en tests.
type PeerLookup interface {
	LookupByServerUUID(serverUUID string) (*Peer, error)
}

// ValidatePeerToken parsea y verifica un JWT inbound: algoritmo EdDSA,
// issuer conocido + paired, firma contra pubkey pineado, audience = nosotros,
// iat/exp dentro de tolerancia. Replay protection es del caller (nonce cache).
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
		// Mapear errores de la libreria jwt a sentinels del dominio.
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
	// Audience debe ser nosotros (defensa contra replay a tercer peer).
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
