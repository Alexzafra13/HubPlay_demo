package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"hubplay/internal/db"
)

type Claims struct {
	UserID   string `json:"sub"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// keyResolver is the lookup function token validation uses to resolve a kid
// into its secret. Taking a function (rather than a concrete KeyStore) keeps
// the JWT layer free of auth-package cycles and trivial to fake in tests.
type keyResolver func(kid string) (*db.SigningKey, error)

// generateAccessToken signs a new access token with the provided key and
// stamps the key's id into the JWT header so validators can resolve the
// secret by kid. Doing the lookup by kid (rather than trying every active
// key) keeps validation O(1) and lets rotation retire keys without breaking
// in-flight tokens signed with the previous primary.
func generateAccessToken(key *db.SigningKey, userID, username, role string, ttl time.Duration, now time.Time) (string, time.Time, error) {
	if key == nil {
		return "", time.Time{}, fmt.Errorf("generateAccessToken: nil signing key")
	}

	expiresAt := now.Add(ttl)
	claims := Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = key.ID

	signed, err := token.SignedString([]byte(key.Secret))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("signing jwt: %w", err)
	}
	return signed, expiresAt, nil
}

// validateAccessToken parses and validates a token by extracting the kid
// from the header, resolving it via the provided resolver, and verifying
// the HMAC signature with that key's secret.
//
// A missing kid, an unknown kid, or the wrong signing algorithm are all
// rejected (distinct error strings, same outcome: 401).
func validateAccessToken(resolve keyResolver, tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, fmt.Errorf("missing kid header")
		}
		k, err := resolve(kid)
		if err != nil {
			return nil, err
		}
		return []byte(k.Secret), nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	return claims, nil
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
