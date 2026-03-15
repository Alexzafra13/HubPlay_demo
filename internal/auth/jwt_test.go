package auth

import (
	"testing"
	"time"
)

const testSecret = "test-secret-32-bytes-long-enough!"

func TestGenerateAndValidateAccessToken(t *testing.T) {
	token, expiresAt, err := generateAccessToken(testSecret, "user-123", "alex", "admin", 15*time.Minute, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token == "" {
		t.Fatal("token should not be empty")
	}
	if expiresAt.Before(time.Now()) {
		t.Error("expiresAt should be in the future")
	}

	claims, err := validateAccessToken(testSecret, token)
	if err != nil {
		t.Fatalf("unexpected error validating: %v", err)
	}

	if claims.UserID != "user-123" {
		t.Errorf("expected user ID 'user-123', got %q", claims.UserID)
	}
	if claims.Username != "alex" {
		t.Errorf("expected username 'alex', got %q", claims.Username)
	}
	if claims.Role != "admin" {
		t.Errorf("expected role 'admin', got %q", claims.Role)
	}
}

func TestValidateAccessToken_Expired(t *testing.T) {
	// Generate token that expired 1 minute ago
	token, _, err := generateAccessToken(testSecret, "user-123", "alex", "user", -1*time.Minute, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = validateAccessToken(testSecret, token)
	if err == nil {
		t.Error("expected error for expired token")
	}
}

func TestValidateAccessToken_WrongSecret(t *testing.T) {
	token, _, err := generateAccessToken(testSecret, "user-123", "alex", "user", 15*time.Minute, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = validateAccessToken("wrong-secret-key!!", token)
	if err == nil {
		t.Error("expected error for wrong secret")
	}
}

func TestValidateAccessToken_Tampered(t *testing.T) {
	token, _, err := generateAccessToken(testSecret, "user-123", "alex", "user", 15*time.Minute, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Tamper with the token
	tampered := token[:len(token)-5] + "XXXXX"
	_, err = validateAccessToken(testSecret, tampered)
	if err == nil {
		t.Error("expected error for tampered token")
	}
}

func TestValidateAccessToken_Garbage(t *testing.T) {
	_, err := validateAccessToken(testSecret, "not-a-jwt-token")
	if err == nil {
		t.Error("expected error for garbage input")
	}
}

func TestHashToken(t *testing.T) {
	hash1 := hashToken("token-abc")
	hash2 := hashToken("token-abc")
	hash3 := hashToken("token-xyz")

	if hash1 != hash2 {
		t.Error("same input should produce same hash")
	}
	if hash1 == hash3 {
		t.Error("different input should produce different hash")
	}
	if len(hash1) != 64 {
		t.Errorf("expected 64-char hex hash, got %d chars", len(hash1))
	}
}
