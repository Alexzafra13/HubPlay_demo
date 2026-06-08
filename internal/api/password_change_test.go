package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"hubplay/internal/auth"
	authmodel "hubplay/internal/auth/model"
)

type fakeUserLookup struct {
	user *authmodel.User
	err  error
}

func (f fakeUserLookup) GetByID(context.Context, string) (*authmodel.User, error) {
	return f.user, f.err
}

func TestEnforcePasswordChange(t *testing.T) {
	mustChange := &authmodel.User{ID: "u-1", PasswordChangeRequired: true}
	ok := &authmodel.User{ID: "u-2", PasswordChangeRequired: false}

	cases := []struct {
		name     string
		user     *authmodel.User
		method   string
		path     string
		withAuth bool
		want     int
	}{
		{"GET pasa aunque deba cambiar", mustChange, http.MethodGet, "/api/v1/me", true, http.StatusOK},
		{"POST bloqueado si debe cambiar", mustChange, http.MethodPost, "/api/v1/libraries", true, http.StatusForbidden},
		{"PATCH bloqueado si debe cambiar", mustChange, http.MethodPatch, "/api/v1/users/u-9", true, http.StatusForbidden},
		{"cambiar propia clave permitido", mustChange, http.MethodPost, "/api/v1/me/password", true, http.StatusOK},
		{"logout permitido", mustChange, http.MethodPost, "/api/v1/auth/logout", true, http.StatusOK},
		{"usuario normal no se ve afectado", ok, http.MethodPost, "/api/v1/libraries", true, http.StatusOK},
		{"sin claims no bloquea (lo hace el middleware de auth)", mustChange, http.MethodPost, "/api/v1/libraries", false, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			h := EnforcePasswordChange(fakeUserLookup{user: tc.user})(next)

			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.withAuth {
				ctx := auth.WithClaims(req.Context(), &auth.Claims{UserID: tc.user.ID})
				req = req.WithContext(ctx)
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Errorf("got %d want %d", rr.Code, tc.want)
			}
		})
	}
}
