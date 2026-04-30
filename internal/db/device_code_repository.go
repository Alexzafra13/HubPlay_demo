package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/domain"
)

// DeviceCode is the sqlc-generated row type for the device_codes table.
// Aliased so callers in `internal/auth/` use a stable name without
// importing the sqlc package directly.
type DeviceCode = sqlc.DeviceCode

// DeviceCodeRepository persists OAuth 2.0 device authorization grants
// (RFC 8628). The lifecycle: insert → poll/approve → consume → expire.
//
// State semantics:
//
//	pending   user_id IS NULL
//	approved  user_id IS NOT NULL AND consumed_at IS NULL
//	consumed  consumed_at IS NOT NULL  (single-use after token issuance)
//	expired   expires_at < now()       (regardless of state above)
//
// The repository is a thin sqlc adapter; the auth service owns the
// state-machine logic.
type DeviceCodeRepository struct {
	q *sqlc.Queries
}

func NewDeviceCodeRepository(database *sql.DB) *DeviceCodeRepository {
	return &DeviceCodeRepository{q: sqlc.New(database)}
}

// Insert persists a fresh code pair. Caller generates device_code +
// user_code; we just write.
func (r *DeviceCodeRepository) Insert(ctx context.Context, code *DeviceCode) error {
	if err := r.q.InsertDeviceCode(ctx, sqlc.InsertDeviceCodeParams{
		DeviceCode: code.DeviceCode,
		UserCode:   code.UserCode,
		DeviceName: code.DeviceName,
		ExpiresAt:  code.ExpiresAt,
		CreatedAt:  code.CreatedAt,
	}); err != nil {
		return fmt.Errorf("insert device code: %w", err)
	}
	return nil
}

// GetByDeviceCode returns the row by its opaque device_code. Used on
// poll. Returns domain.ErrNotFound when missing.
func (r *DeviceCodeRepository) GetByDeviceCode(ctx context.Context, deviceCode string) (*DeviceCode, error) {
	row, err := r.q.GetDeviceCodeByDeviceCode(ctx, deviceCode)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("device code: %w", domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get device code: %w", err)
	}
	return &row, nil
}

// GetByUserCode returns the row by the operator-typed user_code. Used
// on approval (the user-facing /link page).
func (r *DeviceCodeRepository) GetByUserCode(ctx context.Context, userCode string) (*DeviceCode, error) {
	row, err := r.q.GetDeviceCodeByUserCode(ctx, userCode)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("user code: %w", domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get device code by user_code: %w", err)
	}
	return &row, nil
}

// Approve attaches user_id + approved_at to the code. The WHERE clause
// in the SQL guards: only pending (user_id NULL) AND non-expired codes
// transition. A second approval call against the same code is a no-op
// — the caller distinguishes "approved" from "no-op" by re-fetching
// and checking approved_at.
func (r *DeviceCodeRepository) Approve(ctx context.Context, userCode, userID string, at time.Time) error {
	return r.q.ApproveDeviceCode(ctx, sqlc.ApproveDeviceCodeParams{
		UserID:     sql.NullString{String: userID, Valid: true},
		ApprovedAt: sql.NullTime{Time: at, Valid: true},
		UserCode:   userCode,
		ExpiresAt:  at, // expires_at > at — the WHERE clause uses this value as "now"
	})
}

// Consume marks the code as used (single-use after token issuance).
// Called from the poll path immediately before tokens are returned.
func (r *DeviceCodeRepository) Consume(ctx context.Context, deviceCode string, at time.Time) error {
	return r.q.ConsumeDeviceCode(ctx, sqlc.ConsumeDeviceCodeParams{
		ConsumedAt: sql.NullTime{Time: at, Valid: true},
		DeviceCode: deviceCode,
	})
}

// TouchPollAt updates last_polled_at for slow-down detection. Cheap
// UPDATE; called on every poll.
func (r *DeviceCodeRepository) TouchPollAt(ctx context.Context, deviceCode string, at time.Time) error {
	return r.q.TouchDeviceCodePollAt(ctx, sqlc.TouchDeviceCodePollAtParams{
		LastPolledAt: sql.NullTime{Time: at, Valid: true},
		DeviceCode:   deviceCode,
	})
}

// DeleteExpired sweeps rows past their TTL or already consumed. Run by
// a periodic background job.
func (r *DeviceCodeRepository) DeleteExpired(ctx context.Context, olderThan time.Time) error {
	return r.q.DeleteExpiredDeviceCodes(ctx, olderThan)
}
