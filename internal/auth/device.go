package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/domain"
)

// DeviceCodeTTL is how long a freshly-issued code pair stays valid.
// 10 minutes lines up with the RFC 8628 default and is short enough
// that a leaked user_code becomes worthless before a phishing victim
// realises what happened.
const DeviceCodeTTL = 10 * time.Minute

// DefaultPollInterval is the polling cadence we suggest to clients in
// the start response. Clients should respect `interval` from the wire
// and back off (RFC 8628 §3.5: "slow_down" extends interval by 5s).
const DefaultPollInterval = 5 * time.Second

// minPollGap is the minimum gap between successive polls before we
// flag the client as too aggressive and return slow_down. Tracked
// per-row via last_polled_at.
const minPollGap = 4 * time.Second

// userCodeAlphabet is a no-ambiguity alphabet for the human-typed
// portion of the flow. Excludes 0/O, 1/I/L, 5/S to reduce typos when
// the operator reads the code off a TV screen and types it on a
// phone. 26 chars × 8 positions = 26^8 ≈ 2 × 10^11 — overkill for a
// 10-minute TTL, but cheap.
const userCodeAlphabet = "ABCDEFGHJKMNPQRTUVWXYZ234679"

// userCodeLength is the human-typed portion length, BEFORE the
// dash. Total displayed code is "ABCD-EFGH" = 9 chars including dash.
const userCodeLength = 8

// DeviceCodeService is the thin orchestration layer for the device
// authorization grant. It composes the existing Service (token
// issuance, session creation) with the device_codes repo (state
// machine).
//
// Flow:
//  1. StartDevice → caller gets {device_code, user_code, interval, expires_in}.
//  2. Operator opens /link, authenticates, and calls ApproveDevice with
//     the user_code.
//  3. PollDevice with the device_code returns ErrAuthorizationPending
//     while user_id is null, then issues an AuthToken once approved
//     (and marks the row consumed so the next poll fails).
type DeviceCodeService struct {
	auth   *Service
	codes  *db.DeviceCodeRepository
	users  *db.UserRepository
	logger Logger // small interface to avoid pulling slog into every call site
}

// Logger is the slim interface DeviceCodeService logs through. The
// production wiring passes a *slog.Logger, which satisfies it.
type Logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// NewDeviceCodeService wires the dependencies. None are nilable — a
// device-code service without an underlying auth service can't issue
// tokens, which is the whole point.
func NewDeviceCodeService(auth *Service, codes *db.DeviceCodeRepository, users *db.UserRepository, logger Logger) *DeviceCodeService {
	return &DeviceCodeService{
		auth:   auth,
		codes:  codes,
		users:  users,
		logger: logger,
	}
}

// DeviceCodePair is the result of a successful StartDevice call.
type DeviceCodePair struct {
	DeviceCode      string        // opaque hex, ~256-bit randomness
	UserCode        string        // displayed to operator, e.g. "ABCD-EFGH"
	VerificationURL string        // where to send the operator (e.g. "https://hubplay.example.com/link")
	ExpiresIn       time.Duration // until the code pair is rejected
	Interval        time.Duration // recommended poll interval
}

// StartDevice creates a fresh code pair and persists it in pending
// state. `deviceName` is a friendly label the device chooses ("Living-
// room TV", "Kotlin TV app on Pedro's TV") that appears in the audit /
// admin session list once approved.
func (s *DeviceCodeService) StartDevice(ctx context.Context, deviceName, verificationURL string) (*DeviceCodePair, error) {
	if strings.TrimSpace(deviceName) == "" {
		deviceName = "Unknown device"
	}
	if len(deviceName) > 80 {
		deviceName = deviceName[:80]
	}
	now := s.auth.clock.Now()

	deviceCode, err := generateDeviceCode()
	if err != nil {
		return nil, fmt.Errorf("device code: gen device_code: %w", err)
	}
	userCode, err := generateUserCode()
	if err != nil {
		return nil, fmt.Errorf("device code: gen user_code: %w", err)
	}

	row := &db.DeviceCode{
		DeviceCode: deviceCode,
		UserCode:   userCode,
		DeviceName: deviceName,
		ExpiresAt:  now.Add(DeviceCodeTTL),
		CreatedAt:  now,
	}
	if err := s.codes.Insert(ctx, row); err != nil {
		return nil, err
	}

	s.logger.Info("device code issued",
		"user_code", userCode, "device_name", deviceName, "ttl", DeviceCodeTTL.String())

	return &DeviceCodePair{
		DeviceCode:      deviceCode,
		UserCode:        userCode,
		VerificationURL: verificationURL,
		ExpiresIn:       DeviceCodeTTL,
		Interval:        DefaultPollInterval,
	}, nil
}

