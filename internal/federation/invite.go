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

// invitePrefix: namespace reconocible de cada codigo de invitacion.
const invitePrefix = "hp-invite-"

// inviteEntropyBytes: 10 bytes = 80 bits de entropia, muy por encima
// del umbral de fuerza bruta en la ventana de 24h.
const inviteEntropyBytes = 10

// inviteCodeEncoding: base32 RFC 4648. Para codigos que se pegan una
// vez, std encoding es aceptable.
var inviteCodeEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateInviteCode genera un codigo de invitacion de un solo uso.
// Formato: "hp-invite-XXXXXXXXXXXXXXXX".
func GenerateInviteCode() (string, error) {
	b := make([]byte, inviteEntropyBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return invitePrefix + inviteCodeEncoding.EncodeToString(b), nil
}

// NewInvite crea una invitacion lista para persistir. expiresAt = now + ttl.
// El secreto es el codigo, no el UUID local.
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

// ValidateCodeFormat valida formato antes de consultar DB. Rechaza
// prefijo incorrecto o chars no base32. Devuelve ErrInviteInvalidFormat.
func ValidateCodeFormat(code string) error {
	if !strings.HasPrefix(code, invitePrefix) {
		return domain.ErrInviteInvalidFormat
	}
	body := strings.TrimPrefix(code, invitePrefix)
	if body == "" {
		return domain.ErrInviteInvalidFormat
	}
	// Base32 chars: A-Z y 2-7, case-insensitive.
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

// CanonicalCode normaliza un codigo para lookup en DB (uppercase del body).
func CanonicalCode(code string) string {
	if !strings.HasPrefix(code, invitePrefix) {
		return code
	}
	body := strings.TrimPrefix(code, invitePrefix)
	return invitePrefix + strings.ToUpper(body)
}
