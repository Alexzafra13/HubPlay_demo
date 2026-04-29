package federation

import (
	"errors"
	"strings"
	"testing"
	"time"

	"hubplay/internal/clock"
	"hubplay/internal/domain"
)

func TestGenerateInviteCode_FormatAndUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		code, err := GenerateInviteCode()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(code, "hp-invite-") {
			t.Fatalf("missing prefix: %q", code)
		}
		if seen[code] {
			t.Fatalf("duplicate code in %d generations: %q", i, code)
		}
		seen[code] = true
	}
}

func TestValidateCodeFormat(t *testing.T) {
	tests := []struct {
		name    string
		code    string
		wantErr error
	}{
		{"valid uppercase", "hp-invite-ABCDEFGH23456777", nil},
		{"valid lowercase", "hp-invite-abcdefgh23456777", nil},
		{"valid mixed case", "hp-invite-AaBbCcDd23456777", nil},
		{"missing prefix", "ABCDEFGH23456777", domain.ErrInviteInvalidFormat},
		{"wrong prefix", "hp-other-ABCDEFGH23456777", domain.ErrInviteInvalidFormat},
		{"empty body", "hp-invite-", domain.ErrInviteInvalidFormat},
		{"non-base32 digit 1", "hp-invite-ABCDEFGH13456777", domain.ErrInviteInvalidFormat}, // 1 not in base32
		{"non-base32 digit 0", "hp-invite-ABCDEFGH03456777", domain.ErrInviteInvalidFormat}, // 0 not in base32
		{"non-base32 digit 8", "hp-invite-ABCDEFGH83456777", domain.ErrInviteInvalidFormat}, // 8 not in base32
		{"empty string", "", domain.ErrInviteInvalidFormat},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCodeFormat(tt.code)
			if !errors.Is(err, tt.wantErr) && (err != nil) != (tt.wantErr != nil) {
				t.Errorf("got %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestCanonicalCode_UpperCases(t *testing.T) {
	if got := CanonicalCode("hp-invite-abc123"); got != "hp-invite-ABC123" {
		t.Errorf("got %q, want hp-invite-ABC123", got)
	}
	// Non-prefix passthrough — defence in depth, the lookup will fail anyway.
	if got := CanonicalCode("not-an-invite"); got != "not-an-invite" {
		t.Errorf("non-prefix should pass through unchanged, got %q", got)
	}
}

func TestNewInvite_FieldsPopulated(t *testing.T) {
	clk := clock.New()
	inv, err := NewInvite(clk, "user-123", 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if inv.ID == "" {
		t.Error("ID empty")
	}
	if inv.Code == "" {
		t.Error("Code empty")
	}
	if inv.CreatedByUserID != "user-123" {
		t.Errorf("CreatedByUserID = %q, want user-123", inv.CreatedByUserID)
	}
	if !inv.ExpiresAt.After(inv.CreatedAt) {
		t.Errorf("ExpiresAt %v not after CreatedAt %v", inv.ExpiresAt, inv.CreatedAt)
	}
	want := inv.CreatedAt.Add(24 * time.Hour)
	// Sub-second tolerance for clock jitter; the calculation is deterministic
	// modulo whatever the clock returned for "now".
	if diff := inv.ExpiresAt.Sub(want).Abs(); diff > time.Second {
		t.Errorf("ExpiresAt off by %v", diff)
	}
}

func TestInvite_IsUsable(t *testing.T) {
	now := time.Date(2026, 4, 29, 18, 0, 0, 0, time.UTC)
	inv := &Invite{
		CreatedAt: now,
		ExpiresAt: now.Add(time.Hour),
	}
	if !inv.IsUsable(now) {
		t.Error("fresh invite should be usable at its creation time")
	}
	if !inv.IsUsable(now.Add(30 * time.Minute)) {
		t.Error("invite should be usable within validity window")
	}
	if inv.IsUsable(now.Add(time.Hour + time.Second)) {
		t.Error("invite should not be usable after expiry")
	}
	// Already-used invite is never usable.
	usedAt := now.Add(10 * time.Minute)
	inv.AcceptedAt = &usedAt
	if inv.IsUsable(now.Add(20 * time.Minute)) {
		t.Error("used invite should never be usable")
	}
}
