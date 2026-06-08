package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestMiddleware_TokenSources verifica de dónde acepta (y de dónde NO
// acepta) el token el middleware de auth. En concreto fija el fix del
// olor C1: el access token YA NO se acepta por query param `?token=`
// (se filtraba a logs de proxy, Referer e historial). Bearer y cookie
// siguen funcionando.
func TestMiddleware_TokenSources(t *testing.T) {
	svc, _, _ := newTestAuthService(t)
	registerTestUser(t, svc)

	tok, err := svc.Login(context.Background(), "testuser", "password123", "Chrome", "dev-1", "127.0.0.1")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	// Handler protegido que solo responde 200 si el middleware dejó pasar.
	protected := svc.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	do := func(mut func(*http.Request)) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
		mut(req)
		rr := httptest.NewRecorder()
		protected.ServeHTTP(rr, req)
		return rr.Code
	}

	t.Run("Bearer header aceptado", func(t *testing.T) {
		code := do(func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok.AccessToken) })
		if code != http.StatusOK {
			t.Errorf("Bearer: got %d want 200", code)
		}
	})

	t.Run("cookie aceptada", func(t *testing.T) {
		code := do(func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: "hubplay_access", Value: tok.AccessToken})
		})
		if code != http.StatusOK {
			t.Errorf("cookie: got %d want 200", code)
		}
	})

	t.Run("query param ?token= RECHAZADO (C1)", func(t *testing.T) {
		code := do(func(r *http.Request) {
			q := r.URL.Query()
			q.Set("token", tok.AccessToken)
			r.URL.RawQuery = q.Encode()
		})
		if code != http.StatusUnauthorized {
			t.Errorf("query token: got %d want 401 (el token en query debe ignorarse)", code)
		}
	})

	t.Run("sin token RECHAZADO", func(t *testing.T) {
		if code := do(func(*http.Request) {}); code != http.StatusUnauthorized {
			t.Errorf("sin token: got %d want 401", code)
		}
	})
}
