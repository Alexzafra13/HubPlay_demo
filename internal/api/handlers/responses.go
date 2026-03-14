package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"hubplay/internal/domain"
)

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

func respondError(w http.ResponseWriter, status int, code, message string) {
	respondJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func handleServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		respondError(w, http.StatusNotFound, "NOT_FOUND", "resource not found")
	case errors.Is(err, domain.ErrAlreadyExists):
		respondError(w, http.StatusConflict, "ALREADY_EXISTS", "resource already exists")
	case errors.Is(err, domain.ErrInvalidPassword):
		respondError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid username or password")
	case errors.Is(err, domain.ErrUnauthorized), errors.Is(err, domain.ErrInvalidToken):
		respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
	case errors.Is(err, domain.ErrTokenExpired):
		respondError(w, http.StatusUnauthorized, "TOKEN_EXPIRED", "access token has expired")
	case errors.Is(err, domain.ErrForbidden):
		respondError(w, http.StatusForbidden, "FORBIDDEN", "insufficient permissions")
	case errors.Is(err, domain.ErrAccountDisabled):
		respondError(w, http.StatusForbidden, "ACCOUNT_DISABLED", "account is disabled")
	case errors.Is(err, domain.ErrValidation):
		var valErr *domain.ValidationError
		if errors.As(err, &valErr) {
			respondJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]any{
					"code":    "VALIDATION_ERROR",
					"message": "validation failed",
					"details": map[string]any{"fields": valErr.Fields},
				},
			})
			return
		}
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "validation failed")
	case errors.Is(err, domain.ErrConflict):
		respondError(w, http.StatusConflict, "CONFLICT", "operation conflicts with current state")
	default:
		slog.Error("unhandled error", "error", err)
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
	}
}
