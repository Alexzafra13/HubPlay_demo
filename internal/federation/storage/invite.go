package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
	"hubplay/internal/domain"
	"hubplay/internal/federation"
)

func (r *Repository) InsertInvite(ctx context.Context, inv *federation.Invite) error {
	var err error
	if r.useSQLite() {
		err = r.sq.InsertInvite(ctx, sqlc.InsertInviteParams{
			ID:              inv.ID,
			Code:            inv.Code,
			CreatedByUserID: inv.CreatedByUserID,
			CreatedAt:       inv.CreatedAt,
			ExpiresAt:       inv.ExpiresAt,
		})
	} else {
		err = r.pq.InsertInvite(ctx, sqlc_pg.InsertInviteParams{
			ID:              inv.ID,
			Code:            inv.Code,
			CreatedByUserID: inv.CreatedByUserID,
			CreatedAt:       inv.CreatedAt,
			ExpiresAt:       inv.ExpiresAt,
		})
	}
	if err != nil {
		return fmt.Errorf("insert invite: %w", err)
	}
	return nil
}

func (r *Repository) GetInviteByCode(ctx context.Context, code string) (*federation.Invite, error) {
	if r.useSQLite() {
		row, err := r.sq.GetInviteByCode(ctx, code)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("get invite by code: %w", err)
		}
		return inviteFromSqliteRow(row), nil
	}
	row, err := r.pq.GetInviteByCode(ctx, code)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get invite by code: %w", err)
	}
	return inviteFromPgRow(row), nil
}

func (r *Repository) MarkInviteUsed(ctx context.Context, inviteID, peerID string, at time.Time) error {
	var err error
	if r.useSQLite() {
		err = r.sq.MarkInviteUsed(ctx, sqlc.MarkInviteUsedParams{
			AcceptedByPeerID: sql.NullString{String: peerID, Valid: peerID != ""},
			AcceptedAt:       sql.NullTime{Time: at, Valid: true},
			ID:               inviteID,
		})
	} else {
		err = r.pq.MarkInviteUsed(ctx, sqlc_pg.MarkInviteUsedParams{
			AcceptedByPeerID: sql.NullString{String: peerID, Valid: peerID != ""},
			AcceptedAt:       sql.NullTime{Time: at, Valid: true},
			ID:               inviteID,
		})
	}
	if err != nil {
		return fmt.Errorf("mark invite used: %w", err)
	}
	return nil
}

func (r *Repository) ListActiveInvites(ctx context.Context) ([]*federation.Invite, error) {
	now := time.Now().UTC()
	if r.useSQLite() {
		rows, err := r.sq.ListActiveInvites(ctx, now)
		if err != nil {
			return nil, fmt.Errorf("list active invites: %w", err)
		}
		out := make([]*federation.Invite, 0, len(rows))
		for i := range rows {
			out = append(out, inviteFromSqliteRow(rows[i]))
		}
		return out, nil
	}
	rows, err := r.pq.ListActiveInvites(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("list active invites: %w", err)
	}
	out := make([]*federation.Invite, 0, len(rows))
	for i := range rows {
		out = append(out, inviteFromPgRow(rows[i]))
	}
	return out, nil
}

func inviteFromSqliteRow(row sqlc.FederationInvite) *federation.Invite {
	inv := &federation.Invite{
		ID:              row.ID,
		Code:            row.Code,
		CreatedByUserID: row.CreatedByUserID,
		CreatedAt:       row.CreatedAt,
		ExpiresAt:       row.ExpiresAt,
	}
	if row.AcceptedByPeerID.Valid {
		v := row.AcceptedByPeerID.String
		inv.AcceptedByPeerID = &v
	}
	if row.AcceptedAt.Valid {
		t := row.AcceptedAt.Time
		inv.AcceptedAt = &t
	}
	return inv
}

func inviteFromPgRow(row sqlc_pg.FederationInvite) *federation.Invite {
	inv := &federation.Invite{
		ID:              row.ID,
		Code:            row.Code,
		CreatedByUserID: row.CreatedByUserID,
		CreatedAt:       row.CreatedAt,
		ExpiresAt:       row.ExpiresAt,
	}
	if row.AcceptedByPeerID.Valid {
		v := row.AcceptedByPeerID.String
		inv.AcceptedByPeerID = &v
	}
	if row.AcceptedAt.Valid {
		t := row.AcceptedAt.Time
		inv.AcceptedAt = &t
	}
	return inv
}
