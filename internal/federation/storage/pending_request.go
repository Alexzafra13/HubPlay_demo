package storage

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
	"hubplay/internal/domain"
	"hubplay/internal/federation"
)

// InsertPendingRequest persiste una peticion en estado 'pending'.
// Indice unico parcial bloquea duplicados; caller debe chequear antes.
func (r *Repository) InsertPendingRequest(ctx context.Context, p *federation.PendingRequest) error {
	var err error
	if r.useSQLite() {
		err = r.sq.InsertPendingRequest(ctx, sqlc.InsertPendingRequestParams{
			ID:                 p.ID,
			Direction:          string(p.Direction),
			PeerServerUuid:     p.PeerServerUUID,
			PeerName:           p.PeerName,
			PeerBaseUrl:        p.PeerBaseURL,
			PeerPublicKey:      []byte(p.PeerPublicKey),
			PeerAvatarColor:    p.PeerAvatarColor,
			PeerAvatarImageUrl: p.PeerAvatarImageURL,
			RequestToken:       p.RequestToken,
			CreatedAt:          p.CreatedAt,
			ExpiresAt:          p.ExpiresAt,
		})
	} else {
		err = r.pq.InsertPendingRequest(ctx, sqlc_pg.InsertPendingRequestParams{
			ID:                 p.ID,
			Direction:          string(p.Direction),
			PeerServerUuid:     p.PeerServerUUID,
			PeerName:           p.PeerName,
			PeerBaseUrl:        p.PeerBaseURL,
			PeerPublicKey:      []byte(p.PeerPublicKey),
			PeerAvatarColor:    p.PeerAvatarColor,
			PeerAvatarImageUrl: p.PeerAvatarImageURL,
			RequestToken:       p.RequestToken,
			CreatedAt:          p.CreatedAt,
			ExpiresAt:          p.ExpiresAt,
		})
	}
	if err != nil {
		return fmt.Errorf("insert pending request: %w", err)
	}
	return nil
}

func (r *Repository) GetPendingRequestByID(ctx context.Context, id string) (*federation.PendingRequest, error) {
	if r.useSQLite() {
		row, err := r.sq.GetPendingRequestByID(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("get pending request: %w", err)
		}
		return pendingFromSqliteRow(row), nil
	}
	row, err := r.pq.GetPendingRequestByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get pending request: %w", err)
	}
	return pendingFromPgRow(row), nil
}

