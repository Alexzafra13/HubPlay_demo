package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
	"hubplay/internal/domain"
)

// DeviceCode is the domain shape exposed to internal/auth. Used to
// live as an alias to sqlc.DeviceCode; now a proper struct so the
// dual-dialect repo can return the same type regardless of which
// generated package produced the row. Nullable columns keep their
// sql.Null* shape so existing callers (`row.UserID.Valid`,
// `row.ApprovedAt.Time`) don't have to change.
type DeviceCode struct {
	DeviceCode   string
	UserCode     string
	DeviceName   string
	UserID       sql.NullString
	ExpiresAt    time.Time
	CreatedAt    time.Time
	ApprovedAt   sql.NullTime
	ConsumedAt   sql.NullTime
	LastPolledAt sql.NullTime
}

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
	sq *sqlc.Queries
	pq *sqlc_pg.Queries
}

func NewDeviceCodeRepository(driver string, database *sql.DB) *DeviceCodeRepository {
	r := &DeviceCodeRepository{}
	if IsPostgres(driver) {
		r.pq = sqlc_pg.New(database)
	} else {
		r.sq = sqlc.New(database)
	}
	return r
}

func (r *DeviceCodeRepository) useSQLite() bool { return r.sq != nil }

// Insert persists a fresh code pair. Caller generates device_code +
// user_code; we just write.
func (r *DeviceCodeRepository) Insert(ctx context.Context, code *DeviceCode) error {
	var err error
	if r.useSQLite() {
		err = r.sq.InsertDeviceCode(ctx, sqlc.InsertDeviceCodeParams{
			DeviceCode: code.DeviceCode,
			UserCode:   code.UserCode,
			DeviceName: code.DeviceName,
			ExpiresAt:  code.ExpiresAt,
			CreatedAt:  code.CreatedAt,
		})
	} else {
		err = r.pq.InsertDeviceCode(ctx, sqlc_pg.InsertDeviceCodeParams{
			DeviceCode: code.DeviceCode,
			UserCode:   code.UserCode,
			DeviceName: code.DeviceName,
			ExpiresAt:  code.ExpiresAt,
			CreatedAt:  code.CreatedAt,
		})
	}
	if err != nil {
		return fmt.Errorf("insert device code: %w", err)
	}
	return nil
}

// GetByDeviceCode returns the row by its opaque device_code. Used on
// poll. Returns domain.ErrNotFound when missing.
func (r *DeviceCodeRepository) GetByDeviceCode(ctx context.Context, deviceCode string) (*DeviceCode, error) {
	if r.useSQLite() {
		row, err := r.sq.GetDeviceCodeByDeviceCode(ctx, deviceCode)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("device code: %w", domain.ErrNotFound)
		}
		if err != nil {
			return nil, fmt.Errorf("get device code: %w", err)
		}
		dc := deviceCodeFromSqlite(row)
		return &dc, nil
	}
	row, err := r.pq.GetDeviceCodeByDeviceCode(ctx, deviceCode)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("device code: %w", domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get device code: %w", err)
	}
	dc := deviceCodeFromPg(row)
	return &dc, nil
}

// GetByUserCode returns the row by the operator-typed user_code. Used
// on approval (the user-facing /link page).
func (r *DeviceCodeRepository) GetByUserCode(ctx context.Context, userCode string) (*DeviceCode, error) {
	if r.useSQLite() {
		row, err := r.sq.GetDeviceCodeByUserCode(ctx, userCode)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("user code: %w", domain.ErrNotFound)
		}
		if err != nil {
			return nil, fmt.Errorf("get device code by user_code: %w", err)
		}
		dc := deviceCodeFromSqlite(row)
		return &dc, nil
	}
	row, err := r.pq.GetDeviceCodeByUserCode(ctx, userCode)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("user code: %w", domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get device code by user_code: %w", err)
	}
	dc := deviceCodeFromPg(row)
	return &dc, nil
}