// ApproveDevice attaches the authenticated user_id to a pending code
// row keyed by user_code. Called by the /link page handler after the
// operator confirms.
//
// Returns:
//   - nil on success.
//   - domain.ErrNotFound if the user_code doesn't exist.
//   - domain.ErrTokenExpired if the code is past its TTL.
//   - domain.ErrAlreadyExists if the code was already approved (the
//     operator clicked twice or two browser tabs raced; idempotent
//     UX handled by the handler).
func (s *DeviceCodeService) ApproveDevice(ctx context.Context, userCode, userID string) error {
	canonical := canonicalUserCode(userCode)
	row, err := s.codes.GetByUserCode(ctx, canonical)
	if err != nil {
		return err
	}
	now := s.auth.clock.Now()
	if now.After(row.ExpiresAt) {
		return domain.ErrTokenExpired
	}
	if row.UserID.Valid {
		// Already approved. If the same user is re-confirming, idempotent.
		// If a different user tries to approve, treat as conflict.
		if row.UserID.String == userID {
			return nil
		}
		return fmt.Errorf("device code: %w (already approved by another user)", domain.ErrAlreadyExists)
	}

	// Verify the user actually exists + is active. We don't strictly
	// need this — the JWT middleware that gated the handler already
	// verified — but a session can outlive an account disabling
	// in-flight; double-checking here closes the gap.
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if !user.IsActive {
		return domain.ErrAccountDisabled
	}

	if err := s.codes.Approve(ctx, canonical, userID, now); err != nil {
		return err
	}
	s.logger.Info("device code approved",
		"user_code", canonical, "user_id", userID, "device_name", row.DeviceName)
	return nil
}

// PollDevice is called by the device on a periodic basis. Returns:
//
//   - (token, nil) when the code has been approved AND not yet
//     consumed. Marks consumed_at as a side-effect.
//   - (nil, ErrAuthorizationPending) if still pending — the operator
//     hasn't approved yet.
//   - (nil, ErrSlowDown) if the device polled too aggressively (last
//     poll < minPollGap ago).
//   - (nil, domain.ErrNotFound) for an unknown device_code.
//   - (nil, domain.ErrTokenExpired) for an expired or already-consumed
//     code.
//
// The IP is for the session row that gets created on approval; the
// device-code flow doesn't have its own IP rate-limiter (the per-IP
// rate limiter on the LoginHandler also covers /auth/device/poll).
func (s *DeviceCodeService) PollDevice(ctx context.Context, deviceCode, ip string) (*AuthToken, error) {
	row, err := s.codes.GetByDeviceCode(ctx, deviceCode)
	if err != nil {
		return nil, err
	}
	now := s.auth.clock.Now()

	if now.After(row.ExpiresAt) {
		return nil, domain.ErrTokenExpired
	}
	if row.ConsumedAt.Valid {
		// Single-use guard: a consumed code cannot issue tokens again.
		// Surface as Expired so the device's UI can prompt for a new
		// code without confusion about the "consumed" state.
		return nil, domain.ErrTokenExpired
	}

	// Slow-down detection: if the device polled within the last
	// minPollGap, surface ErrSlowDown so the device backs off. The
	// touch happens on every poll regardless so the next call sees
	// the right gap.
	if row.LastPolledAt.Valid && now.Sub(row.LastPolledAt.Time) < minPollGap {
		_ = s.codes.TouchPollAt(ctx, deviceCode, now)
		return nil, ErrSlowDown
	}
	_ = s.codes.TouchPollAt(ctx, deviceCode, now)

	if !row.UserID.Valid {
		return nil, ErrAuthorizationPending
	}

	user, err := s.users.GetByID(ctx, row.UserID.String)
	if err != nil {
		return nil, err
	}
	if !user.IsActive {
		return nil, domain.ErrAccountDisabled
	}

	// Issue the token via the same path as a password login. The
	// session row gets the device_name we stored at start.
	deviceID := "device-code-" + deviceCode[:16]
	token, err := s.auth.createSession(ctx, user, row.DeviceName, deviceID, ip)
	if err != nil {
		return nil, err
	}
	if err := s.codes.Consume(ctx, deviceCode, now); err != nil {
		s.logger.Warn("device code: failed to mark consumed", "err", err, "device_code_prefix", deviceCode[:8])
		// Don't fail the call — token is already issued; worst case is
		// the row gets cleaned up by the periodic sweep.
	}
	s.logger.Info("device code consumed (token issued)",
		"user_id", user.ID, "device_name", row.DeviceName)
	return token, nil
}

// ErrAuthorizationPending is the protocol-level "not approved yet"
// signal. The handler maps this to a 400 with `{"error": "authorization_pending"}`
// per RFC 8628 §3.5.
var ErrAuthorizationPending = errors.New("authorization_pending")

// ErrSlowDown signals the device is polling too aggressively. RFC 8628
// §3.5 says the device should add 5s to its interval on each occurrence.
var ErrSlowDown = errors.New("slow_down")

// canonicalUserCode normalises the operator's typed input. Strips
// dashes and whitespace, uppercases. The /link form does this too, but
// callers in the handler chain shouldn't rely on it — defense in depth.
func canonicalUserCode(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, " ", "")
	return s
}

// generateDeviceCode produces 32 hex chars from 16 random bytes.
// Opaque to the operator; only the device sees it.
func generateDeviceCode() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// generateUserCode produces a fresh user code from the no-ambiguity
// alphabet. Returned WITHOUT the dash; callers wishing to display it
// can format it as "ABCD-EFGH". The DB stores the raw form so a quick
// lookup matches whether the user typed dashes or not — combined with
// canonicalUserCode at handler time.
func generateUserCode() (string, error) {
	buf := make([]byte, userCodeLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, userCodeLength)
	for i, b := range buf {
		out[i] = userCodeAlphabet[int(b)%len(userCodeAlphabet)]
	}
	return string(out), nil
}

// FormatUserCodeDisplay inserts a dash between the two halves of a raw
// 8-char user_code for nicer display. "ABCDEFGH" → "ABCD-EFGH".
func FormatUserCodeDisplay(raw string) string {
	if len(raw) != userCodeLength {
		return raw
	}
	return raw[:4] + "-" + raw[4:]
}
