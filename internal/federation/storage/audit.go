package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
	"hubplay/internal/federation"
)

func (r *Repository) InsertAuditEntry(ctx context.Context, e *federation.AuditEntry) error {
	var err error
	if r.useSQLite() {
		err = r.sq.InsertFederationAuditEntry(ctx, sqlc.InsertFederationAuditEntryParams{
			PeerID:       e.PeerID,
			RemoteUserID: nullableString(e.RemoteUserID),
			Method:       e.Method,
			Endpoint:     e.Endpoint,
			StatusCode:   int64(e.StatusCode),
			BytesOut:     e.BytesOut,
			ItemID:       nullableString(e.ItemID),
			SessionID:    nullableString(e.SessionID),
			ErrorKind:    nullableString(e.ErrorKind),
			DurationMs:   sql.NullInt64{Int64: e.DurationMs, Valid: true},
			OccurredAt:   e.OccurredAt,
		})
	} else {
		err = r.pq.InsertFederationAuditEntry(ctx, sqlc_pg.InsertFederationAuditEntryParams{
			PeerID:       e.PeerID,
			RemoteUserID: nullableString(e.RemoteUserID),
			Method:       e.Method,
			Endpoint:     e.Endpoint,
			StatusCode:   int32(e.StatusCode),
			BytesOut:     e.BytesOut,
			ItemID:       nullableString(e.ItemID),
			SessionID:    nullableString(e.SessionID),
			ErrorKind:    nullableString(e.ErrorKind),
			DurationMs:   sql.NullInt32{Int32: int32(e.DurationMs), Valid: true},
			OccurredAt:   e.OccurredAt,
		})
	}
	if err != nil {
		return fmt.Errorf("insert audit entry: %w", err)
	}
	return nil
}

func (r *Repository) ListAuditEntries(ctx context.Context, peerID string, limit int) ([]*federation.AuditEntry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	if r.useSQLite() {
		rows, err := r.sq.ListFederationAuditEntries(ctx, sqlc.ListFederationAuditEntriesParams{
			PeerID: peerID,
			Limit:  int64(limit),
		})
		if err != nil {
			return nil, fmt.Errorf("list audit entries: %w", err)
		}
		out := make([]*federation.AuditEntry, 0, len(rows))
		for _, row := range rows {
			e := &federation.AuditEntry{
				PeerID:     row.PeerID,
				Method:     row.Method,
				Endpoint:   row.Endpoint,
				StatusCode: int(row.StatusCode),
				BytesOut:   row.BytesOut,
				OccurredAt: row.OccurredAt,
			}
			if row.RemoteUserID.Valid {
				e.RemoteUserID = row.RemoteUserID.String
			}
			if row.ItemID.Valid {
				e.ItemID = row.ItemID.String
			}
			if row.SessionID.Valid {
				e.SessionID = row.SessionID.String
			}
			if row.ErrorKind.Valid {
				e.ErrorKind = row.ErrorKind.String
			}
			if row.DurationMs.Valid {
				e.DurationMs = row.DurationMs.Int64
			}
			out = append(out, e)
		}
		return out, nil
	}
	rows, err := r.pq.ListFederationAuditEntries(ctx, sqlc_pg.ListFederationAuditEntriesParams{
		PeerID: peerID,
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list audit entries: %w", err)
	}
	out := make([]*federation.AuditEntry, 0, len(rows))
	for _, row := range rows {
		e := &federation.AuditEntry{
			PeerID:     row.PeerID,
			Method:     row.Method,
			Endpoint:   row.Endpoint,
			StatusCode: int(row.StatusCode),
			BytesOut:   row.BytesOut,
			OccurredAt: row.OccurredAt,
		}
		if row.RemoteUserID.Valid {
			e.RemoteUserID = row.RemoteUserID.String
		}
		if row.ItemID.Valid {
			e.ItemID = row.ItemID.String
		}
		if row.SessionID.Valid {
			e.SessionID = row.SessionID.String
		}
		if row.ErrorKind.Valid {
			e.ErrorKind = row.ErrorKind.String
		}
		if row.DurationMs.Valid {
			e.DurationMs = int64(row.DurationMs.Int32)
		}
		out = append(out, e)
	}
	return out, nil
}

// PruneAuditBefore deletes audit rows older than the cutoff and
// returns the number of rows removed. Called from a background
// pruner (Phase 7+); for now it exists for completeness.
func (r *Repository) PruneAuditBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	var (
		n   int64
		err error
	)
	if r.useSQLite() {
		n, err = r.sq.PruneFederationAuditBefore(ctx, cutoff)
	} else {
		n, err = r.pq.PruneFederationAuditBefore(ctx, cutoff)
	}
	if err != nil {
		return 0, fmt.Errorf("prune audit: %w", err)
	}
	return n, nil
}