// Approve attaches user_id + approved_at to the code. The WHERE clause
// in the SQL guards: only pending (user_id NULL) AND non-expired codes
// transition. A second approval call against the same code is a no-op
// — the caller distinguishes "approved" from "no-op" by re-fetching
// and checking approved_at.
func (r *DeviceCodeRepository) Approve(ctx context.Context, userCode, userID string, at time.Time) error {
	if r.useSQLite() {
		return r.sq.ApproveDeviceCode(ctx, sqlc.ApproveDeviceCodeParams{
			UserID:     sql.NullString{String: userID, Valid: true},
			ApprovedAt: sql.NullTime{Time: at, Valid: true},
			UserCode:   userCode,
			ExpiresAt:  at,
		})
	}
	return r.pq.ApproveDeviceCode(ctx, sqlc_pg.ApproveDeviceCodeParams{
		UserID:     sql.NullString{String: userID, Valid: true},
		ApprovedAt: sql.NullTime{Time: at, Valid: true},
		UserCode:   userCode,
		ExpiresAt:  at,
	})
}

// Consume marks the code as used (single-use after token issuance).
// Called from the poll path immediately before tokens are returned.
func (r *DeviceCodeRepository) Consume(ctx context.Context, deviceCode string, at time.Time) error {
	if r.useSQLite() {
		return r.sq.ConsumeDeviceCode(ctx, sqlc.ConsumeDeviceCodeParams{
			ConsumedAt: sql.NullTime{Time: at, Valid: true},
			DeviceCode: deviceCode,
		})
	}
	return r.pq.ConsumeDeviceCode(ctx, sqlc_pg.ConsumeDeviceCodeParams{
		ConsumedAt: sql.NullTime{Time: at, Valid: true},
		DeviceCode: deviceCode,
	})
}

// TouchPollAt updates last_polled_at for slow-down detection. Cheap
// UPDATE; called on every poll.
func (r *DeviceCodeRepository) TouchPollAt(ctx context.Context, deviceCode string, at time.Time) error {
	if r.useSQLite() {
		return r.sq.TouchDeviceCodePollAt(ctx, sqlc.TouchDeviceCodePollAtParams{
			LastPolledAt: sql.NullTime{Time: at, Valid: true},
			DeviceCode:   deviceCode,
		})
	}
	return r.pq.TouchDeviceCodePollAt(ctx, sqlc_pg.TouchDeviceCodePollAtParams{
		LastPolledAt: sql.NullTime{Time: at, Valid: true},
		DeviceCode:   deviceCode,
	})
}

// DeleteExpired sweeps rows past their TTL or already consumed. Run by
// a periodic background job.
func (r *DeviceCodeRepository) DeleteExpired(ctx context.Context, olderThan time.Time) error {
	if r.useSQLite() {
		return r.sq.DeleteExpiredDeviceCodes(ctx, olderThan)
	}
	return r.pq.DeleteExpiredDeviceCodes(ctx, olderThan)
}

// ── row mapping helpers ─────────────────────────────────────────────────

func deviceCodeFromSqlite(r sqlc.DeviceCode) DeviceCode {
	return DeviceCode{
		DeviceCode:   r.DeviceCode,
		UserCode:     r.UserCode,
		DeviceName:   r.DeviceName,
		UserID:       r.UserID,
		ExpiresAt:    r.ExpiresAt,
		CreatedAt:    r.CreatedAt,
		ApprovedAt:   r.ApprovedAt,
		ConsumedAt:   r.ConsumedAt,
		LastPolledAt: r.LastPolledAt,
	}
}

func deviceCodeFromPg(r sqlc_pg.DeviceCode) DeviceCode {
	return DeviceCode{
		DeviceCode:   r.DeviceCode,
		UserCode:     r.UserCode,
		DeviceName:   r.DeviceName,
		UserID:       r.UserID,
		ExpiresAt:    r.ExpiresAt,
		CreatedAt:    r.CreatedAt,
		ApprovedAt:   r.ApprovedAt,
		ConsumedAt:   r.ConsumedAt,
		LastPolledAt: r.LastPolledAt,
	}
}
