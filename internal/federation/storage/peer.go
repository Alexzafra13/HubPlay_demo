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

func (r *Repository) InsertPeer(ctx context.Context, p *federation.Peer) error {
	pairedAt := sql.NullTime{}
	if p.PairedAt != nil {
		pairedAt = sql.NullTime{Time: *p.PairedAt, Valid: true}
	}
	var err error
	if r.useSQLite() {
		err = r.sq.InsertPeer(ctx, sqlc.InsertPeerParams{
			ID:             p.ID,
			ServerUuid:     p.ServerUUID,
			Name:           p.Name,
			BaseUrl:        p.BaseURL,
			PublicKey:      []byte(p.PublicKey),
			Status:         string(p.Status),
			CreatedAt:      p.CreatedAt,
			PairedAt:       pairedAt,
			AvatarColor:    p.AvatarColor,
			AvatarImageUrl: p.AvatarImageURL,
		})
	} else {
		err = r.pq.InsertPeer(ctx, sqlc_pg.InsertPeerParams{
			ID:             p.ID,
			ServerUuid:     p.ServerUUID,
			Name:           p.Name,
			BaseUrl:        p.BaseURL,
			PublicKey:      []byte(p.PublicKey),
			Status:         string(p.Status),
			CreatedAt:      p.CreatedAt,
			PairedAt:       pairedAt,
			AvatarColor:    p.AvatarColor,
			AvatarImageUrl: p.AvatarImageURL,
		})
	}
	if err != nil {
		return fmt.Errorf("insert peer: %w", err)
	}
	return nil
}

// UpdatePeerBranding refresca name + color + image_url del peer.
// El admin lo invoca via "Actualizar" en PeersTable: el manager
// re-probea /federation/info del remoto y persiste lo que recibe.
// Idempotente — si nada ha cambiado el UPDATE es no-op semantico.
func (r *Repository) UpdatePeerBranding(ctx context.Context, peerID, name, avatarColor, avatarImageURL string) error {
	var err error
	if r.useSQLite() {
		err = r.sq.UpdatePeerBranding(ctx, sqlc.UpdatePeerBrandingParams{
			Name:           name,
			AvatarColor:    avatarColor,
			AvatarImageUrl: avatarImageURL,
			ID:             peerID,
		})
	} else {
		err = r.pq.UpdatePeerBranding(ctx, sqlc_pg.UpdatePeerBrandingParams{
			Name:           name,
			AvatarColor:    avatarColor,
			AvatarImageUrl: avatarImageURL,
			ID:             peerID,
		})
	}
	if err != nil {
		return fmt.Errorf("update peer branding: %w", err)
	}
	return nil
}

func (r *Repository) UpdatePeerPaired(ctx context.Context, peerID string, at time.Time) error {
	var err error
	if r.useSQLite() {
		err = r.sq.UpdatePeerPaired(ctx, sqlc.UpdatePeerPairedParams{
			PairedAt: sql.NullTime{Time: at, Valid: true},
			ID:       peerID,
		})
	} else {
		err = r.pq.UpdatePeerPaired(ctx, sqlc_pg.UpdatePeerPairedParams{
			PairedAt: sql.NullTime{Time: at, Valid: true},
			ID:       peerID,
		})
	}
	if err != nil {
		return fmt.Errorf("update peer paired: %w", err)
	}
	return nil
}

func (r *Repository) UpdatePeerRevoked(ctx context.Context, peerID string, at time.Time) error {
	var (
		n   int64
		err error
	)
	if r.useSQLite() {
		n, err = r.sq.UpdatePeerRevoked(ctx, sqlc.UpdatePeerRevokedParams{
			RevokedAt: sql.NullTime{Time: at, Valid: true},
			ID:        peerID,
		})
	} else {
		n, err = r.pq.UpdatePeerRevoked(ctx, sqlc_pg.UpdatePeerRevokedParams{
			RevokedAt: sql.NullTime{Time: at, Valid: true},
			ID:        peerID,
		})
	}
	if err != nil {
		return fmt.Errorf("update peer revoked: %w", err)
	}
	if n == 0 {
		// No existe o ya revocado. Surfacear ErrPeerNotFound.
		return domain.ErrPeerNotFound
	}
	return nil
}