// GetActivePendingRequestByPeer devuelve (peticion, true) si existe activa.
func (r *Repository) GetActivePendingRequestByPeer(ctx context.Context, direction federation.PendingRequestDirection, serverUUID string) (*federation.PendingRequest, bool, error) {
	if r.useSQLite() {
		row, err := r.sq.GetActivePendingRequestByPeer(ctx, sqlc.GetActivePendingRequestByPeerParams{
			Direction:      string(direction),
			PeerServerUuid: serverUUID,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, fmt.Errorf("get active pending request: %w", err)
		}
		return pendingFromSqliteRow(row), true, nil
	}
	row, err := r.pq.GetActivePendingRequestByPeer(ctx, sqlc_pg.GetActivePendingRequestByPeerParams{
		Direction:      string(direction),
		PeerServerUuid: serverUUID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get active pending request: %w", err)
	}
	return pendingFromPgRow(row), true, nil
}

func (r *Repository) ListPendingRequests(ctx context.Context, limit int) ([]*federation.PendingRequest, error) {
	if limit <= 0 {
		limit = 50
	}
	if r.useSQLite() {
		rows, err := r.sq.ListPendingRequests(ctx, int64(limit))
		if err != nil {
			return nil, fmt.Errorf("list pending requests: %w", err)
		}
		out := make([]*federation.PendingRequest, 0, len(rows))
		for i := range rows {
			out = append(out, pendingFromSqliteRow(rows[i]))
		}
		return out, nil
	}
	rows, err := r.pq.ListPendingRequests(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("list pending requests: %w", err)
	}
	out := make([]*federation.PendingRequest, 0, len(rows))
	for i := range rows {
		out = append(out, pendingFromPgRow(rows[i]))
	}
	return out, nil
}

// MarkPendingRequestResponded mueve a estado terminal.
// ErrNotFound si ya no es pending (race con otro request o expiry).
func (r *Repository) MarkPendingRequestResponded(ctx context.Context, id string, status federation.PendingRequestStatus, by string, at time.Time) error {
	var (
		n   int64
		err error
	)
	respondedAt := sql.NullTime{Time: at, Valid: true}
	respondedBy := sql.NullString{}
	if by != "" {
		respondedBy = sql.NullString{String: by, Valid: true}
	}
	if r.useSQLite() {
		n, err = r.sq.MarkPendingRequestResponded(ctx, sqlc.MarkPendingRequestRespondedParams{
			Status:             string(status),
			RespondedAt:        respondedAt,
			RespondedByUserID:  respondedBy,
			ID:                 id,
		})
	} else {
		n, err = r.pq.MarkPendingRequestResponded(ctx, sqlc_pg.MarkPendingRequestRespondedParams{
			Status:             string(status),
			RespondedAt:        respondedAt,
			RespondedByUserID:  respondedBy,
			ID:                 id,
		})
	}
	if err != nil {
		return fmt.Errorf("mark pending request responded: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ExpirePendingRequests marca como 'expired' las pending vencidas.
func (r *Repository) ExpirePendingRequests(ctx context.Context, before time.Time) (int, error) {
	var (
		n   int64
		err error
	)
	if r.useSQLite() {
		n, err = r.sq.ExpirePendingRequests(ctx, before)
	} else {
		n, err = r.pq.ExpirePendingRequests(ctx, before)
	}
	if err != nil {
		return 0, fmt.Errorf("expire pending requests: %w", err)
	}
	return int(n), nil
}

func (r *Repository) CountUnreadIncomingPendingRequests(ctx context.Context) (int, error) {
	if r.useSQLite() {
		n, err := r.sq.CountUnreadIncomingPendingRequests(ctx)
		if err != nil {
			return 0, fmt.Errorf("count incoming pending: %w", err)
		}
		return int(n), nil
	}
	n, err := r.pq.CountUnreadIncomingPendingRequests(ctx)
	if err != nil {
		return 0, fmt.Errorf("count incoming pending: %w", err)
	}
	return int(n), nil
}

func pendingFromSqliteRow(row sqlc.FederationPendingRequest) *federation.PendingRequest {
	p := &federation.PendingRequest{
		ID:                 row.ID,
		Direction:          federation.PendingRequestDirection(row.Direction),
		PeerServerUUID:     row.PeerServerUuid,
		PeerName:           row.PeerName,
		PeerBaseURL:        row.PeerBaseUrl,
		PeerPublicKey:      ed25519.PublicKey(row.PeerPublicKey),
		PeerAvatarColor:    row.PeerAvatarColor,
		PeerAvatarImageURL: row.PeerAvatarImageUrl,
		RequestToken:       row.RequestToken,
		CreatedAt:          row.CreatedAt,
		ExpiresAt:          row.ExpiresAt,
		Status:             federation.PendingRequestStatus(row.Status),
	}
	if row.RespondedAt.Valid {
		t := row.RespondedAt.Time
		p.RespondedAt = &t
	}
	if row.RespondedByUserID.Valid {
		p.RespondedByUserID = row.RespondedByUserID.String
	}
	return p
}

func pendingFromPgRow(row sqlc_pg.FederationPendingRequest) *federation.PendingRequest {
	p := &federation.PendingRequest{
		ID:                 row.ID,
		Direction:          federation.PendingRequestDirection(row.Direction),
		PeerServerUUID:     row.PeerServerUuid,
		PeerName:           row.PeerName,
		PeerBaseURL:        row.PeerBaseUrl,
		PeerPublicKey:      ed25519.PublicKey(row.PeerPublicKey),
		PeerAvatarColor:    row.PeerAvatarColor,
		PeerAvatarImageURL: row.PeerAvatarImageUrl,
		RequestToken:       row.RequestToken,
		CreatedAt:          row.CreatedAt,
		ExpiresAt:          row.ExpiresAt,
		Status:             federation.PendingRequestStatus(row.Status),
	}
	if row.RespondedAt.Valid {
		t := row.RespondedAt.Time
		p.RespondedAt = &t
	}
	if row.RespondedByUserID.Valid {
		p.RespondedByUserID = row.RespondedByUserID.String
	}
	return p
}
