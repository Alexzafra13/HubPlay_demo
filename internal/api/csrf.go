package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"

	"hubplay/internal/api/apperror"
	"hubplay/internal/domain"
)

const (
	csrfCookieName = "hubplay_csrf"
	csrfHeaderName = "X-CSRF-Token"
	csrfTokenBytes = 16 // 32 hex chars
)

// CSRFProtect implements the double-submit cookie pattern.
// It sets a non-HttpOnly CSRF cookie on every response and validates that
// mutating requests (POST/PUT/DELETE/PATCH) include a matching header.
//
// CSRF validation is only enforced when the request carries an auth session
// cookie (hubplay_access). Unauthenticated requests (login, setup, refresh)
// skip validation because there is no session to hijack.
func CSRFProtect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read or generate the CSRF token
		token := ""
		if c, err := r.Cookie(csrfCookieName); err == nil && c.Value != "" {
			token = c.Value
		}
		if token == "" {
			generated, err := generateCSRFToken()
			if err != nil {
				slog.Error("csrf: token generation failed", "error", err)
				apperror.Write(w, r.Context(), domain.NewInternal(err))
				return
			}
			token = generated
		}

		// Always (re)set the cookie so the frontend can read it.
		//
		// `Secure` follows the actual transport (TLS detected on the
		// connection or X-Forwarded-Proto=https) so plain http://
		// localhost dev still attaches the cookie to mutating
		// requests. Forcing Secure on plain HTTP made some browsers
		// drop the cookie on POST while keeping it on GET — same
		// failure mode that hit the auth cookie.
		secure := false
		if r.TLS != nil {
			secure = true
		} else if r.Header.Get("X-Forwarded-Proto") == "https" {
			secure = true
		}
		http.SetCookie(w, &http.Cookie{
			Name:     csrfCookieName,
			Value:    token,
			Path:     "/",
			MaxAge:   86400, // 24h
			HttpOnly: false, // JS must read this
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
		})

		// Safe methods don't need CSRF validation
		if isSafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		// Only enforce CSRF when the request has an auth session cookie.
		// Without a session cookie there is no authenticated state to protect.
		if _, err := r.Cookie("hubplay_access"); err != nil {
			next.ServeHTTP(w, r)
			return
		}

		// Validate: header must match cookie. Constant-time compare for
		// consistency with the other token checks (e.g. the /metrics
		// gate) — the double-submit token isn't a high-value secret, but
		// there's no reason to leave a timing signal on the table.
		headerToken := r.Header.Get(csrfHeaderName)
		if headerToken == "" || subtle.ConstantTimeCompare([]byte(headerToken), []byte(token)) != 1 {
			apperror.Write(w, r.Context(), &domain.AppError{
				Code:       "CSRF_FAILED",
				HTTPStatus: http.StatusForbidden,
				Message:    "missing or invalid CSRF token",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

func isSafeMethod(method string) bool {
	m := strings.ToUpper(method)
	return m == "GET" || m == "HEAD" || m == "OPTIONS"
}

// generateCSRFToken returns a fresh hex-encoded random token.
// crypto/rand.Read is documented to never fail on supported platforms,
// but a hardware-RNG outage is the kind of failure where panicking
// inside an HTTP handler is the wrong call — it'd take down the whole
// server. The middleware now renders a 500 via apperror.Write instead.
func generateCSRFToken() (string, error) {
	b := make([]byte, csrfTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
