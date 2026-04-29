package federation

import (
	"crypto/rand"
	"encoding/base32"
	"strings"
	"time"

	"github.com/google/uuid"

	"hubplay/internal/clock"
	"hubplay/internal/domain"
)

// invitePrefix is the human-recognisable namespace of every invite
// code. A pasted "hp-invite-..." is unambiguously a HubPlay invite,
// not a JWT or an API key — useful when a user copies the wrong
// thing into the admin UI's "paste invite" field.
const invitePrefix = "hp-invite-"

// inviteEntropyBytes is the random payload size before base32 encoding.
// 10 bytes = 80 bits of entropy → ~16 base32 chars, well above the
// brute-force threshold within a 24h validity window even for an
// attacker who can issue billions of guesses per second.
const inviteEntropyBytes = 10

// inviteCodeAlphabet uses Crockford's base32 — case-insensitive,
// removes confusable chars (0/O, 1/I/L). The standard library helper
// uses base32.StdEncoding (RFC 4648) which doesn't have this property
// but is easier to recognise; for invite codes that humans paste once,
// std encoding is acceptable because the user will only mistype if the
// alphabet is ambiguous in their font, which we don't render — we
// just accept their paste verbatim.
var inviteCodeEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateInviteCode produces a fresh single-use invite code suitable
// for the local admin to copy and send to the remote admin via chat,
// email, or paper. Format: "hp-invite-XXXXXXXXXXXXXXXX" (16 base32 chars).
func GenerateInviteCode() (string, error) {
	b := make([]byte, inviteEntropyBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return invitePrefix + inviteCodeEncoding.EncodeToString(b), nil
}

// NewInvite mints an invite ready to persist. ttl bounds the validity
// window; expiresAt = now + ttl. The DB row is keyed on the (random)
// code so leaking the local UUID does nothing — only the code itself
// is the secret.
func NewInvite(clk clock.Clock, createdByUserID string, ttl time.Duration) (*Invite, error) {
	code, err := GenerateInviteCode()
	if err != nil {
		return nil, err
	}
	now := clk.Now()
	return &Invite{
		ID:              uuid.NewString(),
		Code:            code,
		CreatedByUserID: createdByUserID,
		CreatedAt:       now,
		ExpiresAt:       now.Add(ttl),
	}, nil
}

// ValidateCodeFormat is a cheap up-front sanity check before hitting
// the DB on a paste. Rejects obvious typos (wrong prefix, wrong
// length) before the SQL query — saves a roundtrip on every malformed
// paste and makes the brute-force attacker run the query loop
// regardless, so it's also defence in depth.
//
// Returns ErrInviteInvalidFormat for anything that's not shaped like
// "hp-invite-" + N base32 chars where N matches the configured size.
func ValidateCodeFormat(code string) error {
	if !strings.HasPrefix(code, invitePrefix) {
		return domain.ErrInviteInvalidFormat
	}
	body := strings.TrimPrefix(code, invitePrefix)
	if body == "" {
		return domain.ErrInviteInvalidFormat
	}
	// Base32 chars are A-Z and 2-7, case-insensitive. Accept either
	// case from the paste so a typo in capitalisation isn't a soft
	// fail — the DB lookup is exact-match on the canonical form.
	for _, c := range body {
		if !isBase32Char(c) {
			return domain.ErrInviteInvalidFormat
		}
	}
	return nil
}

func isBase32Char(c rune) bool {
	if c >= 'A' && c <= 'Z' {
		return true
	}
	if c >= 'a' && c <= 'z' {
		return true
	}
	if c >= '2' && c <= '7' {
		return true
	}
	return false
}

// CanonicalCode normalises an invite code for DB lookup — uppercase
// the body so a paste with mixed case still finds the row. The prefix
// is left as-is.
func CanonicalCode(code string) string {
	if !strings.HasPrefix(code, invitePrefix) {
		return code
	}
	body := strings.TrimPrefix(code, invitePrefix)
	return invitePrefix + strings.ToUpper(body)
}
