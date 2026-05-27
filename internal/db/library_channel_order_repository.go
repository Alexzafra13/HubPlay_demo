package db

import (
	iptvmodel "hubplay/internal/iptv/model"
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// LibraryChannelOrderRepository wraps the `library_channel_order`
// table dual-dialect. Pattern B (raw SQL with RewritePlaceholders)
// for the same reason UserChannelOrderRepository goes raw: sqlc 1.31.x
// truncates the trailing identifier of queries ending in
// `ORDER BY ... ASC`, which would silently break List(). The two
// repos are intentionally near-identical in shape so future
// composition (admin overlay applied before user overlay) stays
// symmetric and easy to reason about.
type LibraryChannelOrderRepository struct {
	db     *sql.DB
	driver string
}

func NewLibraryChannelOrderRepository(driver string, database *sql.DB) *LibraryChannelOrderRepository {
	return &LibraryChannelOrderRepository{db: database, driver: driver}
}

// Upsert writes (or replaces) a single override row for the library.
// The admin panel composes a full ordering as a series of upserts
// inside a transaction (see ReplaceAll); single Upsert is exposed for
// surgical edits like the per-channel "hide" toggle on the admin
// channel list.
func (r *LibraryChannelOrderRepository) Upsert(ctx context.Context, libraryID, channelID string, position int, hidden bool) error {
	now := timeNow().UTC()
	query := RewritePlaceholders(r.driver, `
		INSERT INTO library_channel_order (library_id, channel_id, position, hidden, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(library_id, channel_id) DO UPDATE SET
			position   = excluded.position,
			hidden     = excluded.hidden,
			updated_at = excluded.updated_at`)
	var hiddenArg any
	if IsPostgres(r.driver) {
		hiddenArg = hidden
	} else if hidden {
		hiddenArg = int64(1)
	} else {
		hiddenArg = int64(0)
	}
	if _, err := r.db.ExecContext(ctx, query, libraryID, channelID, position, hiddenArg, now); err != nil {
		return fmt.Errorf("upsert library_channel_order: %w", err)
	}
	return nil
}

// Delete removes a single override. After this call the channel
// falls back to its M3U-import number and is visible again to
// everyone (subject to per-user overlays).
func (r *LibraryChannelOrderRepository) Delete(ctx context.Context, libraryID, channelID string) error {
	query := RewritePlaceholders(r.driver,
		`DELETE FROM library_channel_order WHERE library_id = ? AND channel_id = ?`)
	if _, err := r.db.ExecContext(ctx, query, libraryID, channelID); err != nil {
		return fmt.Errorf("delete library_channel_order: %w", err)
	}
	return nil
}

// Reset wipes every override for a library. The "Restore M3U order"
// button calls this — much cheaper than streaming the whole channel
// list back through Upsert with default values.
func (r *LibraryChannelOrderRepository) Reset(ctx context.Context, libraryID string) error {
	query := RewritePlaceholders(r.driver,
		`DELETE FROM library_channel_order WHERE library_id = ?`)
	if _, err := r.db.ExecContext(ctx, query, libraryID); err != nil {
		return fmt.Errorf("reset library_channel_order: %w", err)
	}
	return nil
}

// List returns every override for a library, ordered by the admin's
// chosen position. Used by the curation panel to render which
// channels the admin has touched and which still inherit the M3U
// number.
func (r *LibraryChannelOrderRepository) List(ctx context.Context, libraryID string) ([]iptvmodel.LibraryChannelOrderEntry, error) {
	query := RewritePlaceholders(r.driver, `
		SELECT library_id, channel_id, position, hidden, updated_at
		FROM library_channel_order
		WHERE library_id = ?
		ORDER BY position ASC, channel_id ASC`)
	rows, err := r.db.QueryContext(ctx, query, libraryID)
	if err != nil {
		return nil, fmt.Errorf("list library_channel_order: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	out := []iptvmodel.LibraryChannelOrderEntry{}
	for rows.Next() {
		var e iptvmodel.LibraryChannelOrderEntry
		if IsPostgres(r.driver) {
			if err := rows.Scan(&e.LibraryID, &e.ChannelID, &e.Position, &e.Hidden, &e.UpdatedAt); err != nil {
				return nil, fmt.Errorf("scan library_channel_order: %w", err)
			}
		} else {
			var hiddenInt int64
			if err := rows.Scan(&e.LibraryID, &e.ChannelID, &e.Position, &hiddenInt, &e.UpdatedAt); err != nil {
				return nil, fmt.Errorf("scan library_channel_order: %w", err)
			}
			e.Hidden = hiddenInt != 0
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ReplaceAll wipes every override for a library and re-installs the
// provided ordering atomically.
func (r *LibraryChannelOrderRepository) ReplaceAll(ctx context.Context, libraryID string, entries []iptvmodel.LibraryChannelOrderEntry) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	delQ := RewritePlaceholders(r.driver,
		`DELETE FROM library_channel_order WHERE library_id = ?`)
	if _, err := tx.ExecContext(ctx, delQ, libraryID); err != nil {
		return fmt.Errorf("clear library_channel_order: %w", err)
	}

	if len(entries) == 0 {
		return tx.Commit()
	}

	placeholders := make([]string, 0, len(entries))
	args := make([]any, 0, len(entries)*5)
	now := timeNow().UTC()
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
		args = append(args, libraryID, e.ChannelID, i+1, hiddenArg, now)
	}
	insQ := RewritePlaceholders(r.driver,
		"INSERT INTO library_channel_order (library_id, channel_id, position, hidden, updated_at) VALUES " + strings.Join(placeholders, ", "))
	if _, err := tx.ExecContext(ctx, insQ, args...); err != nil {
		return fmt.Errorf("insert library_channel_order batch: %w", err)
	}
	return tx.Commit()
}
