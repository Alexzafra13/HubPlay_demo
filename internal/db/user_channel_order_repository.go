package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// UserChannelOrderEntry is one user's override row for a single
// channel. The personalisation panel renders the rows and lets the
// user move them up/down (position) or toggle them off (hidden).
type UserChannelOrderEntry struct {
	UserID    string
	ChannelID string
	Position  int
	Hidden    bool
	UpdatedAt time.Time
}

// UserChannelOrderRepository wraps the `user_channel_order` table
// dual-dialect. Pattern B (raw SQL with RewritePlaceholders) because
// sqlc 1.31.1 truncates the trailing identifier of queries ending
// in `ORDER BY ... ASC` — half of the queries here would have been
// silently corrupted at generation time. See docs/memory/conventions.md.
type UserChannelOrderRepository struct {
	db     *sql.DB
	driver string
}

func NewUserChannelOrderRepository(driver string, database *sql.DB) *UserChannelOrderRepository {
	return &UserChannelOrderRepository{db: database, driver: driver}
}

// Upsert writes (or replaces) a single override row. The personalisation
// endpoint composes a full ordering as a series of upserts inside a
// transaction (see ReplaceAll below) — single Upsert is exposed for
// surgical edits like the per-channel "hide" toggle.
//
// hidden is a bool on Postgres (BOOLEAN column) and an INTEGER on
// SQLite (the CHECK constraint enforces 0/1). The bool→int coerce
// happens here so callers can use the same shape across dialects.
func (r *UserChannelOrderRepository) Upsert(ctx context.Context, userID, channelID string, position int, hidden bool) error {
	now := time.Now().UTC()
	query := RewritePlaceholders(r.driver, `
		INSERT INTO user_channel_order (user_id, channel_id, position, hidden, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id, channel_id) DO UPDATE SET
			position   = excluded.position,
			hidden     = excluded.hidden,
			updated_at = excluded.updated_at`)
	var hiddenArg any
	if IsPostgres(r.driver) {
		hiddenArg = hidden
	} else {
		// SQLite stores BOOLEAN as INTEGER with a CHECK(hidden IN (0, 1))
		// constraint; pass an int explicitly so the driver doesn't have
		// to coerce the bool itself (modernc.org/sqlite would, but
		// we'd rather be explicit and match the constraint shape).
		if hidden {
			hiddenArg = int64(1)
		} else {
			hiddenArg = int64(0)
		}
	}
	if _, err := r.db.ExecContext(ctx, query, userID, channelID, position, hiddenArg, now); err != nil {
		return fmt.Errorf("upsert user_channel_order: %w", err)
	}
	return nil
}

// Delete removes a single override. After this call the user sees
// that channel's admin-provided number and visibility again.
func (r *UserChannelOrderRepository) Delete(ctx context.Context, userID, channelID string) error {
	query := RewritePlaceholders(r.driver,
		`DELETE FROM user_channel_order WHERE user_id = ? AND channel_id = ?`)
	if _, err := r.db.ExecContext(ctx, query, userID, channelID); err != nil {
		return fmt.Errorf("delete user_channel_order: %w", err)
	}
	return nil
}

// Reset wipes every override row for a user. The "Restore admin's
// order" button calls this — much cheaper than streaming the whole
// channel list back through Upsert with default values.
func (r *UserChannelOrderRepository) Reset(ctx context.Context, userID string) error {
	query := RewritePlaceholders(r.driver,
		`DELETE FROM user_channel_order WHERE user_id = ?`)
	if _, err := r.db.ExecContext(ctx, query, userID); err != nil {
		return fmt.Errorf("reset user_channel_order: %w", err)
	}
	return nil
}

// List returns every override a user has, ordered by their chosen
// position. Used by the personalisation panel to render which
// channels the user has touched and which still inherit the admin
// defaults.
func (r *UserChannelOrderRepository) List(ctx context.Context, userID string) ([]UserChannelOrderEntry, error) {
	query := RewritePlaceholders(r.driver, `
		SELECT user_id, channel_id, position, hidden, updated_at
		FROM user_channel_order
		WHERE user_id = ?
		ORDER BY position ASC, channel_id ASC`)
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list user_channel_order: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	out := []UserChannelOrderEntry{}
	for rows.Next() {
		var e UserChannelOrderEntry
		if IsPostgres(r.driver) {
			if err := rows.Scan(&e.UserID, &e.ChannelID, &e.Position, &e.Hidden, &e.UpdatedAt); err != nil {
				return nil, fmt.Errorf("scan user_channel_order: %w", err)
			}
		} else {
			// SQLite: hidden comes back as int64.
			var hiddenInt int64
			if err := rows.Scan(&e.UserID, &e.ChannelID, &e.Position, &hiddenInt, &e.UpdatedAt); err != nil {
				return nil, fmt.Errorf("scan user_channel_order: %w", err)
			}
			e.Hidden = hiddenInt != 0
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ReplaceAll wipes every override for a user and re-installs the
// provided ordering atomically. Used by the panel's "Save order"
// button which sends the entire reordered list (vs. asking the
// frontend to compute and send only the deltas). One round trip,
// one transaction, no partial states.
//
// Channels NOT present in `entries` are left without an override
// row — they fall through to the admin defaults. That's the only
// way the user can opt back into the admin's ordering for a subset
// of channels without nuking the entire personalisation.
func (r *UserChannelOrderRepository) ReplaceAll(ctx context.Context, userID string, entries []UserChannelOrderEntry) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	delQ := RewritePlaceholders(r.driver,
		`DELETE FROM user_channel_order WHERE user_id = ?`)
	if _, err := tx.ExecContext(ctx, delQ, userID); err != nil {
		return fmt.Errorf("clear user_channel_order: %w", err)
	}

	if len(entries) == 0 {
		return tx.Commit()
	}

	// Build a single multi-row INSERT — N round trips would dominate
	// the latency for a 500-channel list. Each tuple gets its own
	// 5-placeholder block.
	placeholders := make([]string, 0, len(entries))
	args := make([]any, 0, len(entries)*5)
	now := time.Now().UTC()
	for i, e := range entries {
		placeholders = append(placeholders, "(?, ?, ?, ?, ?)")
		var hiddenArg any
		if IsPostgres(r.driver) {
			hiddenArg = e.Hidden
		} else if e.Hidden {
			hiddenArg = int64(1)
		} else {
			hiddenArg = int64(0)
		}
		args = append(args, userID, e.ChannelID, i+1, hiddenArg, now)
	}
	insQ := RewritePlaceholders(r.driver,
		"INSERT INTO user_channel_order (user_id, channel_id, position, hidden, updated_at) VALUES "+strings.Join(placeholders, ", "))
	if _, err := tx.ExecContext(ctx, insQ, args...); err != nil {
		return fmt.Errorf("insert user_channel_order batch: %w", err)
	}
	return tx.Commit()
}
