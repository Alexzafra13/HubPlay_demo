package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
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
			token = generateCSRFToken()
		}

		// Always (re)set the cookie so the frontend can read it
		http.SetCookie(w, &http.Cookie{
			Name:     csrfCookieName,
			Value:    token,
			Path:     "/",
			MaxAge:   86400, // 24h
			HttpOnly: false, // JS must read this
			Secure:   true,
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

		// Validate: header must match cookie
		headerToken := r.Header.Get(csrfHeaderName)
		if headerToken == "" || headerToken != token {
			http.Error(w,
				`{"error":{"code":"CSRF_FAILED","message":"missing or invalid CSRF token"}}`,
				http.StatusForbidden,
			)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func isSafeMethod(method string) bool {
	m := strings.ToUpper(method)
	return m == "GET" || m == "HEAD" || m == "OPTIONS"
}

func generateCSRFToken() string {
	b := make([]byte, csrfTokenBytes)
	if _, err := rand.Read(b); err != nil {
		// Fallback should never happen with crypto/rand
		panic("csrf: failed to generate random token: " + err.Error())
	}
	return hex.EncodeToString(b)
}
