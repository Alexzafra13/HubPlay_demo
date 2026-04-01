package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCSRF_SafeMethodsPass(t *testing.T) {
	handler := CSRFProtect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, method := range []string{"GET", "HEAD", "OPTIONS"} {
		req := httptest.NewRequest(method, "/api/v1/health", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", method, rr.Code)
		}
		// Should set CSRF cookie
		cookies := rr.Result().Cookies()
		found := false
		for _, c := range cookies {
			if c.Name == csrfCookieName {
				found = true
				if c.HttpOnly {
					t.Errorf("CSRF cookie must NOT be HttpOnly")
				}
			}
		}
		if !found {
			t.Errorf("%s: expected CSRF cookie to be set", method)
		}
	}
}

func TestCSRF_MutatingWithoutToken(t *testing.T) {
	handler := CSRFProtect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestCSRF_MutatingWithValidToken(t *testing.T) {
	handler := CSRFProtect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First, do a GET to get the CSRF cookie
	getReq := httptest.NewRequest("GET", "/api/v1/health", nil)
	getRR := httptest.NewRecorder()
	handler.ServeHTTP(getRR, getReq)

	var csrfToken string
	for _, c := range getRR.Result().Cookies() {
		if c.Name == csrfCookieName {
			csrfToken = c.Value
		}
	}
	if csrfToken == "" {
		t.Fatal("no CSRF cookie set on GET")
	}

	// Now POST with matching cookie + header
	postReq := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	postReq.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfToken})
	postReq.Header.Set(csrfHeaderName, csrfToken)

	postRR := httptest.NewRecorder()
	handler.ServeHTTP(postRR, postReq)

	if postRR.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", postRR.Code)
	}
}

func TestCSRF_MismatchedTokenFails(t *testing.T) {
	handler := CSRFProtect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/v1/test", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "real-token"})
	req.Header.Set(csrfHeaderName, "wrong-token")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}
