package auth

import (
	"context"
	"net/http"
	"strings"

	"hubplay/internal/api/apperror"
	"hubplay/internal/domain"
)

type contextKey string

const claimsKey contextKey = "claims"

// Middleware validates JWT tokens from the Authorization header.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractToken(r)
		if token == "" {
			apperror.Write(w, r.Context(), domain.NewUnauthorized("missing authorization"))
			return
		}

		claims, err := s.ValidateToken(r.Context(), token)
		if err != nil {
			apperror.Write(w, r.Context(), &domain.AppError{
				Code:       "TOKEN_INVALID",
				HTTPStatus: http.StatusUnauthorized,
				Message:    "invalid or expired token",
			})
			return
		}

		ctx := context.WithValue(r.Context(), claimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdmin rejects non-admin users with 403.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := GetClaims(r.Context())
		if claims == nil || claims.Role != "admin" {
			apperror.Write(w, r.Context(), domain.NewForbidden("admin access required"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// GetClaims returns the JWT claims from the request context.
func GetClaims(ctx context.Context) *Claims {
	claims, _ := ctx.Value(claimsKey).(*Claims)
	return claims
}

// WithClaims returns a new context with the given claims attached.
// This is useful for testing handlers that call GetClaims.
func WithClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

func extractToken(r *http.Request) string {
	// 1. Authorization: Bearer <token>
	if auth := r.Header.Get("Authorization"); auth != "" {
		if strings.HasPrefix(auth, "Bearer ") {
			return strings.TrimPrefix(auth, "Bearer ")
		}
	}
	// 2. HTTP-only cookie (secure web clients)
	if c, err := r.Cookie("hubplay_access"); err == nil && c.Value != "" {
		return c.Value
	}
	// 3. Query param (for WebSocket/SSE)
	if token := r.URL.Query().Get("token"); token != "" {
		return token
	}
	return ""
}