func (r *Repository) UpdatePeerLastSeen(ctx context.Context, peerID string, at time.Time, statusCode int) error {
	var err error
	if r.useSQLite() {
		err = r.sq.UpdatePeerLastSeen(ctx, sqlc.UpdatePeerLastSeenParams{
			LastSeenAt:         sql.NullTime{Time: at, Valid: true},
			LastSeenStatusCode: sql.NullInt64{Int64: int64(statusCode), Valid: true},
			ID:                 peerID,
		})
	} else {
		err = r.pq.UpdatePeerLastSeen(ctx, sqlc_pg.UpdatePeerLastSeenParams{
			LastSeenAt:         sql.NullTime{Time: at, Valid: true},
			LastSeenStatusCode: sql.NullInt32{Int32: int32(statusCode), Valid: true},
			ID:                 peerID,
		})
	}
	if err != nil {
		return fmt.Errorf("update peer last seen: %w", err)
	}
	return nil
}

func (r *Repository) GetPeerByID(ctx context.Context, id string) (*federation.Peer, error) {
	if r.useSQLite() {
		row, err := r.sq.GetPeerByID(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrPeerNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("get peer by id: %w", err)
		}
		return peerFromSqliteRow(row), nil
	}
	row, err := r.pq.GetPeerByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrPeerNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get peer by id: %w", err)
	}
	return peerFromPgRow(row), nil
}

func (r *Repository) GetPeerByServerUUID(ctx context.Context, serverUUID string) (*federation.Peer, error) {
	if r.useSQLite() {
		row, err := r.sq.GetPeerByServerUUID(ctx, serverUUID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrPeerNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("get peer by server uuid: %w", err)
		}
		return peerFromSqliteRow(row), nil
	}
	row, err := r.pq.GetPeerByServerUUID(ctx, serverUUID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrPeerNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get peer by server uuid: %w", err)
	}
	return peerFromPgRow(row), nil
}

func (r *Repository) ListPeers(ctx context.Context) ([]*federation.Peer, error) {
	if r.useSQLite() {
		rows, err := r.sq.ListPeers(ctx)
		if err != nil {
			return nil, fmt.Errorf("list peers: %w", err)
		}
		out := make([]*federation.Peer, 0, len(rows))
		for i := range rows {
			out = append(out, peerFromSqliteRow(rows[i]))
		}
		return out, nil
	}
	rows, err := r.pq.ListPeers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list peers: %w", err)
	}
	out := make([]*federation.Peer, 0, len(rows))
	for i := range rows {
		out = append(out, peerFromPgRow(rows[i]))
	}
	return out, nil
}

func peerFromSqliteRow(row sqlc.FederationPeer) *federation.Peer {
	p := &federation.Peer{
		ID:             row.ID,
		ServerUUID:     row.ServerUuid,
		Name:           row.Name,
		BaseURL:        row.BaseUrl,
		PublicKey:      row.PublicKey,
		Status:         federation.PeerStatus(row.Status),
		CreatedAt:      row.CreatedAt,
		AvatarColor:    row.AvatarColor,
		AvatarImageURL: row.AvatarImageUrl,
	}
	if row.PairedAt.Valid {
		t := row.PairedAt.Time
		p.PairedAt = &t
	}
	if row.LastSeenAt.Valid {
		t := row.LastSeenAt.Time
		p.LastSeenAt = &t
	}
	if row.LastSeenStatusCode.Valid {
		v := int(row.LastSeenStatusCode.Int64)
		p.LastSeenStatusCode = &v
	}
	if row.RevokedAt.Valid {
		t := row.RevokedAt.Time
		p.RevokedAt = &t
	}
	return p
}

func peerFromPgRow(row sqlc_pg.FederationPeer) *federation.Peer {
	p := &federation.Peer{
		ID:             row.ID,
		ServerUUID:     row.ServerUuid,
		Name:           row.Name,
		BaseURL:        row.BaseUrl,
		PublicKey:      row.PublicKey,
		Status:         federation.PeerStatus(row.Status),
		CreatedAt:      row.CreatedAt,
		AvatarColor:    row.AvatarColor,
		AvatarImageURL: row.AvatarImageUrl,
	}
	if row.PairedAt.Valid {
		t := row.PairedAt.Time
		p.PairedAt = &t
	}
	if row.LastSeenAt.Valid {
		t := row.LastSeenAt.Time
		p.LastSeenAt = &t
	}
	if row.LastSeenStatusCode.Valid {
		v := int(row.LastSeenStatusCode.Int32)
		p.LastSeenStatusCode = &v
	}
	if row.RevokedAt.Valid {
		t := row.RevokedAt.Time
		p.RevokedAt = &t
	}
	return p
}
